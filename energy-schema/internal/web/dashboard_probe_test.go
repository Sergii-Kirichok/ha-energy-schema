package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// Карточка «Диагностика АКБ» добавляется рядом с батарейной один раз.
func TestPatchProbeCardAppendsOnce(t *testing.T) {
	src := `{"views":[{"cards":[
	  {"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"}]}]}]}`
	var cfg any
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if changed, err := patchProbeCard(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchProbeCard(cfg); changed {
		t.Error("second run must be a no-op")
	}
	card := findCardWith(cfg, chargeProbeEntity)
	if card == nil || card["title"] != "Диагностика АКБ" {
		t.Fatalf("probe card missing: %v", card)
	}
	if n := len(card["entities"].([]any)); n != 7 {
		t.Errorf("probe card rows = %d, want 7", n)
	}
}

// Строки прогона, оставшиеся в карточке «Заряд АКБ» от прежней версии,
// выносятся (мигрируют в свою карточку) — патч их выкидывает.
func TestPatchChargeCardDropsProbeRows(t *testing.T) {
	src := `{"views":[{"cards":[{"type":"entities","show_header_toggle":false,"entities":[
	  {"entity":"input_boolean.energy_schema_charge_auto"},
	  {"entity":"input_boolean.energy_schema_charge_probe","name":"Диагностика: прогон тока"},
	  {"entity":"input_number.energy_schema_probe_high_a","name":"Прогон: ток шага, А"},
	  {"entity":"input_button.energy_schema_charge_apply"},
	  {"entity":"sensor.energy_schema_charge_pending"}]}]}]}`
	var cfg any
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if changed, _ := patchChargeCard(cfg); !changed {
		t.Fatal("must change (probe rows dropped)")
	}
	b, _ := json.Marshal(findCardWith(cfg, dashChargeMarker)["entities"])
	if strings.Contains(string(b), "probe") {
		t.Errorf("probe rows must be gone from the charge card: %s", b)
	}
}
