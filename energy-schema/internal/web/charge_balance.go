package web

import (
	"fmt"
	"log"
	"math"
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
		st.save(chargeFile)
		log.Printf("charge: balancing on — cycles to %.2f V, rest to %.0f %%", t.BalanceStopV, t.BalanceResumeSOC)
	}
	// BMS после «полный» не даёт заряд выше ~97 %: снова заряжаем, только когда лимит > 0
	limit, known := s.store.AttrNum(bmsLimitsEntity, "charge_a"), s.store.Attr(bmsLimitsEntity, "charge_a") != ""
	bmsZero := known && limit <= 0
	if st.BalPhase == balRest && soc <= t.BalanceResumeSOC && known && limit > 0 {
		st.BalPhase = balCharge
		st.save(chargeFile)
		log.Printf("charge: balancing — SOC %.0f%%, charge again (cycle %d)", soc, st.BalCycles+1)
	}
	if cellMax := s.store.Num(bmsCellMaxEntity); st.BalPhase == balCharge && (cellMax >= t.BalanceStopV || bmsZero || soc >= 100) {
		st.BalPhase, st.BalCycles, st.BalTopMV = balRest, st.BalCycles+1, delta
		log.Printf("charge: balancing — top of cycle %d: cell max %.3f V, delta %.0f mV, BMS limit 0: %v, SOC %.0f%%",
			st.BalCycles, cellMax, delta, bmsZero, soc)
		if s.store.Available(bmsCellDeltaEntity) && delta <= t.BalanceDoneMV {
			st.BalPhase = balDone
			st.BalNote = fmt.Sprintf("выровнено: разбег на верху %.0f мВ за %d циклов", delta, st.BalCycles)
			log.Printf("charge: balancing done — %s", st.BalNote)
			s.balanceOff()
		}
		st.save(chargeFile)
	}
	switch st.BalPhase {
	case balCharge:
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
