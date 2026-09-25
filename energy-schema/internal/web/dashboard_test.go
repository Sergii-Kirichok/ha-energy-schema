package web

import (
	"encoding/json"
	"strings"
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

func TestPatchChargeCard(t *testing.T) {
	src := `{"views":[{"sections":[{"cards":[{"type":"markdown"}]},{"cards":[
	  {"type":"gauge","entity":"sensor.x"},
	  {"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"}]}]}]}]}`
	var cfg any
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	changed, err := patchChargeCard(cfg)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	sec := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[1].(map[string]any)
	cards := sec["cards"].([]any)
	if len(cards) != 3 || cards[2].(map[string]any)["title"] != "Заряд АКБ" {
		t.Fatalf("cards = %v", cards)
	}
	if changed, _ := patchChargeCard(cfg); changed {
		t.Error("second patch must be a no-op")
	}
	if b, err := json.Marshal(cfg); err != nil || !strings.Contains(string(b), dashChargeMarker) {
		t.Errorf("marshal: %v %s", err, b)
	}
}

func TestPatchDeltaGauge(t *testing.T) {
	src := `{"views":[{"sections":[{"cards":[
	  {"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"},{"entity":"sensor.energy_schema_bms_cell_delta"}]}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	// the delta ROW in the entities card must not count as the gauge
	if changed, err := patchDeltaGauge(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchDeltaGauge(cfg); changed {
		t.Error("gauge must be added once")
	}
	cards := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[0].(map[string]any)["cards"].([]any)
	if len(cards) != 2 || cards[1].(map[string]any)["type"] != "gauge" {
		t.Errorf("cards = %v", cards)
	}
}
