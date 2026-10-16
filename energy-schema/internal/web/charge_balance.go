package web

import (
	"fmt"
	"log"
	"math"
	"time"
)

// Балансировка «по верху» (хелпер «Заряд: балансировка», только при включённом
// авто-регуляторе): циклы «заряд током хвоста до BalanceStopV на макс. ячейке
// (или BMS снял лимит заряда, или SOC 100) → отдых без заряда до BalanceResumeSOC и разрешения BMS (лимит заряда > 0)».
// На верху каждого цикла запоминается разбег ячеек; когда он ≤ BalanceDoneMV —
// выровнено, хелпер гасится сам. В заряде сеть помогает так же, как при полном
// заряде (fullCharge: прогноз и ToU), в отдыхе ToU возвращается — дом от АКБ.
const (
	chargeBalanceEntity = "input_boolean.energy_schema_charge_balance"
	chargeBalanceStatus = "sensor.energy_schema_charge_balance_status"
	bmsLimitsEntity     = "sensor.energy_schema_bms_limits"
	bmsCellMaxEntity    = "sensor.energy_schema_bms_cell_max"
	bmsCellDeltaEntity  = "sensor.energy_schema_bms_cell_delta"
	balCharge           = "charge"
	balRest             = "rest"
	balDone             = "done" // выровнено, ждём, пока HA погасит хелпер
)

// balanceTick — шаг цикла балансировки. ok — ток ведёт балансировка (amps, mode);
// иначе — обычная регуляция. Статус публикуется каждый тик.
func (s *Server) balanceTick(st *chargeState, soc, maxA float64, auto bool) (amps float64, mode string, ok bool) {
	t := s.cfg.Charge
	on := s.store.On(chargeBalanceEntity)
	delta := s.store.Num(bmsCellDeltaEntity)
	defer func() {
		_ = s.client.SetState(chargeBalanceStatus, balanceText(st, on, auto, delta, t.BalanceResumeSOC), map[string]any{
			"friendly_name": "Балансировка", "icon": "mdi:scale-balance",
			"phase": st.BalPhase, "cycles": st.BalCycles, "top_delta_mv": st.BalTopMV})
	}()
	switch {
	case !on:
		if st.BalPhase != "" {
			if st.BalPhase != balDone {
				log.Printf("charge: balancing off after %d cycles", st.BalCycles)
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
		st.BalSoft = false
		st.save(chargeFile)
		log.Printf("charge: balancing on — cycles to %.2f V, rest to %.0f %%", t.BalanceStopV, t.BalanceResumeSOC)
	}
	// BMS после «полный» не даёт заряд выше ~97 %: снова заряжаем, только когда лимит > 0
	limit, known := s.store.AttrNum(bmsLimitsEntity, "charge_a"), s.store.Attr(bmsLimitsEntity, "charge_a") != ""
	bmsZero := known && limit <= 0
	if st.BalPhase == balRest && soc <= t.BalanceResumeSOC && known && limit > 0 {
		st.BalPhase = balCharge
		st.BalSoft = false
		st.save(chargeFile)
		log.Printf("charge: balancing — SOC %.0f%%, charge again (cycle %d)", soc, st.BalCycles+1)
	}
	// верх — по ячейке или запрету заряда BMS; SOC 100 % не верх: счётчик BMS
	// доходит до 100 % и на плоской части (16.10: 100 % при 3,36 В)
	if cellMax := s.store.Num(bmsCellMaxEntity); st.BalPhase == balCharge && (cellMax >= t.BalanceStopV || bmsZero) {
		st.BalPhase, st.BalTopMV = balRest, delta
		if cellMax >= t.BalanceStopV { // цикл — только настоящий верх; запрет BMS на плоской части — просто отдых
			st.BalCycles++
		}
		log.Printf("charge: balancing — top of cycle %d: cell max %.3f V, delta %.0f mV, BMS limit 0: %v, SOC %.0f%%",
			st.BalCycles, cellMax, delta, bmsZero, soc)
		// разбег судим только на верху колена (3,60 В): ниже он мал при любом балансе
		if s.store.Available(bmsCellDeltaEntity) && delta <= t.BalanceDoneMV && cellMax >= t.BalanceStopV {
			st.BalPhase = balDone
			st.BalNote = fmt.Sprintf("выровнено: разбег на верху %.0f мВ за %d циклов", delta, st.BalCycles)
			log.Printf("charge: balancing done — %s", st.BalNote)
			s.balanceOff()
		} else if n := int(t.BalanceMaxCycles); n > 0 && st.BalCycles >= n {
			// пассивный балансир слабый: часами у верха батарею не держим
			st.BalPhase = balDone
			st.BalNote = fmt.Sprintf("остановлено после %d циклов: разбег на верху %.0f мВ", st.BalCycles, delta)
			log.Printf("charge: balancing stopped — %s", st.BalNote)
			s.balanceOff()
		}
		st.save(chargeFile)
	}
	// разбег пошёл (или ячейка у колена) — минимальный ток до конца цикла
	// (без возврата: одна запись за цикл)
	cellMax := s.store.Num(bmsCellMaxEntity)
	byDelta := t.BalanceSoftMV > 0 && s.store.Available(bmsCellDeltaEntity) && delta >= t.BalanceSoftMV
	if st.BalPhase == balCharge && !st.BalSoft && (byDelta || t.BalanceSoftV > 0 && cellMax >= t.BalanceSoftV) {
		st.BalSoft = true
		st.save(chargeFile)
		log.Printf("charge: balancing — delta %.0f mV, cell max %.3f V: minimum current to the top", delta, cellMax)
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

func (s *Server) balanceOff() {
	if err := s.client.CallService("input_boolean", "turn_off", map[string]any{"entity_id": chargeBalanceEntity}); err != nil {
		log.Println("charge: balancing helper off:", err)
	}
}

// balanceActive — ток сейчас ведёт балансировка (для fullCharge: сеть/ToU).
func (st *chargeState) balanceActive() bool {
	return st.BalPhase == balCharge || st.BalPhase == balRest
}

func balanceText(st *chargeState, on, auto bool, delta, resume float64) string {
	switch {
	case st.BalPhase == balDone || !on && st.BalNote != "":
		return st.BalNote
	case !on:
		return "выкл"
	case !auto:
		return "пауза — авто-регулятор выключен"
	case st.BalPhase == balRest:
		return fmt.Sprintf("отдых до %.0f %% и разрешения BMS · разбег на верху %.0f мВ · циклов %d", resume, st.BalTopMV, st.BalCycles)
	}
	return fmt.Sprintf("заряд · разбег %.0f мВ · циклов %d", delta, st.BalCycles)
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

// balanceGrid — в цикле балансировки сеть помогает без прогноза и без SOC:
// SOC BMS у верха врёт (16.10: 100 % при 3,36 В, ToU вернулся — заряд встал).
// ToU выкл. на фазу заряда, вернуть на отдыхе — две записи 146 за цикл.
func (s *Server) balanceGrid(st *chargeState, charging bool) {
	if charging && s.store.On("binary_sensor.deye_sun_30k_grid") && s.store.On(gridChargeSwitch) {
		s.touPatchNow(st, time.Now())
		_ = s.client.SetState(chargeFullPlan, "балансировка: заряд от сети", map[string]any{"friendly_name": "Заряд: полный до 100 %", "icon": "mdi:battery-clock"})
		return
	}
	s.touRestore(st)
	_ = s.client.SetState(chargeFullPlan, "—", map[string]any{"friendly_name": "Заряд: полный до 100 %", "icon": "mdi:battery-clock"})
}
