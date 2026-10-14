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

func TestSocLimitsFreshFirst(t *testing.T) {
	st := hass.NewStore()
	st.ReplaceStates(map[string]string{"number.deye_sun_30k_battery_shutdown_soc": "20", "number.deye_sun_30k_battery_low_soc": "25"})
	if sd, lo := socLimits(st); sd != 20 || lo != 25 {
		t.Fatalf("solarman fallback: %v %v", sd, lo)
	}
	st.SetVirtual(InvShutdownSOC, "25")
	st.SetVirtual(InvLowSOC, "30")
	if sd, lo := socLimits(st); sd != 25 || lo != 30 {
		t.Errorf("fresh registers must win: %v %v", sd, lo)
	}
}

func TestDimCol(t *testing.T) {
	if got := dimCol("#ffffff"); got != "#65676d" {
		t.Errorf("dimCol(white) = %s", got)
	}
	if got := dimCol("bad"); got != "#23272f" {
		t.Errorf("bad input → track colour, got %s", got)
	}
}

func TestChargeTarget(t *testing.T) {
	st := hass.NewStore()
	st.ReplaceStates(map[string]string{"input_number.energy_schema_charge_target_soc": "80",
		"input_boolean.energy_schema_charge_auto": "on", "input_boolean.energy_schema_charge_full_now": "off"})
	if got := chargeTarget(st); got != 80 {
		t.Errorf("auto on, no full charge → night target, got %v", got)
	}
	st.ReplaceStates(map[string]string{"input_number.energy_schema_charge_target_soc": "80",
		"input_boolean.energy_schema_charge_auto": "on", "input_boolean.energy_schema_charge_full_now": "on"})
	if got := chargeTarget(st); got != 100 {
		t.Errorf("full charge now → 100, got %v", got)
	}
}

func TestBattHours(t *testing.T) {
	for _, c := range []struct {
		h, load float64
		want    string
	}{
		{5.5, 2, "≈ 5 ч 30 м"},
		{47.75, 2, "≈ 47 ч 45 м"},
		{52, 2, "≈ 2 д 4 ч"},
		{99, 0.01, "≈ —"},
	} {
		if got := battHours(c.h, c.load); got != c.want {
			t.Errorf("battHours(%v, %v) = %q, want %q", c.h, c.load, got, c.want)
		}
	}
}
