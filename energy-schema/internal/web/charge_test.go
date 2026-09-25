package web

import (
	"path/filepath"
	"testing"
	"time"
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
		{"at target -> hold 1A", func(c *chargeInput) { c.SOC = 90 }, 1, "night/hold"},
		{"above target -> hold 1A", func(c *chargeInput) { c.SOC = 97 }, 1, "night/hold"},
		{"full due 85 -> normal taper", func(c *chargeInput) { c.SOC = 85; c.FullDue = true }, 13, "full/taper"},
		{"full due 90..99 -> trickle 1A", func(c *chargeInput) { c.SOC = 95; c.FullDue = true }, 1, "full/trickle"},
		{"full due 100 -> hold", func(c *chargeInput) { c.SOC = 100; c.FullDue = true }, 1, "full/hold"},
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
		{25, 2, 13}, {16, 2, 8}, {1, 2, 1}, {0, 2, 1}, {5, 1, 5}, {5, 0, 5}, {3, 2, 2},
	}
	for _, c := range cases {
		if got := regValue(c.amps, c.factor); got != c.want {
			t.Errorf("regValue(%v,%v) = %v, want %v", c.amps, c.factor, got, c.want)
		}
	}
}
