package hass

import (
	"math"
	"testing"
	"time"
)

// fakeClock pins nowFn to a sequence of instants, one per Replace call.
func fakeClock(t *testing.T, ts ...time.Time) {
	t.Helper()
	i := 0
	nowFn = func() time.Time {
		if i >= len(ts) {
			t.Fatalf("Replace called %d times, only %d instants scripted", i+1, len(ts))
		}
		i++
		return ts[i-1]
	}
	t.Cleanup(func() { nowFn = time.Now })
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Two polls 30 s apart at 6000 W integrate 6 kW × (30/3600) h = 0.05 kWh.
// The first poll (no lastTick yet) integrates nothing.
func TestDayEnergyIntegrates(t *testing.T) {
	t0 := time.Date(2026, 6, 10, 12, 0, 0, 0, time.Local)
	fakeClock(t, t0, t0.Add(30*time.Second))
	s := NewStore()
	s.ReplaceStates(map[string]string{"sensor.pv_power": "6000", "sensor.pv_voltage": "6000"})
	if got := s.DayEnergy("sensor.pv_power"); got != 0 {
		t.Fatalf("first poll integrated %v, want 0", got)
	}
	s.ReplaceStates(map[string]string{"sensor.pv_power": "6000", "sensor.pv_voltage": "6000"})
	if got := s.DayEnergy("sensor.pv_power"); !approx(got, 0.05) {
		t.Errorf("DayEnergy = %v, want 0.05", got)
	}
	if got := s.DayEnergy("sensor.pv_voltage"); got != 0 {
		t.Errorf("non-*_power entity integrated %v, want 0", got)
	}
}

// A gap of ≥120 s between polls (restart / lost link) adds nothing, but the
// tick still advances so the next normal-interval poll integrates again.
func TestDayEnergySkipsLargeGap(t *testing.T) {
	t0 := time.Date(2026, 6, 10, 12, 0, 0, 0, time.Local)
	fakeClock(t, t0, t0.Add(121*time.Second), t0.Add(151*time.Second))
	s := NewStore()
	m := map[string]string{"sensor.pv_power": "6000"}
	s.ReplaceStates(m)
	s.ReplaceStates(m)
	if got := s.DayEnergy("sensor.pv_power"); got != 0 {
		t.Fatalf("121 s gap integrated %v, want 0", got)
	}
	s.ReplaceStates(m)
	if got := s.DayEnergy("sensor.pv_power"); !approx(got, 0.05) {
		t.Errorf("after gap, 30 s poll = %v, want 0.05", got)
	}
}

// Crossing local midnight resets today's peak and energy; the rolling 24h
// buffers and last-good values survive.
func TestMidnightResetsDayKeepsRoll(t *testing.T) {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.Local)
	fakeClock(t, midnight.Add(-40*time.Second), midnight.Add(-10*time.Second), midnight.Add(20*time.Second))
	s := NewStore()
	s.ReplaceStates(map[string]string{"sensor.pv_power": "6000"})
	s.ReplaceStates(map[string]string{"sensor.pv_power": "6000"})
	if s.DayMax("sensor.pv_power") != 6000 || !approx(s.DayEnergy("sensor.pv_power"), 0.05) {
		t.Fatalf("before midnight: max=%v energy=%v", s.DayMax("sensor.pv_power"), s.DayEnergy("sensor.pv_power"))
	}
	s.ReplaceStates(map[string]string{"sensor.pv_power": "1000"})
	if got := s.DayMax("sensor.pv_power"); got != 1000 {
		t.Errorf("DayMax after midnight = %v, want 1000 (reset, then new sample)", got)
	}
	// reset to 0, then the 30 s poll at 1000 W integrates 1 kW × 30/3600 h
	if got := s.DayEnergy("sensor.pv_power"); !approx(got, 1000.0/1000/120) {
		t.Errorf("DayEnergy after midnight = %v, want %v", got, 1000.0/1000/120)
	}
	if got := s.Max24h("sensor.pv_power"); got != 6000 {
		t.Errorf("Max24h after midnight = %v, want 6000 (rolling buffer kept)", got)
	}
	if got := s.LastNum("sensor.pv_power"); got != 1000 {
		t.Errorf("LastNum = %v, want 1000", got)
	}
}

// InitDayBoundary stamps today so the first Replace after a restart does not
// wipe peaks/energy seeded from history.
func TestInitDayBoundaryKeepsSeeded(t *testing.T) {
	s := NewStore()
	s.InitDayBoundary()
	s.SetDayEnergy("sensor.pv_power", 12.5)
	s.SeedDayMax("sensor.pv_power", time.Now(), 7000)
	s.ReplaceStates(map[string]string{"sensor.pv_power": "500"})
	if got := s.DayEnergy("sensor.pv_power"); got != 12.5 {
		t.Errorf("seeded DayEnergy wiped: %v", got)
	}
	if got := s.DayMax("sensor.pv_power"); got != 7000 {
		t.Errorf("seeded DayMax wiped: %v", got)
	}
	// without the stamp, the "" → today transition clears the seeds
	u := NewStore()
	u.SetDayEnergy("sensor.pv_power", 12.5)
	u.ReplaceStates(map[string]string{"sensor.pv_power": "500"})
	if got := u.DayEnergy("sensor.pv_power"); got != 0 {
		t.Errorf("unstamped store kept seed: %v", got)
	}
}

// lastGood survives "unavailable"/"unknown" and only moves on the next real
// value; LostInfo reports the outage and clears once the entity is back.
func TestLastGoodAcrossOutage(t *testing.T) {
	s := NewStore()
	s.ReplaceStates(map[string]string{"sensor.grid_power": "-350"})
	s.Replace(map[string]Entity{"sensor.grid_power": {State: "unavailable", LastChanged: time.Now().Add(-90 * time.Minute)}})
	if s.LastState("sensor.grid_power") != "-350" || s.LastNum("sensor.grid_power") != -350 {
		t.Errorf("lastGood after unavailable = %q", s.LastState("sensor.grid_power"))
	}
	if got := s.LostInfo("sensor.grid_power"); got != "1ч 30м" {
		t.Errorf("LostInfo = %q, want '1ч 30м'", got)
	}
	s.Replace(map[string]Entity{"sensor.grid_power": {State: "unknown", LastChanged: time.Now().Add(-30 * time.Second)}})
	if s.LastNum("sensor.grid_power") != -350 {
		t.Error("unknown must not overwrite lastGood")
	}
	if got := s.LostInfo("sensor.grid_power"); got != "<1 мин" {
		t.Errorf("LostInfo = %q, want '<1 мин'", got)
	}
	// unavailable with zero LastChanged (FromStates) → nothing to report
	s.ReplaceStates(map[string]string{"sensor.grid_power": "unavailable"})
	if got := s.LostInfo("sensor.grid_power"); got != "" {
		t.Errorf("LostInfo without LastChanged = %q, want empty", got)
	}
	s.ReplaceStates(map[string]string{"sensor.grid_power": "120"})
	if s.LastNum("sensor.grid_power") != 120 || s.LostInfo("sensor.grid_power") != "" {
		t.Errorf("back online: last=%v lost=%q", s.LastNum("sensor.grid_power"), s.LostInfo("sensor.grid_power"))
	}
}
