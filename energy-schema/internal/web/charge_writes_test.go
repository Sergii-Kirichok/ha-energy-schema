package web

import (
	"path/filepath"
	"testing"
	"time"

	"energy-schema/internal/hass"
)

// Записи в энергонезависимую память инвертора: некритичная смена ступени — не
// чаще chargeMinWriteGap, стоп (0) и смена день/ночь — сразу; счётчик за сутки.
func TestChargeTickMinWriteGap(t *testing.T) {
	s, f := tickSetup(t, nil, map[int]int{128: 3}) // SOC 86 → полный лимит 24 А (reg 12), в инверторе 10; 128 уже 3
	st := recentState()
	st.Wrote108, st.LastMode = time.Now().Add(-time.Minute), "night/taper"
	runTick(s, &st)
	wantVals(t, "max (within gap)", f.setValues(chargeMaxEntity))
	st.Wrote108 = time.Now().Add(-chargeMinWriteGap - time.Second)
	runTick(s, &st)
	wantVals(t, "max (gap passed)", f.setValues(chargeMaxEntity), 12)
	if total, by := s.writes.today(); total != 1 || by["reg108"] != 1 {
		t.Errorf("writes today = %d %v", total, by)
	}
}

func TestChargeTickHoldIsUrgent(t *testing.T) {
	s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_battery": "92"}, nil)
	st := recentState()
	st.Wrote108, st.LastMode = time.Now(), "night/taper"
	runTick(s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 0)
	if !st.Holding {
		t.Error("hold must be remembered for hysteresis")
	}
}

func TestChargeTickCloudyDayIsNotNight(t *testing.T) {
	s, f := tickSetup(t, map[string]string{"sensor.deye_sun_30k_pv_power": "800"}, nil)
	m := hass.FromStates(map[string]string{
		"sensor.deye_sun_30k_battery": "86", "input_number.energy_schema_charge_max_a": "24",
		"input_number.energy_schema_charge_taper_soc": "80", "input_number.energy_schema_charge_target_soc": "90",
		"input_number.energy_schema_charge_full_days": "14", "input_number.energy_schema_charge_grid_a": "6",
		"sensor.deye_sun_30k_pv_power": "800", chargeAutoHelper: "on", "sun.sun": "above_horizon"})
	sun := m["sun.sun"]
	sun.Attrs = map[string]string{"elevation": "25"}
	m["sun.sun"] = sun
	s.store.Replace(m)
	st := recentState()
	runTick(s, &st)
	if st.Night {
		t.Fatal("PV 0.8 kW at sun 25° is a cloudy day, not night")
	}
	wantVals(t, "max", f.setValues(chargeMaxEntity), 12)
	sun.Attrs["elevation"] = "4"
	m["sun.sun"] = sun
	s.store.Replace(m)
	runTick(s, &st)
	if !st.Night {
		t.Error("low PV and sun 4° → night")
	}
}

func TestSettled(t *testing.T) {
	prev := inputSettle
	inputSettle = 50 * time.Millisecond
	t.Cleanup(func() { inputSettle = prev })
	var st chargeState
	if st.settled("grid", 5) {
		t.Fatal("first sight is not settled")
	}
	if st.settled("grid", 6) {
		t.Fatal("changed value restarts the wait")
	}
	time.Sleep(60 * time.Millisecond)
	if !st.settled("grid", 6) {
		t.Error("unchanged for inputSettle → settled")
	}
}

func TestWriteLogPersistsAndRolls(t *testing.T) {
	p := filepath.Join(t.TempDir(), "w.json")
	w := loadWriteLog(p)
	w.add("reg108", 2)
	w.add("switch", 1)
	if total, _ := loadWriteLog(p).today(); total != 3 {
		t.Fatalf("persisted total = %d", total)
	}
	w.Day = "2000-01-01" // вчера и раньше — обнуляется
	if total, _ := w.today(); total != 0 {
		t.Errorf("new day must reset, got %d", total)
	}
	var nilLog *writeLog
	nilLog.add("x", 1) // без паники
}
