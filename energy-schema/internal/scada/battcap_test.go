package scada

import (
	"testing"

	"energy-schema/internal/hass"
)

func TestBattCapKWh(t *testing.T) {
	st := hass.NewStore()
	if k, src := BattCapKWh(st, 60); k != 60 || src != "config" {
		t.Fatalf("no data → fallback, got %v %s", k, src)
	}
	m := hass.FromStates(map[string]string{BattAhEntity: "315", bmsLimitsEntity: "88 / 163 А"})
	e := m[bmsLimitsEntity]
	e.Attrs = map[string]string{"packs": "12"}
	m[bmsLimitsEntity] = e
	st.Replace(m)
	if k, src := BattCapKWh(st, 60); src != "inverter" || k < 193.5 || k > 193.6 {
		t.Errorf("315 Ah × 12 × 51.2 V = 193.5 kWh, got %v %s", k, src)
	}
}

func TestInvToAvrOn(t *testing.T) {
	for _, c := range []struct {
		pos  string
		load float64
		want bool
	}{{"inverter", 0, true}, {"reserve", 3, false}, {"", 3.35, true}, {"unavailable", 0.01, false}} {
		if got := invToAvrOn(c.pos, c.load); got != c.want {
			t.Errorf("invToAvrOn(%q, %v) = %v", c.pos, c.load, got)
		}
	}
}
