package web

import (
	"testing"
	"time"

	"energy-schema/internal/config"
)

// Базовая картина дня: SOC 86 %, общий лимит 24 А, спад 80→90 %, от сети 6 А,
// солнце 5 кВт, авто-регулятор включён; инвертор: 108=10, 110=1 (×2 канала).
func tickSetup(t *testing.T, states map[string]string, regs map[int]int) (*Server, *haFake) {
	t.Helper()
	base := map[string]string{
		"sensor.deye_sun_30k_battery":                  "86",
		"input_number.energy_schema_charge_max_a":      "24",
		"input_number.energy_schema_charge_taper_soc":  "80",
		"input_number.energy_schema_charge_target_soc": "90",
		"input_number.energy_schema_charge_full_days":  "14",
		"input_number.energy_schema_charge_grid_a":     "6",
		"sensor.deye_sun_30k_pv_power":                 "5000",
		chargeAutoHelper:                               "on",
	}
	for k, v := range states {
		base[k] = v
	}
	r := map[int]int{108: 10, 109: 50, 110: 1}
	for k, v := range regs {
		r[k] = v
	}
	f := &haFake{regs: r}
	s := newHAFakeServer(t, f)
	s.cfg.Charge = config.DefaultCharge()
	s.store.ReplaceStates(base)
	return s, f
}

// recentState — LastFull свежий и SOC < 100: chargeTick не пишет /data/charge.json.
func recentState() chargeState { return chargeState{LastFull: time.Now()} }

func runTick(s *Server, st *chargeState) (lastMax, lastGrid float64) {
	lastMax, lastGrid = -1, -1
	s.chargeTick(st, &lastMax, &lastGrid)
	return
}

func wantVals(t *testing.T, what string, got []float64, want ...float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s writes = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s writes = %v, want %v", what, got, want)
		}
	}
}

func TestChargeTickAutoTaper(t *testing.T) {
	s, f := tickSetup(t, nil, nil)
	st := recentState()
	lastMax, lastGrid := runTick(s, &st)
	// до цели — полный лимит 24 А → reg 12 при ×2; от сети 6 А → 3
	wantVals(t, "max", f.setValues(chargeMaxEntity), 12)
	wantVals(t, "grid", f.setValues(chargeGridEntity), 3)
	if st.Factor != 2 || lastMax != 12 || lastGrid != 3 {
		t.Errorf("factor %v lastMax %v lastGrid %v", st.Factor, lastMax, lastGrid)
	}
	state, a, ok := f.state("sensor.energy_schema_charge_setpoint")
	if !ok || state != "24" || a["mode"] != "max" || a["auto"] != true || a["channels"] != 2.0 || a["target_a"] != 24.0 {
		t.Errorf("setpoint = %q %v", state, a)
	}
	nf, _, ok := f.state("sensor.energy_schema_charge_next_full")
	if want := st.LastFull.Add(14 * 24 * time.Hour).Format("2006-01-02"); !ok || nf != want {
		t.Errorf("next_full = %q, want %q", nf, want)
	}
}

func TestChargeTickAutoHoldWritesZero(t *testing.T) {
	s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "95"}, nil)
	st := recentState()
	runTick(s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	if st.ZeroSince.IsZero() {
		t.Error("write of 0 must start the zero self-check")
	}
	if state, a, _ := f.state("sensor.energy_schema_charge_setpoint"); state != "0" || a["mode"] != "night/hold" {
		t.Errorf("setpoint = %q %v", state, a["mode"])
	}
}

func TestChargeTickZeroBrokenFallsBackToOne(t *testing.T) {
	s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "95"}, nil)
	st := recentState()
	st.ZeroBroken = true
	runTick(s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 1)
	if _, a, _ := f.state("sensor.energy_schema_charge_setpoint"); a["mode"] != "night/hold/zero-unsupported" {
		t.Errorf("mode = %v", a["mode"])
	}
}

func TestChargeTickNightReadyWritesMaxA(t *testing.T) {
	for _, c := range []struct {
		reg110 int
		want   float64
	}{{1, 12}, {0, 24}} { // ×2 → 24/2; ×1 → весь лимит
		s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "70", "sensor.deye_sun_30k_pv_power": "0"},
			map[int]int{110: c.reg110})
		st := recentState()
		runTick(s, &st)
		if !st.Night {
			t.Fatal("pv 0 must switch to night")
		}
		wantVals(t, "max", f.setValues(chargeMaxEntity), c.want)
		if _, a, _ := f.state("sensor.energy_schema_charge_setpoint"); a["mode"] != "night-ready" {
			t.Errorf("mode = %v", a["mode"])
		}
	}
}

func TestChargeTickAutoClearsOverrides(t *testing.T) {
	s, f := tickSetup(t, nil, nil)
	s.ovr.set(map[string]bool{"max_charge": true, "grid_a": true})
	st := recentState()
	runTick(s, &st)
	if s.ovr.get("max_charge") || s.ovr.get("grid_a") {
		t.Error("auto on must clear inverter-page manual flags")
	}
	wantVals(t, "max", f.setValues(chargeMaxEntity), 12)
	wantVals(t, "grid", f.setValues(chargeGridEntity), 3)
}

func TestChargeTickManualWritesMaxA(t *testing.T) {
	// SOC 95 в авто дал бы удержание 0; вручную — просто «Общий лимит»
	s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "95", chargeAutoHelper: "off",
		gridChargeSwitch: "on"}, nil)
	st := recentState()
	runTick(s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 12)
	if len(f.find("/services/switch/turn_off")) != 0 {
		t.Error("grid charging switch is manual only — never touched by the regulator")
	}
	if state, a, _ := f.state("sensor.energy_schema_charge_setpoint"); state != "24" || a["mode"] != "manual" || a["auto"] != false {
		t.Errorf("setpoint = %q %v", state, a)
	}
}

func TestChargeTickManualRespectsOverrides(t *testing.T) {
	s, f := tickSetup(t, map[string]string{chargeAutoHelper: "off"}, nil)
	s.ovr.set(map[string]bool{"max_charge": true})
	st := recentState()
	runTick(s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity))
	wantVals(t, "grid", f.setValues(chargeGridEntity), 3)
	if !s.ovr.get("max_charge") {
		t.Error("auto off must keep manual flags")
	}
	f.reset()
	s.ovr.set(map[string]bool{"grid_a": true})
	runTick(s, &st)
	if len(f.find("/services/number/set_value")) != 0 {
		t.Errorf("both overridden → no writes, got %v", f.find("/services/number/set_value"))
	}
}

func TestChargeTickNoWriteWhenInverterMatches(t *testing.T) {
	s, f := tickSetup(t, nil, map[int]int{108: 12, 128: 3})
	st := recentState()
	lastMax, lastGrid := -1.0, 3.0
	s.chargeTick(&st, &lastMax, &lastGrid)
	if n := len(f.find("/services/number/set_value")); n != 0 {
		t.Errorf("register already at target: %d writes", n)
	}
}

func TestChargeTickFullNow(t *testing.T) {
	s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "95", chargeFullNowEntity: "on"}, nil)
	st := recentState()
	runTick(s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 3) // SOC 95 ≥ порога хвоста: 6 А / 2 канала
	if _, a, _ := f.state("sensor.energy_schema_charge_setpoint"); a["mode"] != "full/tail" || a["full_due"] != true {
		t.Errorf("attrs = %v", a)
	}
	if len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Error("full_now button must stay on below 100 %")
	}
}

func TestChargeTickEarlyReturns(t *testing.T) {
	for name, states := range map[string]map[string]string{
		"soc unavailable": {"sensor.deye_sun_30k_battery": "unavailable"},
		"helpers missing": {"input_number.energy_schema_charge_max_a": "0"},
	} {
		s, f := tickSetup(t, states, nil)
		st := recentState()
		runTick(s, &st)
		if len(f.calls) != 0 {
			t.Errorf("%s: unexpected HA calls %v", name, f.calls)
		}
	}
}

func TestChargeTickReadFailure(t *testing.T) {
	s, f := tickSetup(t, nil, nil)
	f.fail = "read_holding_registers"
	st := recentState() // множитель неизвестен → не пишем вовсе
	runTick(s, &st)
	if n := len(f.find("/services/number/set_value")); n != 0 {
		t.Errorf("unknown factor: %d writes", n)
	}
	if _, _, ok := f.state("sensor.energy_schema_charge_setpoint"); ok {
		t.Error("setpoint must not be published without a factor")
	}
	st.Factor = 2 // уже известен с прошлого тика → пишем по памяти
	runTick(s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 12)
}

// 0 уже стоит в инверторе (рестарт аддона): самопроверка стартует без записи;
// в первые 2 минуты вердикт «wait» — ничего не меняется.
func TestChargeTickZeroAlreadyInInverter(t *testing.T) {
	s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "95"}, map[int]int{108: 0})
	st := recentState()
	runTick(s, &st)
	if st.ZeroSince.IsZero() || st.ZeroChecked || st.ZeroBroken {
		t.Errorf("zero check state = %+v", st)
	}
	if len(f.setValues(chargeMaxEntity)) != 0 {
		t.Error("reg108 already 0 — no write expected")
	}
}
