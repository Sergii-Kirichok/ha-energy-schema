package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// Существующая карточка в старом порядке: переставляется по шаблону, свои имена
// и строки пользователя сохраняются (свои — перед последними двумя), простая строка
// авто-регулятора становится парой условных; повтор — no-op.
func TestPatchChargeCardReorders(t *testing.T) {
	src := `{"views":[{"cards":[{"type":"entities","show_header_toggle":false,"entities":[
	  {"entity":"input_boolean.energy_schema_charge_auto","name":"Мой авто"},
	  {"entity":"sensor.my_extra","name":"Своя"},
	  {"entity":"sensor.energy_schema_charge_setpoint","name":"Ток"},
	  {"entity":"sensor.energy_schema_charge_pending"},
	  {"entity":"input_button.energy_schema_charge_apply"},
	  {"type":"section"},
	  {"entity":"input_boolean.energy_schema_charge_balance"}]}]}]}`
	var cfg any
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if changed, err := patchChargeCard(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchChargeCard(cfg); changed {
		t.Error("second run must be a no-op")
	}
	ents := findCardWith(cfg, dashChargeMarker)["entities"].([]any)
	tmpl := dashChargeCard["entities"].([]any)
	if len(ents) != len(tmpl)+2 {
		t.Fatalf("len %d, want %d", len(ents), len(tmpl)+2)
	}
	n := len(tmpl) - 2
	for i := 0; i < n; i++ {
		if rowKey(ents[i]) != rowKey(tmpl[i]) {
			t.Errorf("row %d = %s, want %s", i, rowKey(ents[i]), rowKey(tmpl[i]))
		}
	}
	if entityID(ents[n]) != "sensor.my_extra" || ents[n+1].(map[string]any)["type"] != "section" {
		t.Errorf("user rows misplaced: %v %v", ents[n], ents[n+1])
	}
	if entityID(ents[n+2]) != chargeApplyBt || entityID(ents[n+3]) != chargePending {
		t.Errorf("last two = %s, %s", entityID(ents[n+2]), entityID(ents[n+3]))
	}
	b, _ := json.Marshal(ents)
	if !strings.Contains(string(b), `"name":"Мой авто"`) || !strings.Contains(string(b), `"name":"Ток"`) ||
		strings.Count(string(b), `"type":"conditional"`) != 2 || strings.Contains(string(b), `"Авто-регулятор"`) {
		t.Errorf("names / conditional rows: %s", b)
	}
	off := ents[1].(map[string]any)
	if off["row"].(map[string]any)["name"] != "Мой авто" || !strings.Contains(rowKey(off), "=off") {
		t.Errorf("custom name must stay in the off variant: %v", off)
	}
}

// Уже условные строки: карточка находится по маркеру, ничего не дублируется.
func TestPatchChargeCardConditionalIdempotent(t *testing.T) {
	var cfg any
	b, _ := json.Marshal(map[string]any{"views": []any{map[string]any{"cards": []any{dashChargeCard}}}})
	_ = json.Unmarshal(b, &cfg)
	if findCardWith(cfg, dashChargeMarker) == nil {
		t.Fatal("marker lookup must see the conditional row")
	}
	if changed, _ := patchChargeCard(cfg); changed {
		t.Error("template card must be a no-op")
	}
}
