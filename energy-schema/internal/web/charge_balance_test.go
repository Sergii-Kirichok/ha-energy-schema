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
	if st.Factor == 0 {
		st.Factor = 2 // как после первого тика: 110 = 1 — каналы ×2
	}
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
	wantBal(t, f, &st, balCharge, "balance/charge", "заряд 6 А · разбег 5 мВ · порций 0")
	if p := loadChargeState(chargeFile); p.BalPhase != balCharge || time.Since(p.BalStart) > time.Minute {
		t.Error("phase and session start must be persisted")
	}
}

func TestBalanceChargeToRestOnCellVoltage(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "99", bmsCellMaxEntity: "3.550"}, nil, "100")
	st := recentState()
	st.BalPhase = balCharge
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	wantBal(t, f, &st, balRest, "balance/rest", "пауза: ещё 60 мин / до ≤3,45 В · разбег на верху 80 мВ · порций 1")
	if st.BalCycles != 1 || st.BalTopMV != 80 || !st.BalSoft || time.Since(st.BalRestAt) > time.Minute {
		t.Errorf("pulses %d top %v soft %v pause %v", st.BalCycles, st.BalTopMV, st.BalSoft, st.BalRestAt)
	}
	if p := loadChargeState(chargeFile); p.BalRestAt.IsZero() || !p.BalSoft {
		t.Error("pause start and soft must be persisted")
	}
	if len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Error("80 mV > 40 — not done yet")
	}
}

func TestBalanceRestOnBMSLimitZero(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "97"}, nil, "0")
	st := recentState()
	st.BalPhase = balRest // пауза из старой версии (без начала): отсчёт с сейчас, BMS запретил заряд — ток 0
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	wantBal(t, f, &st, balRest, "balance/rest", "пауза: ещё 60 мин / разрешения BMS · разбег на верху 0 мВ · порций 0")
	st.BalPhase = balCharge
	f.reset()
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	wantBal(t, f, &st, balRest, "balance/rest", "пауза: ещё 60 мин / разрешения BMS · разбег на верху 80 мВ · порций 0")
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
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "100", bmsCellMaxEntity: "3.550",
		bmsCellDeltaEntity: "35"}, nil, "100")
	st := recentState()
	st.BalPhase, st.BalCycles = balCharge, 2
	balTick(t, s, &st) // 3,55 В — верх порции 3, разбег 35 ≤ 40 → выровнено
	const note = "выровнено: разбег на верху 35 мВ за 3 порций"
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

// У верха: минимальный ток (1 в регистре) — по тому, что раньше: разбег
// ≥ 10 мВ или верхняя ячейка ≥ 3,45 В; до конца сессии, без возврата на ток
// хвоста; после паузы следующая порция — сразу минимальный ток.
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
	wantBal(t, f, &st, balCharge, "balance/charge", "заряд 6 А · разбег 8 мВ · порций 0")
	st.BalSoft = true
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "98", bmsCellMaxEntity: "3.380", bmsCellDeltaEntity: "8"}, map[int]int{108: 1}, "100")
	balTick(t, s, &st) // ниже порогов — минимальный ток остаётся
	wantVals(t, "max", f.setValues(chargeMaxEntity))
	wantBal(t, f, &st, balCharge, "balance/soft", "заряд 2 А · разбег 8 мВ · порций 0")
	st.BalPhase, st.BalRestAt = balRest, time.Now().Add(-61*time.Minute) // пауза прошла — следующая порция сразу 2 А
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "97", bmsCellDeltaEntity: "5"}, map[int]int{108: 0}, "100")
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 1)
	wantBal(t, f, &st, balCharge, "balance/soft", "заряд 2 А · разбег 5 мВ · порций 0")
}

// 16.10: BMS досчитала SOC до 100 % при 3,36 В — это не верх: заряд малым
// током продолжается, «выровнено» по разбегу плоской части не ставится. Если
// BMS при этом запретила заряд — отдых, но тоже без «выровнено».
func TestBalanceSOC100OnFlatIsNotTop(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "100", bmsCellMaxEntity: "3.361", bmsCellDeltaEntity: "2"}, map[int]int{108: 3}, "100")
	st := recentState()
	st.BalPhase = balCharge
	balTick(t, s, &st)
	wantBal(t, f, &st, balCharge, "balance/charge", "заряд 6 А · разбег 2 мВ · порций 0")
	if len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Error("must not finish on the flat part")
	}
	s, f = balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "100", bmsCellMaxEntity: "3.361", bmsCellDeltaEntity: "2"}, nil, "0")
	balTick(t, s, &st)
	wantBal(t, f, &st, balRest, "balance/rest", "пауза: ещё 60 мин / разрешения BMS · разбег на верху 2 мВ · порций 0")
	if len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Error("BMS limit 0 on the flat part — rest, not done")
	}
}

// «Выровнено» — только на верху колена: BMS запретила заряд при 3,50 В с
// разбегом 30 мВ — отдых, но не конец балансировки.
func TestBalanceDoneOnlyAtStopV(t *testing.T) {
	s, f := balSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "100", bmsCellMaxEntity: "3.500", bmsCellDeltaEntity: "30"}, nil, "0")
	st := recentState()
	st.BalPhase = balCharge
	balTick(t, s, &st)
	if st.BalPhase != balRest || len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Errorf("phase %q — want rest, not done", st.BalPhase)
	}
}
