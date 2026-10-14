package web

import "testing"

func TestPublishLimit(t *testing.T) {
	f := &haFake{}
	s := newHAFakeServer(t, f)
	s.publishLimit(2, 6, 12, "night/taper", 85, false, true, 1.04, 88)
	state, a, ok := f.state("sensor.energy_schema_charge_setpoint")
	if !ok || state != "12" {
		t.Fatalf("state = %q (want reg108 × channels = 12)", state)
	}
	want := map[string]any{"mode": "night/taper", "target_a": 12.0, "reg108": 6.0, "channels": 2.0, "soc": 85.0,
		"full_due": false, "auto": true, "zero_floor_a": 1.04, "hold_from_soc": 88.0, "unit_of_measurement": "A"}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("attr %s = %v, want %v", k, a[k], v)
		}
	}
	// множитель ещё не прочитан (0) → считаем ×1
	s.publishLimit(0, 7, 7, "manual", 50, false, false, 0, 90)
	if state, a, _ := f.state("sensor.energy_schema_charge_setpoint"); state != "7" || a["channels"] != 1.0 {
		t.Errorf("factor 0: state %q channels %v", state, a["channels"])
	}
}

// Живые значения 03.10: макс №1 в М4, мин №5 в М2, темп. 62/58 (+40).
func detailRegs() map[int]int {
	return map[int]int{regCellMaxN: 1, regCellMaxMod: 4, regCellMinN: 5, regCellMinMod: 2,
		regTempMax: 62, regTempMaxN: 3, regTempMaxMod: 4, regTempMin: 58, regTempMinN: 9, regTempMinMod: 1,
		regChgLimit: 500, regDsgLimit: 1200, regPacks: 5, regOverChg: 2, regOverDsg: 1, regInsulation: 3000}
}

func TestPublishDetail(t *testing.T) {
	f := &haFake{}
	s := newHAFakeServer(t, f)
	s.store.ReplaceStates(map[string]string{"sensor.deye_sun_30k_battery_current": "20"}) // разряд → выборка
	if err := s.publishDetail(detailRegs()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 11 {
		t.Errorf("want 11 sensors, got %d", len(f.calls))
	}
	for id, want := range map[string]string{
		"sensor.energy_schema_bms_cell_max_loc": "М4·№1",
		"sensor.energy_schema_bms_cell_min_loc": "М2·№5",
		"sensor.energy_schema_bms_temp_max":     "22",
		"sensor.energy_schema_bms_temp_min":     "18",
		"sensor.energy_schema_bms_overcharge":   "2",
		"sensor.energy_schema_bms_insulation":   "3000",
		"sensor.energy_schema_bms_limits":       "50 / 120 А",
		"sensor.energy_schema_bms_weak_cell":    "мало данных",
	} {
		if got, _, ok := f.state(id); !ok || got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if _, a, _ := f.state("sensor.energy_schema_bms_temp_max"); a["where"] != "М4·№3" || a["unit_of_measurement"] != "°C" {
		t.Errorf("temp_max attrs = %v", a)
	}
	if _, a, _ := f.state("sensor.energy_schema_bms_temp_min"); a["where"] != "М1·№9" {
		t.Errorf("temp_min where = %v", a["where"])
	}
	if _, a, _ := f.state("sensor.energy_schema_bms_overcharge"); a["over_discharge"] != 1.0 {
		t.Errorf("over_discharge = %v", a["over_discharge"])
	}
	if _, a, _ := f.state("sensor.energy_schema_bms_weak_cell"); a["discharge_samples"] != 1.0 || a["charge_samples"] != 0.0 {
		t.Errorf("weak attrs = %v", a)
	}
}

func TestPublishDetailError(t *testing.T) {
	f := &haFake{fail: "/states/"}
	s := newHAFakeServer(t, f)
	if err := s.publishDetail(detailRegs()); err == nil {
		t.Error("HA error must surface")
	}
}

func TestWithLoc(t *testing.T) {
	a := withLoc(map[string]any{"x": 1}, 3, 12)
	if a["where"] != "М3·№12" || a["x"] != 1 {
		t.Errorf("withLoc = %v", a)
	}
}
