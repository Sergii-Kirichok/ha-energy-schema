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

func TestPatchBatteryCardDedupesSOH(t *testing.T) {
	src := `{"views":[{"cards":[{"type":"entities","entities":[
	  {"entity":"sensor.deye_sun_30k_battery"},
	  {"entity":"sensor.energy_schema_bms_soh","name":"Здоровье (SOH)"},
	  {"entity":"sensor.energy_schema_bms_soh","name":"Здоровье (SOH, BMS)"},
	  {"entity":"sensor.energy_schema_bms_cell_max"},{"entity":"sensor.energy_schema_bms_cell_min"},
	  {"entity":"sensor.energy_schema_bms_cell_delta"},{"entity":"sensor.energy_schema_bms_balance"},
	  {"entity":"sensor.energy_schema_bms_cycles"}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	changed, err := patchBatteryCard(cfg)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	ents := findEntitiesCard(cfg)["entities"].([]any)
	n := 0
	for _, e := range ents {
		if entityID(e) == bmsSOHEntity {
			n++
		}
	}
	if n != 1 || len(ents) != 7 {
		t.Errorf("soh rows = %d, len = %d: %v", n, len(ents), ents)
	}
	if changed, _ := patchBatteryCard(cfg); changed {
		t.Error("second run must be a no-op")
	}
}

func TestPatchFlowBattery(t *testing.T) {
	src := `{"views":[{"sections":[{"cards":[{"type":"custom:power-flow-card-plus","entities":{"battery":{"entity":"sensor.deye_sun_30k_battery_power","state_of_charge":"sensor.deye_sun_30k_battery"},"grid":{"entity":"sensor.x"}}}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	if changed, err := patchFlowBattery(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchFlowBattery(cfg); changed {
		t.Error("second run must be a no-op")
	}
	b, _ := json.Marshal(cfg)
	if !strings.Contains(string(b), `"entity":"sensor.deye_battery_power_kw","state_of_charge"`) || strings.Contains(string(b), flowBattHalved) {
		t.Errorf("cfg = %s", b)
	}
}

func TestPatchDeltaCleanupAndPowerRow(t *testing.T) {
	src := `{"views":[{"sections":[{"cards":[
	  {"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"},{"entity":"sensor.deye_sun_30k_battery_power","name":"Мощность"}]},
	  {"type":"gauge","entity":"sensor.energy_schema_bms_cell_delta"},
	  {"type":"markdown","content":"{{ states('sensor.energy_schema_bms_cell_delta') }}"},
	  {"type":"conditional","conditions":[],"card":{"type":"tile","entity":"sensor.energy_schema_bms_cell_delta"}},
	  {"type":"conditional","conditions":[],"card":{"type":"tile","entity":"sensor.energy_schema_bms_cell_delta"}}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	if changed, err := patchDeltaCleanup(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchDeltaCleanup(cfg); changed {
		t.Error("second run must be a no-op")
	}
	cards := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[0].(map[string]any)["cards"].([]any)
	if len(cards) != 1 {
		t.Fatalf("only the entities card must remain, got %d", len(cards))
	}
	if _, err := patchBatteryCard(cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cards[0])
	if strings.Contains(string(b), flowBattHalved) || !strings.Contains(string(b), `"entity":"sensor.deye_battery_power_kw","name":"Мощность"`) {
		t.Errorf("power row not switched: %s", b)
	}
}
