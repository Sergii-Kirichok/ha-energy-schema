package web

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Живые значения 03.10: макс ячейка №1 в модуле 4, мин №5 в модуле 2, темп. 62/58 (+40).
func TestBMSDetailDecode(t *testing.T) {
	if cellLoc(4, 1) != "М4·№1" || tempC(62) != 22 || tempC(58) != 18 {
		t.Errorf("cellLoc/tempC: %s %v %v", cellLoc(4, 1), tempC(62), tempC(58))
	}
}

func TestCellStats(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cs.json")
	c := loadCellStats(p)
	for i := 0; i < 40; i++ {
		c.add(20, "М2·№5", "М4·№1") // разряд: считаем минимальную
	}
	for i := 0; i < 10; i++ {
		c.add(20, "М7·№3", "М4·№1")
	}
	c.add(-20, "М2·№5", "М9·№9") // заряд: считаем максимальную
	c.add(1, "М1·№1", "М1·№1")   // покой — не считаем
	weak, a := c.summary()
	if !strings.HasPrefix(weak, "М2·№5 — 80%") {
		t.Errorf("weak = %q", weak)
	}
	if a["discharge_samples"] != 50 || a["charge_samples"] != 1 {
		t.Errorf("samples = %v / %v", a["discharge_samples"], a["charge_samples"])
	}
	c.saved = c.saved.AddDate(-1, 0, 0)
	c.add(20, "М2·№5", "М4·№1") // сохранение
	if got := loadCellStats(p); got.MinDsg["М2·№5"] != 41 {
		t.Errorf("persisted %v", got.MinDsg)
	}
}

func TestPatchBMSCards(t *testing.T) {
	var cfg any
	_ = json.Unmarshal([]byte(`{"views":[{"sections":[{"cards":[{"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"}]}]}]}]}`), &cfg)
	if ch, err := patchBMSCards(cfg); err != nil || !ch {
		t.Fatalf("changed=%v err=%v", ch, err)
	}
	if ch, _ := patchBMSCards(cfg); ch {
		t.Error("second run must be a no-op")
	}
	b, _ := json.Marshal(cfg)
	if !strings.Contains(string(b), dashBMSMarker) || !strings.Contains(string(b), "custom:apexcharts-card") {
		t.Errorf("cards missing: %s", b)
	}
}

func TestPatchBMSTable(t *testing.T) {
	var cfg any
	_ = json.Unmarshal([]byte(`{"views":[{"sections":[{"cards":[{"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"}]}]}]}]}`), &cfg)
	if ch, err := patchBMSTable(cfg); err != nil || !ch {
		t.Fatalf("changed=%v err=%v", ch, err)
	}
	if ch, _ := patchBMSTable(cfg); ch {
		t.Error("second run must be a no-op")
	}
	b, _ := json.Marshal(cfg)
	for _, w := range []string{"SOC | SOH | Циклы | Ячейки | t | Δ", "energy_schema_bms_cell_delta"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("table lacks %q", w)
		}
	}
}
