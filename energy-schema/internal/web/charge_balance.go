package web

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"energy-schema/internal/config"
)

// Балансировка «по верху» (хелпер «Заряд: балансировка», только при включённом
// авто-регуляторе): самая высокая ячейка держится у верха порциями заряда, чтобы
// пассивный балансир BMS работал. Порция — ток хвоста, с разбега BalanceSoftMV
// (или ячейки BalanceSoftV) — минимальный; после первого верха сессии порции
// сразу минимальным током. Верх — ячейка ≥ BalanceStopV (или BMS снял лимит
// заряда) → пауза без заряда, пока не пройдёт BalancePauseMin, ячейка не опустится
// до BalanceResumeV и BMS снова не разрешит заряд. Разбег на верху ≤ BalanceDoneMV —
// выровнено, хелпер гасится сам; через BalanceMaxDays — тоже. ToU выключен всю
// сессию (сеть помогает и в порциях), возвращается по её окончании.
const (
	chargeBalanceEntity = "input_boolean.energy_schema_charge_balance"
	chargeBalanceStatus = "sensor.energy_schema_charge_balance_status"
	bmsLimitsEntity     = "sensor.energy_schema_bms_limits"
	bmsCellMaxEntity    = "sensor.energy_schema_bms_cell_max"
	bmsCellDeltaEntity  = "sensor.energy_schema_bms_cell_delta"
	balCharge           = "charge"
	balRest             = "rest" // пауза между порциями
	balDone             = "done" // выровнено, ждём, пока HA погасит хелпер
)

// balanceTick — шаг балансировки. ok — ток ведёт балансировка (amps, mode);
// иначе — обычная регуляция. Статус публикуется каждый тик.
func (s *Server) balanceTick(st *chargeState, soc, maxA float64, auto bool) (amps float64, mode string, ok bool) {
	t := s.cfg.Charge
	on := s.store.On(chargeBalanceEntity)
	delta := s.store.Num(bmsCellDeltaEntity)
	cellMax := s.store.Num(bmsCellMaxEntity)
	limit, known := s.store.AttrNum(bmsLimitsEntity, "charge_a"), s.store.Attr(bmsLimitsEntity, "charge_a") != ""
	cellOK := s.store.Available(bmsCellMaxEntity) && cellMax <= t.BalanceResumeV
	bmsOK := known && limit > 0
	defer func() {
		f := math.Max(1, st.Factor)
		_ = s.client.SetState(chargeBalanceStatus, balanceText(st, on, auto, delta, regValue(amps, f)*f, pauseWait(st, t, cellOK, bmsOK)), map[string]any{
			"friendly_name": "Балансировка", "icon": "mdi:scale-balance",
			"phase": st.BalPhase, "cycles": st.BalCycles, "top_delta_mv": st.BalTopMV})
	}()
	switch {
	case !on:
		if st.BalPhase != "" {
			if st.BalPhase != balDone {
				log.Printf("charge: balancing off after %d pulses", st.BalCycles)
			}
			st.BalPhase = ""
			st.save(chargeFile)
		}
		return 0, "", false
	case st.BalPhase == balDone:
		s.balanceOff() // HA ещё не обновил хелпер (или прошлый вызов не дошёл)
		return 0, "", false
	case !auto:
		return 0, "", false // пауза: ручной режим не трогаем
	case st.BalPhase == "":
		st.BalPhase, st.BalCycles, st.BalTopMV, st.BalNote = balCharge, 0, 0, ""
		st.BalSoft, st.BalStart, st.BalRestAt = false, time.Now(), time.Time{}
		st.save(chargeFile)
		log.Printf("charge: balancing on — pulses to %.2f V, pause %.0f min and ≤ %.2f V", t.BalanceStopV, t.BalancePauseMin, t.BalanceResumeV)
	case st.BalStart.IsZero(): // сессия начата до 0.6.54 — дни считаем с сейчас
		st.BalStart = time.Now()
		st.save(chargeFile)
	}
	if st.BalPhase == balRest && st.BalRestAt.IsZero() {
		st.BalRestAt = time.Now()
		st.save(chargeFile)
	}
	// пассивный балансир слабый: дольше BalanceMaxDays батарею у верха не держим
	if d := t.BalanceMaxDays; d > 0 && time.Since(st.BalStart) >= time.Duration(d*24)*time.Hour {
		st.BalPhase = balDone
		st.BalNote = fmt.Sprintf("остановлено через %.0f дн.: разбег на верху %.0f мВ", d, st.BalTopMV)
		log.Printf("charge: balancing stopped — %s", st.BalNote)
		s.balanceOff()
		st.save(chargeFile)
		return 0, "", false
	}
	if st.BalPhase == balRest && pauseWait(st, t, cellOK, bmsOK) == "" {
		st.BalPhase = balCharge
		st.save(chargeFile)
		log.Printf("charge: balancing — pause over, cell max %.3f V: pulse %d", cellMax, st.BalCycles+1)
	}
	// верх — по ячейке или запрету заряда BMS; SOC 100 % не верх: счётчик BMS
	// доходит до 100 % и на плоской части (16.10: 100 % при 3,36 В)
	if bmsZero := known && limit <= 0; st.BalPhase == balCharge && (cellMax >= t.BalanceStopV || bmsZero) {
		st.BalPhase, st.BalTopMV, st.BalRestAt, st.BalSoft = balRest, delta, time.Now(), true
		if cellMax >= t.BalanceStopV { // порция — только настоящий верх; запрет BMS на плоской части — просто пауза
			st.BalCycles++
		}
		log.Printf("charge: balancing — top of pulse %d: cell max %.3f V, delta %.0f mV, BMS limit 0: %v, SOC %.0f%%",
			st.BalCycles, cellMax, delta, bmsZero, soc)
		// разбег судим только на верху колена: ниже он мал при любом балансе
		if s.store.Available(bmsCellDeltaEntity) && delta <= t.BalanceDoneMV && cellMax >= t.BalanceStopV {
			st.BalPhase = balDone
			st.BalNote = fmt.Sprintf("выровнено: разбег на верху %.0f мВ за %d порций", delta, st.BalCycles)
			log.Printf("charge: balancing done — %s", st.BalNote)
			s.balanceOff()
		}
		st.save(chargeFile)
	}
	// батарея разрядилась (пропал свет) — к верху быстро, «Общим лимитом»;
	// минимальный ток снова включится у колена
	if st.BalPhase == balCharge && soc < t.BalanceFastSOC && cellMax < t.BalanceSoftV {
		if st.BalSoft {
			st.BalSoft = false
			st.save(chargeFile)
			log.Printf("charge: balancing — SOC %.0f%%, fast charge to the top", soc)
		}
		return math.Max(chargeMinA, maxA), "balance/fast", true
	}
	// разбег пошёл (или ячейка у колена) — минимальный ток до конца сессии
	// (без возврата: одна запись)
	byDelta := t.BalanceSoftMV > 0 && s.store.Available(bmsCellDeltaEntity) && delta >= t.BalanceSoftMV
	if st.BalPhase == balCharge && !st.BalSoft && (byDelta || t.BalanceSoftV > 0 && cellMax >= t.BalanceSoftV) {
		st.BalSoft = true
		st.save(chargeFile)
		log.Printf("charge: balancing — delta %.0f mV, cell max %.3f V: minimum current", delta, cellMax)
	}
	switch st.BalPhase {
	case balCharge:
		if st.BalSoft {
			return chargeMinA, "balance/soft", true
		}
		return math.Min(maxA, math.Max(chargeMinA, t.FullTailA)), "balance/charge", true
	case balRest:
		return 0, "balance/rest", true
	}
	return 0, "", false
}

// pauseWait — чего ещё ждёт пауза: время, ячейка, разрешение BMS ("" — ничего).
func pauseWait(st *chargeState, t config.ChargeTuning, cellOK, bmsOK bool) string {
	var w []string
	if left := time.Duration(t.BalancePauseMin*float64(time.Minute)) - time.Since(st.BalRestAt); left > 0 {
		w = append(w, fmt.Sprintf("ещё %.0f мин", math.Ceil(left.Minutes())))
	}
	if !cellOK {
		w = append(w, "до ≤"+strings.Replace(fmt.Sprintf("%.2f", t.BalanceResumeV), ".", ",", 1)+" В")
	}
	if !bmsOK {
		w = append(w, "разрешения BMS")
	}
	return strings.Join(w, " / ")
}

func (s *Server) balanceOff() {
	if err := s.client.CallService("input_boolean", "turn_off", map[string]any{"entity_id": chargeBalanceEntity}); err != nil {
		log.Println("charge: balancing helper off:", err)
	}
}

// balanceActive — ток сейчас ведёт балансировка (для fullCharge: сеть/ToU).
func (st *chargeState) balanceActive() bool {
	return st.BalPhase == balCharge || st.BalPhase == balRest
}

// balanceText — статус; amps — фактический ток порции, wait — pauseWait.
func balanceText(st *chargeState, on, auto bool, delta, amps float64, wait string) string {
	switch {
	case st.BalPhase == balDone || !on && st.BalNote != "":
		return st.BalNote
	case !on:
		return "выкл"
	case !auto:
		return "пауза — авто-регулятор выключен"
	case st.BalPhase == balRest:
		if wait != "" {
			wait = ": " + wait
		}
		return fmt.Sprintf("пауза%s · разбег на верху %.0f мВ · порций %d", wait, st.BalTopMV, st.BalCycles)
	}
	return fmt.Sprintf("заряд %.0f А · разбег %.0f мВ · порций %d", amps, delta, st.BalCycles)
}

// exclusive — «Балансировка по верху» и «Полный заряд сейчас» взаимоисключающие:
// выигрывает включённый последним. Возвращает хелпер, который надо выключить ("" — ничего).
func exclusive(prevBal, bal, prevFull, full bool) string {
	switch {
	case !prevBal && bal && full:
		return chargeFullNowEntity
	case !prevFull && full && bal:
		return chargeBalanceEntity
	}
	return ""
}

// exclusiveToggles — шаг exclusive по текущим состояниям хелперов; prev* — с прошлого вызова.
func (s *Server) exclusiveToggles(prevBal, prevFull *bool) {
	bal, full := s.store.On(chargeBalanceEntity), s.store.On(chargeFullNowEntity)
	if off := exclusive(*prevBal, bal, *prevFull, full); off != "" {
		log.Printf("charge: balance and full-now are exclusive — turning off %s", off)
		if err := s.client.CallService("input_boolean", "turn_off", map[string]any{"entity_id": off}); err != nil {
			log.Println("charge: exclusive toggles:", err)
		}
	}
	*prevBal, *prevFull = bal, full
}

// balanceGrid — на заряд порции сеть помогает без прогноза и без SOC (SOC BMS
// у верха врёт: 16.10 — 100 % при 3,36 В). На паузу ToU возвращается: с
// выключенным ToU Deye заряжал от сети ~1 А по своему лимиту и рег. 108 = 0 не
// помогал (19.10 «пауза» дотянула до 3,58+ В, а на 3,60 В BMS объявляет 100 % и
// не даёт заряда до 97 %); цели слотов ToU ниже SOC — сеть не заряжает.
// Две записи 146 на порцию.
func (s *Server) balanceGrid(st *chargeState, charging bool) {
	if !charging || !s.store.On(gridChargeSwitch) { // пауза или «Заряд от сети» выключен вручную — ToU как было
		s.touRestore(st)
		_ = s.client.SetState(chargeFullPlan, "—", map[string]any{"friendly_name": "Заряд: полный до 100 %", "icon": "mdi:battery-clock"})
		return
	}
	// пропал свет — ToU не трогаем: выключенный ToU без сети безвреден, а
	// возврат и повторное выключение — 2 записи 146 на каждое пропадание (17.10)
	if s.store.On("binary_sensor.deye_sun_30k_grid") {
		s.touPatchNow(st, time.Now())
		_ = s.client.SetState(chargeFullPlan, "балансировка: заряд от сети", map[string]any{"friendly_name": "Заряд: полный до 100 %", "icon": "mdi:battery-clock"})
	}
}
