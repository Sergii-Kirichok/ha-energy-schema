package web

import (
	"encoding/json"
	"testing"
)

func TestPatchBatteryCard(t *testing.T) {
	src := `{"views":[{"sections":[{"cards":[{"type":"markdown"}]},{"cards":[
	  {"type":"gauge","entity":"sensor.x"},
	  {"type":"entities","entities":[
	    {"entity":"sensor.deye_sun_30k_battery","name":"Заряд (SOC)"},
	    {"entity":"sensor.deye_sun_30k_battery_soh","name":"Здоровье (SOH)"},
	    "sensor.deye_sun_30k_battery_voltage"]}]}]}]}`
	var cfg any
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	changed, err := patchBatteryCard(cfg)
	if err != nil || !changed {
		t.Fatalf("first patch: changed=%v err=%v", changed, err)
	}
	card := findEntitiesCard(cfg)
	ents := card["entities"].([]any)
	// Solarman SOH row is REPLACED by ours (not kept), others inserted after it
	if len(ents) != 2+len(dashBMSRows) {
		t.Fatalf("len = %d", len(ents))
	}
	if entityID(ents[1]) != bmsSOHEntity || entityID(ents[2]) != "sensor.energy_schema_bms_cell_max" || entityID(ents[len(ents)-1]) != "sensor.deye_sun_30k_battery_voltage" {
		t.Errorf("order wrong: %v", ents)
	}
	for _, e := range ents {
		if entityID(e) == dashAnchorEntity {
			t.Error("computed Solarman SOH row must be gone")
		}
	}
	// idempotent
	if changed, err := patchBatteryCard(cfg); err != nil || changed {
		t.Errorf("second patch: changed=%v err=%v", changed, err)
	}
	// no anchor -> error, untouched
	var other any
	_ = json.Unmarshal([]byte(`{"views":[]}`), &other)
	if _, err := patchBatteryCard(other); err == nil {
		t.Error("expected error without battery card")
	}
}
