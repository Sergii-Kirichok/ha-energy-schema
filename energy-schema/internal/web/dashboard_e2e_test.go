package web

import (
	"encoding/json"
	"sync"
	"testing"

	"energy-schema/internal/hass"
	"energy-schema/internal/hass/hatest"
)

// fakeDash — состояние фейкового HA: текущий конфиг дашборда и счётчик save.
type fakeDash struct {
	mu    sync.Mutex
	cfg   json.RawMessage
	saves int
}

func newFakeDash(t *testing.T, cfg string) (*Server, *fakeDash) {
	d := &fakeDash{cfg: json.RawMessage(cfg)}
	srv := hatest.NewServer(t, func(req map[string]any) (any, bool, string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		switch req["type"] {
		case "lovelace/config":
			return d.cfg, true, ""
		case "lovelace/config/save":
			d.cfg, _ = json.Marshal(req["config"])
			d.saves++
			return nil, true, ""
		}
		return nil, false, "unknown command"
	})
	return &Server{client: hass.NewClient(srv.URL+"/api", hatest.Token)}, d
}

func (d *fakeDash) snapshot() (int, any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var cfg any
	_ = json.Unmarshal(d.cfg, &cfg)
	return d.saves, cfg
}

func TestEnsureDashboardE2E(t *testing.T) {
	s, d := newFakeDash(t, `{"views":[{"sections":[{"cards":[
	  {"type":"entities","entities":[
	    {"entity":"sensor.deye_sun_30k_battery","name":"Заряд (SOC)"},
	    {"entity":"sensor.deye_sun_30k_battery_soh","name":"Здоровье (SOH)"},
	    "sensor.deye_sun_30k_battery_voltage"]}]}]}]}`)
	s.ensureDashboard("home-energy")
	saves, cfg := d.snapshot()
	if saves != 1 {
		t.Fatalf("saves = %d, want 1", saves)
	}
	ents := findEntitiesCard(cfg)["entities"].([]any)
	if len(ents) != 2+len(dashBMSRows) || entityID(ents[1]) != bmsSOHEntity {
		t.Fatalf("entities = %v", ents)
	}
	for i, r := range dashBMSRows {
		if entityID(ents[1+i]) != r["entity"] {
			t.Errorf("row %d = %v, want %v", i, ents[1+i], r["entity"])
		}
	}
	if entityID(ents[len(ents)-1]) != "sensor.deye_sun_30k_battery_voltage" {
		t.Errorf("tail = %v", ents[len(ents)-1])
	}
	// already patched → idempotent, no second save
	s.ensureDashboard("home-energy")
	if saves, _ := d.snapshot(); saves != 1 {
		t.Errorf("saves after second run = %d, want 1", saves)
	}
}

func TestEnsureDashboardNoCard(t *testing.T) {
	s, d := newFakeDash(t, `{"views":[{"cards":[{"type":"markdown"}]}]}`)
	s.ensureDashboard("home-energy")
	if saves, _ := d.snapshot(); saves != 0 {
		t.Errorf("saves = %d, want 0", saves)
	}
}
