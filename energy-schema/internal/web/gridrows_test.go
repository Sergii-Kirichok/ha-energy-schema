package web

import (
	"encoding/json"
	"strings"
	"testing"

	"energy-schema/internal/hass"
)

func TestGridRowText(t *testing.T) {
	s := &Server{store: hass.NewStore()}
	s.store.ReplaceStates(map[string]string{
		"sensor.deye_sun_30k_grid_l1_power":   "40",
		"sensor.deye_sun_30k_grid_l1_voltage": "237.3",
		"sensor.deye_sun_30k_grid_l2_power":   "52",
		"sensor.deye_sun_30k_grid_l2_voltage": "unavailable",
	})
	if txt, _, _, ok := s.gridRowText(1); !ok || txt != "40 W · 237.3 V" {
		t.Errorf("L1 = %q ok=%v", txt, ok)
	}
	if _, _, _, ok := s.gridRowText(2); ok {
		t.Error("L2 with unavailable voltage must be !ok")
	}
	if _, _, _, ok := s.gridRowText(3); ok {
		t.Error("L3 missing must be !ok")
	}
}

func TestPatchGridRows(t *testing.T) {
	src := `{"views":[{"sections":[{"cards":[{"type":"entities","entities":[
	  {"entity":"sensor.deye_sun_30k_grid_power","name":"Мощность сети"},
	  {"entity":"sensor.deye_sun_30k_grid_l1_power","name":"L1"},
	  {"entity":"sensor.deye_sun_30k_grid_l2_power","name":"L2"},
	  "sensor.deye_sun_30k_grid_l3_power"]}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	if changed, err := patchGridRows(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchGridRows(cfg); changed {
		t.Error("second run must be a no-op")
	}
	b, _ := json.Marshal(cfg)
	for _, want := range []string{`"entity":"sensor.energy_schema_grid_l1","name":"L1"`, `"entity":"sensor.energy_schema_grid_l3","name":"L3"`, `"sensor.deye_sun_30k_grid_power"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("cfg lacks %s: %s", want, b)
		}
	}
	if strings.Contains(string(b), "grid_l2_power") {
		t.Error("phase power rows must be replaced")
	}
}

func TestPatchGridCard(t *testing.T) {
	// прежняя markdown-версия карточки конвертируется обратно в entities
	src := `{"views":[{"sections":[{"cards":[{"type":"heading","heading":"Сеть и фазы"},
	  {"type":"markdown","visibility":[{"condition":"screen"}],
	   "content":"x {% set g = 'sensor.energy_schema_grid_l' ~ n %} y"},
	  {"type":"tile","entity":"x.y"}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	if changed, err := patchGridCard(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchGridCard(cfg); changed {
		t.Error("second run must be a no-op")
	}
	if changed, _ := patchGridRows(cfg); changed {
		t.Error("grid rows patch must not touch the converted card")
	}
	cards := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[0].(map[string]any)["cards"].([]any)
	c := cards[1].(map[string]any)
	if len(cards) != 3 || c["type"] != "entities" || c["content"] != nil || c["visibility"] == nil {
		t.Fatalf("card not converted in place: %v", cards)
	}
	ents := c["entities"].([]any)
	if len(ents) != 5 {
		t.Fatalf("want 5 rows, got %d", len(ents))
	}
	l1 := ents[2].(map[string]any)
	if l1["entity"] != "sensor.energy_schema_grid_l1" || l1["name"] != nil {
		t.Errorf("L1 row = %v (name must stay friendly_name)", l1)
	}
	tap := l1["tap_action"].(map[string]any)
	if tap["action"] != "more-info" || tap["entity"] != "sensor.energy_schema_grid_outage_l1" {
		t.Errorf("L1 tap_action = %v, want more-info of the outage sensor", tap)
	}
	// card-mod красит иконку по напряжению фазы
	style := l1["card_mod"].(map[string]any)["style"].(string)
	if !strings.Contains(style, "sensor.deye_sun_30k_grid_l1_voltage") || !strings.Contains(style, "--paper-item-icon-color") {
		t.Errorf("card_mod style lacks the voltage colour rule: %s", style)
	}
}
