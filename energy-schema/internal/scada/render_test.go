package scada

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"energy-schema/internal/config"
	"energy-schema/internal/hass"
)

// fixtureStore is a deterministic, branch-exercising snapshot of HA states.
func fixtureStore() *hass.Store {
	m := map[string]string{
		"sensor.sim_contactor": "rybhoz",
		"sensor.sim_inv_grid":  "on",
		"sensor.sim_avr_pos":   "inverter",
		"sensor.sim_avr_link":  "ok",
		"sensor.sim_gen_state": "off",
		"sensor.sim_export":    "off",
		"sensor.sim_green_dir": "import",

		"sensor.deye_sun_30k_load_power":          "2250",
		"sensor.deye_sun_30k_pv1_power":           "3200",
		"sensor.deye_sun_30k_pv2_power":           "3100",
		"sensor.deye_sun_30k_pv3_power":           "2900",
		"sensor.deye_sun_30k_pv1_voltage":         "620",
		"sensor.deye_sun_30k_pv2_voltage":         "618",
		"sensor.deye_sun_30k_pv3_voltage":         "615",
		"sensor.deye_sun_30k_pv1_current":         "5.2",
		"sensor.deye_sun_30k_pv2_current":         "5.0",
		"sensor.deye_sun_30k_pv3_current":         "4.7",
		"sensor.deye_sun_30k_battery_voltage":     "480",
		"sensor.deye_sun_30k_battery_current":     "5",
		"sensor.deye_sun_30k_battery":             "78",
		"sensor.deye_sun_30k_battery_temperature": "25",
		"sensor.deye_sun_30k_battery_soh":         "99.49",
		"sensor.deye_sun_30k_battery_state":       "discharging",
		"sensor.deye_sun_30k_device_fault":        "OK",
		"sensor.deye_sun_30k_device_alarm":        "OK",
		"sensor.deye_sun_30k_device_state":        "Normal",
		"sensor.deye_sun_30k_today_production":    "14",

		"number.deye_sun_30k_battery_shutdown_soc": "15",
		"number.deye_sun_30k_battery_low_soc":      "10",
		"binary_sensor.deye_sun_30k_battery_fault": "off",
		"binary_sensor.deye_sun_30k_battery_alarm": "off",

		"sensor.sim_gen_start_signal":      "off",
		"sensor.sim_gen_coolant_heater":    "off",
		"sensor.sim_gen_coolant_temp":      "20",
		"sensor.sim_gen_time_to_start_min": "5",
		"sensor.sim_gen_oil_remaining_h":   "120",
		"sensor.sim_gen_runtime_h":         "340.5",
	}
	for ph := 1; ph <= 3; ph++ {
		n := strconv.Itoa(ph)
		p := "sensor.sim_ryb_l" + n
		m[p+"_on"] = "on"
		m[p+"_vin"] = "235"
		m[p+"_vout"] = "230"
		m[p+"_step"] = "2"
		m[p+"_load"] = "12"
		m[p+"_vmin"] = "228"
		m[p+"_vmax"] = "240"
		m[p+"_mode"] = "stabilize"
		m[p+"_link"] = "ok"

		g := "sensor.sim_green_l" + n
		m[g+"_on"] = "on"
		m[g+"_v"] = "230"
		m[g+"_a"] = "5"

		gen := "sensor.sim_gen_l" + n
		m[gen+"_v"] = "0"
		m[gen+"_load"] = "0"
	}
	st := hass.NewStore()
	st.Replace(m)
	return st
}

// TestRenderGolden guards the exact SVG output against accidental changes.
// Regenerate after an intentional visual change with:
//
//	UPDATE_GOLDEN=1 go test ./internal/scada/...
func TestRenderGolden(t *testing.T) {
	got := Render(fixtureStore(), config.Default())
	golden := filepath.Join("testdata", "golden.svg")

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden (%d bytes)", len(got))
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v (run with UPDATE_GOLDEN=1 to create)", err)
	}
	if got != string(want) {
		t.Errorf("SVG output changed: got %d bytes, want %d bytes", len(got), len(want))
	}
}
