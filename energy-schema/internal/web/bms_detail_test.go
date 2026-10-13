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

func TestCellStatsRackChange(t *testing.T) {
	c := loadCellStats(filepath.Join(t.TempDir(), "cs.json"))
	c.add(20, "М1·№1", "М1·№2")
	r := map[int]int{10032: 0x3330, 10033: 0x3034, 10034: 0x3031, 10035: 0x3030, 10036: 0x3743, 10037: 0x3332, 10038: 0x3030, 10039: 0x3733}
	if got := rackSerial(r); got != "300401007C320073" {
		t.Fatalf("serial %q", got)
	}
	if c.sameRack(rackSerial(r), 239) || c.Dsg != 0 {
		t.Fatal("file without rack serial → reset once")
	}
	c.add(20, "М1·№1", "М1·№2")
	if !c.sameRack(rackSerial(r), 240) || c.Dsg != 1 {
		t.Fatal("same rack, cycles grow → keep")
	}
	if c.sameRack(rackSerial(r), 0) || c.Dsg != 0 {
		t.Error("cycles dropped 240 → 0 (modules replaced) → reset")
	}
	if c.sameRack("OTHER", 0) {
		t.Error("other BMU serial → reset")
	}
}
