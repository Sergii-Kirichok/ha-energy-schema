package web

import (
	"path/filepath"
	"testing"
	"time"

	"energy-schema/internal/hass"
)

func TestChargeSetpoint(t *testing.T) {
	base := chargeInput{MaxA: 25, TaperSOC: 80, TargetSOC: 90, CellLevel: "ok"}
	cases := []struct {
		name string
		mod  func(*chargeInput)
		amps float64
		mode string
	}{
		{"below taper -> max", func(c *chargeInput) { c.SOC = 50 }, 25, "max"},
		{"at taper start -> max", func(c *chargeInput) { c.SOC = 80 }, 25, "night/taper"},
		{"midway 85 -> half", func(c *chargeInput) { c.SOC = 85 }, 13, "night/taper"},
		{"89 -> near min", func(c *chargeInput) { c.SOC = 89 }, 3, "night/taper"},
		{"at target -> hold 0", func(c *chargeInput) { c.SOC = 90 }, 0, "night/hold"},
		{"above target -> hold 0", func(c *chargeInput) { c.SOC = 97 }, 0, "night/hold"},
		{"full due 85 -> normal taper", func(c *chargeInput) { c.SOC = 85; c.FullDue = true }, 13, "full/taper"},
		{"full due 90..99 -> trickle 1A", func(c *chargeInput) { c.SOC = 95; c.FullDue = true }, 1, "full/trickle"},
		{"full due 100 -> hold 0", func(c *chargeInput) { c.SOC = 100; c.FullDue = true }, 0, "full/hold"},
		{"cells bad below 70% ignored", func(c *chargeInput) { c.SOC = 50; c.CellLevel = "bad" }, 25, "max"},
		{"cells bad from 70% caps at 5", func(c *chargeInput) { c.SOC = 75; c.CellLevel = "bad" }, 5, "max/cells"},
		{"cells bad below cap untouched", func(c *chargeInput) { c.SOC = 89; c.CellLevel = "bad" }, 3, "night/taper"},
		{"taper >= target -> max until target", func(c *chargeInput) { c.SOC = 85; c.TaperSOC = 95 }, 25, "max"},
	}
	for _, c := range cases {
		in := base
		c.mod(&in)
		amps, mode := chargeSetpoint(in)
		if amps != c.amps || mode != c.mode {
			t.Errorf("%s: got %.0f A %q, want %.0f A %q", c.name, amps, mode, c.amps, c.mode)
		}
	}
}

func TestChargeStatePersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "charge.json")
	if st := loadChargeState(p); !st.LastFull.IsZero() {
		t.Fatal("missing file must give zero state")
	}
	when := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	(chargeState{LastFull: when}).save(p)
	if st := loadChargeState(p); !st.LastFull.Equal(when) {
		t.Errorf("reloaded %v, want %v", st.LastFull, when)
	}
}

func TestRegValue(t *testing.T) {
	cases := []struct{ amps, factor, want float64 }{
		{25, 2, 13}, {16, 2, 8}, {1, 2, 1}, {0, 2, 0}, {5, 1, 5}, {5, 0, 5}, {3, 2, 2},
	}
	for _, c := range cases {
		if got := regValue(c.amps, c.factor); got != c.want {
			t.Errorf("regValue(%v,%v) = %v, want %v", c.amps, c.factor, got, c.want)
		}
	}
}

func TestHoldWritesZero(t *testing.T) {
	in := chargeInput{MaxA: 25, TaperSOC: 80, TargetSOC: 90, SOC: 91}
	if a, mode := chargeSetpoint(in); a != 0 || mode != "night/hold" {
		t.Errorf("hold = %.0f %q, want 0 night/hold", a, mode)
	}
	if regValue(0, 2) != 0 {
		t.Error("regValue(0) must pass 0 through")
	}
	in.FullDue = true
	if a, _ := chargeSetpoint(in); a != 1 {
		t.Errorf("full trickle must stay 1 A, got %.0f", a)
	}
}

func TestZeroVerdict(t *testing.T) {
	cases := []struct {
		since   time.Duration
		a       float64
		ok      bool
		verdict string
	}{
		{30 * time.Second, 20, true, "wait"},
		{3 * time.Minute, 0, false, "wait"}, // батарея полная / нет солнца — не судим
		{3 * time.Minute, 20, true, "broken"},
		{3 * time.Minute, 0.1, true, "ok"},
		{3 * time.Minute, 2, true, "wait"},
	}
	for _, c := range cases {
		if v := zeroVerdict(c.since, c.a, c.ok); v != c.verdict {
			t.Errorf("zeroVerdict(%v,%.1f) = %s, want %s", c.since, c.a, v, c.verdict)
		}
	}
}

func TestChargeSignature(t *testing.T) {
	s := &Server{store: hass.NewStore()}
	s.store.ReplaceStates(map[string]string{"sensor.deye_sun_30k_battery": "85", "input_number.energy_schema_charge_max_a": "20"})
	a := s.chargeSignature()
	s.store.ReplaceStates(map[string]string{"sensor.deye_sun_30k_battery": "85", "input_number.energy_schema_charge_max_a": "20"})
	if s.chargeSignature() != a {
		t.Error("same inputs must give the same signature")
	}
	s.store.ReplaceStates(map[string]string{"sensor.deye_sun_30k_battery": "85", "input_number.energy_schema_charge_max_a": "19"})
	if s.chargeSignature() == a {
		t.Error("slider change must change the signature")
	}
	s.store.ReplaceStates(map[string]string{"sensor.deye_sun_30k_battery": "86", "input_number.energy_schema_charge_max_a": "19"})
	if s.chargeSignature() == a {
		t.Error("SOC change must change the signature")
	}
}

func TestNightMode(t *testing.T) {
	in := chargeInput{MaxA: 25, TaperSOC: 80, TargetSOC: 90, SOC: 100, Night: true}
	if a, mode := chargeSetpoint(in); a != 25 || mode != "night-ready" {
		t.Errorf("night = %.0f %q, want 25 night-ready", a, mode)
	}
	steps := []struct {
		pv        float64
		wasNight  bool
		wantNight bool
	}{
		{2000, false, false}, {1200, false, false}, {900, false, true}, // вечер: <1 кВт → ночь
		{1200, true, true}, {1500, true, true}, {1600, true, false}, // утро: >1,5 кВт → день
	}
	for _, c := range steps {
		if got := nightHyst(c.pv, c.wasNight); got != c.wantNight {
			t.Errorf("nightHyst(%.0f,%v) = %v", c.pv, c.wasNight, got)
		}
	}
	if pvBucket(900) != "n" || pvBucket(1200) != "m" || pvBucket(2000) != "d" {
		t.Error("pvBucket")
	}
}
