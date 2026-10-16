package web

import (
	"path/filepath"
	"testing"
	"time"

	"energy-schema/internal/hass"
)

// balSetup — балансировка включена; states поверх базовой картины, limitA —
// атрибут charge_a лимитов BMS ("" — сенсора нет). 128=3 уже стоит (6 А / 2).
func balSetup(t *testing.T, states map[string]string, regs map[int]int, limitA string) (*Server, *haFake) {
	t.Helper()
	prev := chargeFile
	chargeFile = filepath.Join(t.TempDir(), "charge.json")
	t.Cleanup(func() { chargeFile = prev })
	base := map[string]string{chargeBalanceEntity: "on", bmsCellMaxEntity: "3.400", bmsCellDeltaEntity: "80"}
	for k, v := range states {
		base[k] = v
	}
	r := map[int]int{128: 3}
	for k, v := range regs {
		r[k] = v
	}
	s, f := tickSetup(t, base, r)
	m := hass.FromStates(tickStates(base))
	if limitA != "" {
		m[bmsLimitsEntity] = hass.Entity{State: limitA + " / 100 А", Attrs: map[string]string{"charge_a": limitA}}
	}
	s.store.Replace(m)
	return s, f
}

func balTick(t *testing.T, s *Server, st *chargeState) {
	t.Helper()
	st.Wrote108 = time.Now() // смена фазы — запись сразу, без chargeMinWriteGap
	runTick(s, st)
}

func wantBal(t *testing.T, f *haFake, st *chargeState, phase, mode, status string) {
	t.Helper()
	if st.BalPhase != phase {
		t.Errorf("phase = %q, want %q", st.BalPhase, phase)
	}
	if _, a, _ := f.state("sensor.energy_schema_charge_setpoint"); mode != "" && a["mode"] != mode {
		t.Errorf("mode = %v, want %q", a["mode"], mode)
	}
	if got, _, _ := f.state(chargeBalanceStatus); got != status {
		t.Errorf("status = %q, want %q", got, status)
	}
}

func TestBalanceStartsWithTailCurrent(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "96", bmsCellDeltaEntity: "5"}, nil, "100")
	st := recentState()
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 3) // хвост 6 А / 2 канала
	wantBal(t, f, &st, balCharge, "balance/charge", "заряд · разбег 5 мВ · циклов 0")
	if loadChargeState(chargeFile).BalPhase != balCharge {
		t.Error("phase must be persisted")
	}
}

func TestBalanceChargeToRestOnCellVoltage(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "99", bmsCellMaxEntity: "3.600"}, nil, "100")
	st := recentState()
	st.BalPhase = balCharge
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	wantBal(t, f, &st, balRest, "balance/rest", "отдых до 97 % и разрешения BMS · разбег на верху 80 мВ · циклов 1")
	if st.BalCycles != 1 || st.BalTopMV != 80 {
		t.Errorf("cycles %d top %v", st.BalCycles, st.BalTopMV)
	}
	if len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Error("80 mV > 40 — not done yet")
	}
}

func TestBalanceRestOnBMSLimitZero(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "97"}, nil, "0")
	st := recentState()
	st.BalPhase = balRest // SOC 97 ≤ 97, но BMS запретил заряд — отдых, ток 0
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	wantBal(t, f, &st, balRest, "balance/rest", "отдых до 97 % и разрешения BMS · разбег на верху 0 мВ · циклов 0")
	st.BalPhase = balCharge
	f.reset()
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	wantBal(t, f, &st, balRest, "balance/rest", "отдых до 97 % и разрешения BMS · разбег на верху 80 мВ · циклов 1")
}

func TestBalanceRestToCharge(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "98"}, map[int]int{108: 0}, "100")
	st := recentState()
	st.BalPhase, st.BalCycles, st.BalTopMV = balRest, 2, 60
	balTick(t, s, &st) // 98 % > 97 — отдыхаем дальше, 0 уже стоит — записей нет
	wantVals(t, "max", f.setValues(chargeMaxEntity))
	wantBal(t, f, &st, balRest, "balance/rest", "отдых до 97 % и разрешения BMS · разбег на верху 60 мВ · циклов 2")
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "96"}, map[int]int{108: 0}, "") // лимит BMS неизвестен — ждём
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity))
	wantBal(t, f, &st, balRest, "balance/rest", "отдых до 97 % и разрешения BMS · разбег на верху 60 мВ · циклов 2")
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "97", bmsCellDeltaEntity: "5"}, map[int]int{108: 0}, "100")
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 3)
	wantBal(t, f, &st, balCharge, "balance/charge", "заряд · разбег 5 мВ · циклов 2")
}

func TestBalanceNoWriteWhenUnchanged(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "97", bmsCellDeltaEntity: "5"}, map[int]int{108: 3}, "100")
	st := recentState()
	st.BalPhase = balCharge
	balTick(t, s, &st)
	if n := len(f.find("/services/number/set_value")); n != 0 {
		t.Errorf("tail current already in the inverter: %d writes", n)
	}
	if n := len(f.find("/services/solarman/write_multiple_holding_registers")); n != 0 {
		t.Errorf("no ToU writes expected: %d", n)
	}
}

func TestBalanceDone(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "100", bmsCellMaxEntity: "3.580",
		bmsCellDeltaEntity: "35"}, nil, "100")
	st := recentState()
	st.BalPhase, st.BalCycles = balCharge, 2
	balTick(t, s, &st) // SOC 100 — верх цикла 3, разбег 35 ≤ 40 → выровнено
	const note = "выровнено: разбег на верху 35 мВ за 3 циклов"
	offs := f.find("/services/input_boolean/turn_off")
	if len(offs) != 1 || offs[0].body["entity_id"] != chargeBalanceEntity {
		t.Fatalf("helper turn_off: %v", offs)
	}
	wantBal(t, f, &st, balDone, "night/hold", note) // обычная регуляция: SOC 100 > цели
	f.reset()
	balTick(t, s, &st) // HA ещё не погасил хелпер — новый цикл не начинается
	wantBal(t, f, &st, balDone, "night/hold", note)
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "100", chargeBalanceEntity: "off"}, nil, "100")
	balTick(t, s, &st)
	wantBal(t, f, &st, "", "night/hold", note)
	if len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Error("helper already off")
	}
}

func TestBalanceOffResetsAndAutoOffPauses(t *testing.T) {
	s, f := balSetup(t, map[string]string{chargeBalanceEntity: "off"}, nil, "")
	st := recentState()
	st.BalPhase, st.BalCycles = balRest, 3
	st.LastMode = "balance/rest" // выход из балансировки — запись сразу
	balTick(t, s, &st)
	wantBal(t, f, &st, "", "max", "выкл") // обычная регуляция: SOC 86 < цели
	wantVals(t, "max", f.setValues(chargeMaxEntity), 12)
	s, f = balSetup(t, map[string]string{chargeAutoHelper: "off"}, nil, "")
	balTick(t, s, &st)
	wantBal(t, f, &st, "", "manual", "пауза — авто-регулятор выключен")
}

// Верх цикла: минимальный ток (1 в регистре) — по тому, что раньше: разбег
// ≥ 10 мВ или верхняя ячейка ≥ 3,45 В; до конца цикла, без возврата на ток
// хвоста; новый цикл заряда — снова ток хвоста.
func TestBalanceSoftTop(t *testing.T) {
	for _, c := range []struct{ cell, delta string }{{"3.460", "5"}, {"3.380", "12"}} { // ячейка раньше, разбег раньше
		s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "98", bmsCellMaxEntity: c.cell, bmsCellDeltaEntity: c.delta}, map[int]int{108: 3}, "100")
		st := recentState()
		st.BalPhase = balCharge
		balTick(t, s, &st)
		wantVals(t, "max", f.setValues(chargeMaxEntity), 1)
		if !st.BalSoft || !loadChargeState(chargeFile).BalSoft {
			t.Errorf("%+v: soft must be set and persisted", c)
		}
	}
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "98", bmsCellMaxEntity: "3.380", bmsCellDeltaEntity: "8"}, map[int]int{108: 3}, "100")
	st := recentState()
	st.BalPhase = balCharge
	balTick(t, s, &st) // 8 мВ и 3,38 В — ещё ток хвоста, записей нет
	wantVals(t, "max", f.setValues(chargeMaxEntity))
	wantBal(t, f, &st, balCharge, "balance/charge", "заряд · разбег 8 мВ · циклов 0")
	st.BalSoft = true
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "98", bmsCellMaxEntity: "3.380", bmsCellDeltaEntity: "8"}, map[int]int{108: 1}, "100")
	balTick(t, s, &st) // ниже порогов — минимальный ток остаётся
	wantVals(t, "max", f.setValues(chargeMaxEntity))
	wantBal(t, f, &st, balCharge, "balance/soft", "заряд · разбег 8 мВ · циклов 0")
	st.BalPhase = balRest
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "97", bmsCellDeltaEntity: "5"}, map[int]int{108: 0}, "100")
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 3)
	wantBal(t, f, &st, balCharge, "balance/charge", "заряд · разбег 5 мВ · циклов 0")
}

// Предел циклов: верх 5-го цикла без «выровнено» — балансировка выключается
// сама; верх 4-го — ещё отдых и следующий цикл.
func TestBalanceMaxCycles(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "99", bmsCellMaxEntity: "3.600"}, nil, "100")
	st := recentState()
	st.BalPhase, st.BalCycles = balCharge, 3
	balTick(t, s, &st)
	wantBal(t, f, &st, balRest, "balance/rest", "отдых до 97 % и разрешения BMS · разбег на верху 80 мВ · циклов 4")
	if len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Error("cycle 4 of 5 — keep going")
	}
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "99", bmsCellMaxEntity: "3.600"}, nil, "100")
	st.BalPhase = balCharge
	balTick(t, s, &st)
	offs := f.find("/services/input_boolean/turn_off")
	if st.BalPhase != balDone || len(offs) != 1 || offs[0].body["entity_id"] != chargeBalanceEntity {
		t.Fatalf("phase %q, turn_off %v", st.BalPhase, offs)
	}
	if want := "остановлено после 5 циклов: разбег на верху 80 мВ"; st.BalNote != want {
		t.Errorf("note = %q, want %q", st.BalNote, want)
	}
}
