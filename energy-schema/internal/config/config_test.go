package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Refresh != 3 {
		t.Errorf("Refresh = %d, want 3", c.Refresh)
	}
	if c.APIBase != "http://supervisor/core/api" {
		t.Errorf("APIBase = %q", c.APIBase)
	}
	if c.In1Name != "Рыбхоз" || c.In2Name != "Зелёный" {
		t.Errorf("input names = %q / %q", c.In1Name, c.In2Name)
	}
	if c.PVLabels[3] != "Поле 4" {
		t.Errorf("PVLabels = %v", c.PVLabels)
	}
	if c.HomeMax != 30 || c.PVMax != 33 {
		t.Errorf("maxes = %v / %v", c.HomeMax, c.PVMax)
	}
}

func TestLoadMissingFileKeepsDefaultsWithEnvToken(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "nope.json"), "SUPERTOKEN")
	if c.Token != "SUPERTOKEN" {
		t.Errorf("Token = %q, want SUPERTOKEN", c.Token)
	}
	// No LLAT given => API base stays on the Supervisor proxy.
	if c.APIBase != "http://supervisor/core/api" {
		t.Errorf("APIBase = %q, want supervisor", c.APIBase)
	}
	if c.Title != "Энергосистема" {
		t.Errorf("Title = %q", c.Title)
	}
}

func TestLoadOptionsOverlay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "options.json")
	body := `{
	  "refresh_seconds": 7,
	  "ha_url": "http://homeassistant:8123/",
	  "ha_token": "LLAT123",
	  "title": "ДОМ",
	  "in1_name": "",
	  "pv2_label": "Крыша",
	  "batt_capacity_kwh": 45.5,
	  "home_max": 25,
	  "pv_t2": 18
	}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	c := Load(path, "ENVTOK")

	if c.Refresh != 7 {
		t.Errorf("Refresh = %d, want 7", c.Refresh)
	}
	// LLAT present => token overrides env token and APIBase switches to it (trailing slash trimmed).
	if c.Token != "LLAT123" {
		t.Errorf("Token = %q, want LLAT123", c.Token)
	}
	if c.APIBase != "http://homeassistant:8123/api" {
		t.Errorf("APIBase = %q", c.APIBase)
	}
	if c.Title != "ДОМ" {
		t.Errorf("Title = %q", c.Title)
	}
	// Empty option must not clobber the default.
	if c.In1Name != "Рыбхоз" {
		t.Errorf("In1Name = %q, want default", c.In1Name)
	}
	if c.PVLabels[1] != "Крыша" || c.PVLabels[0] != "Поле 1" {
		t.Errorf("PVLabels = %v", c.PVLabels)
	}
	if c.BattCap != 45.5 {
		t.Errorf("BattCap = %v", c.BattCap)
	}
	if c.HomeMax != 25 {
		t.Errorf("HomeMax = %v", c.HomeMax)
	}
	// Untouched threshold keeps its default.
	if c.HomeT1 != 3 {
		t.Errorf("HomeT1 = %v, want default 3", c.HomeT1)
	}
	if c.PVT2 != 18 {
		t.Errorf("PVT2 = %v", c.PVT2)
	}
}
