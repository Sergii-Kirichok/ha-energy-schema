package scada

import (
	"strconv"
	"testing"

	"energy-schema/internal/hass"
)

func storeFrom(m map[string]string) *hass.Store {
	s := hass.NewStore()
	s.ReplaceStates(m)
	return s
}

func rybMap(on1, on2, on3 bool) map[string]string {
	m := map[string]string{}
	for i, v := range []bool{on1, on2, on3} {
		if v {
			m["sensor.sim_ryb_l"+strconv.Itoa(i+1)+"_on"] = "on"
		}
	}
	return m
}

func TestRybLineState(t *testing.T) {
	if got := rybLineState(storeFrom(rybMap(false, false, false))); got != "off" {
		t.Errorf("all off -> %q, want off", got)
	}
	if got := rybLineState(storeFrom(rybMap(true, false, false))); got != "bad" {
		t.Errorf("partial -> %q, want bad", got)
	}
	if got := rybLineState(storeFrom(rybMap(true, true, true))); got != "on" {
		t.Errorf("all on -> %q, want on", got)
	}
}

func TestGreenLineState(t *testing.T) {
	off := map[string]string{}
	if got := greenLineState(storeFrom(off)); got != "off" {
		t.Errorf("all off -> %q, want off", got)
	}

	// On but no voltage anywhere -> bad.
	noV := map[string]string{}
	for ph := 1; ph <= 3; ph++ {
		noV["sensor.sim_green_l"+strconv.Itoa(ph)+"_on"] = "on"
	}
	if got := greenLineState(storeFrom(noV)); got != "bad" {
		t.Errorf("on/no-voltage -> %q, want bad", got)
	}

	// All three on with valid voltage -> on.
	good := map[string]string{}
	for ph := 1; ph <= 3; ph++ {
		good["sensor.sim_green_l"+strconv.Itoa(ph)+"_on"] = "on"
		good["sensor.sim_green_l"+strconv.Itoa(ph)+"_v"] = "230"
	}
	if got := greenLineState(storeFrom(good)); got != "on" {
		t.Errorf("all on+voltage -> %q, want on", got)
	}

	// Two phases only -> bad even with voltage.
	two := map[string]string{
		"sensor.sim_green_l1_on": "on", "sensor.sim_green_l1_v": "230",
		"sensor.sim_green_l2_on": "on", "sensor.sim_green_l2_v": "230",
	}
	if got := greenLineState(storeFrom(two)); got != "bad" {
		t.Errorf("two phases -> %q, want bad", got)
	}
}

func TestRybPhase(t *testing.T) {
	on := storeFrom(map[string]string{"sensor.sim_ryb_l1_on": "on"})
	if got := rybPhase(on, 1, true); got != "on" {
		t.Errorf("energized -> %q, want on", got)
	}
	// датчик молчит, но инвертор видит напряжение (контактор на Рыбхозе) -> потеря связи
	lost := storeFrom(map[string]string{"sensor.deye_sun_30k_grid_l1_voltage": "226"})
	if got := rybPhase(lost, 1, true); got != "lost" {
		t.Errorf("off+inverter -> %q, want lost", got)
	}
	// датчик молчит и инвертор не подтверждает -> реальный обрыв
	brk := storeFrom(map[string]string{"sensor.deye_sun_30k_grid_l1_voltage": "0"})
	if got := rybPhase(brk, 1, true); got != "bad" {
		t.Errorf("off+no-inverter -> %q, want bad", got)
	}
	// инвертор видит напряжение, но контактор НЕ на Рыбхозе -> верить нельзя -> обрыв
	if got := rybPhase(lost, 1, false); got != "bad" {
		t.Errorf("off+inverter+notRyb -> %q, want bad", got)
	}
}

func TestStabOut(t *testing.T) {
	on := storeFrom(map[string]string{"sensor.sim_ryb_l2_on": "on"})
	if got := stabOut(on, 2, true); got != "on" {
		t.Errorf("on -> %q, want on", got)
	}
	if got := stabOut(storeFrom(map[string]string{}), 2, true); got != "off" {
		t.Errorf("break -> %q, want off (output de-energized)", got)
	}
	lost := storeFrom(map[string]string{"sensor.deye_sun_30k_grid_l2_voltage": "226"})
	if got := stabOut(lost, 2, true); got != "lost" {
		t.Errorf("lost -> %q, want lost", got)
	}
}

func TestPhCol(t *testing.T) {
	s := storeFrom(map[string]string{
		"on.ok":   "on",
		"on.low":  "on",
		"on.high": "on",
		"off.x":   "off",
		"v.ok":    "230",
		"v.low":   "180",
		"v.high":  "260",
	})
	if got := phCol(s, "off.x", "v.ok", 200, 250); got != cRed {
		t.Errorf("off -> %q, want red", got)
	}
	if got := phCol(s, "on.ok", "v.ok", 200, 250); got != cGrn {
		t.Errorf("in range -> %q, want green", got)
	}
	if got := phCol(s, "on.low", "v.low", 200, 250); got != cOrg {
		t.Errorf("under range -> %q, want orange", got)
	}
	if got := phCol(s, "on.high", "v.high", 200, 250); got != cOrg {
		t.Errorf("over range -> %q, want orange", got)
	}
}
