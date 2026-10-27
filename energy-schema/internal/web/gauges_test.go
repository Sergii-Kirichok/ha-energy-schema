package web

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"energy-schema/internal/hass"
)

func TestAvgPowerKW(t *testing.T) {
	t0 := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Hour) }
	sts := []hass.HistState{{Time: h(-2), State: "1000"}, {Time: h(2), State: "unavailable"},
		{Time: h(3), State: "4000"}, {Time: h(4), State: "1000"}}
	// окно [0, 6]: 1 кВт 2 ч, дыра 1 ч, 4 кВт 1 ч, 1 кВт 2 ч → 8 кВт·ч / 5 ч
	if a, ok := avgPowerKW(sts, t0, h(6)); !ok || math.Abs(a-1.6) > 1e-9 {
		t.Fatalf("avg %v %v", a, ok)
	}
	if _, ok := avgPowerKW(sts[3:], h(5), h(5).Add(30*time.Minute)); ok {
		t.Fatal("under an hour of data must not count")
	}
	if d := battDays(144, 2); d != 3 {
		t.Fatalf("days %v", d)
	}
	if d := battDays(144, 0); d != battDaysMax {
		t.Fatalf("no load %v", d)
	}
}

func sectionCards(cfg any, i int) []any {
	return cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[i].(map[string]any)["cards"].([]any)
}

func roundTrip(cfg any) any {
	b, _ := json.Marshal(cfg)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func TestPatchGaugesFresh(t *testing.T) {
	var cfg any
	_ = json.Unmarshal([]byte(`{"views":[{"sections":[{"type":"grid","cards":[{"type":"heading"},
		{"type":"gauge","entity":"sensor.deye_pv_power_kw"}]},
		{"type":"grid","cards":[{"type":"gauge","entity":"sensor.energy_schema_grid_off_hours"},{"type":"markdown"}]}]}]}`), &cfg)
	if ch, _ := patchGauges(cfg); !ch {
		t.Fatal("not patched")
	}
	c := sectionCards(cfg, 0)
	if len(c) != 4 || c[2].(map[string]any)["type"] != "custom:energy-schema-gauge" || c[3].(map[string]any)["entity"] != pvTodayEntity {
		t.Fatalf("cards: %v", c)
	}
	if len(sectionCards(cfg, 1)) != 1 {
		t.Fatal("outage gauge not removed")
	}
	if ch, _ := patchGauges(roundTrip(cfg)); ch {
		t.Fatal("second patch must be a no-op")
	}
}

// пользователь перенёс apexcharts-копии и «Солнце сегодня» в другую секцию —
// замена на месте, в сетку с «Солнце (PV)» ничего не добавляется
func TestPatchGaugesKeepsUserPlace(t *testing.T) {
	var cfg any
	_ = json.Unmarshal([]byte(`{"views":[{"sections":[{"type":"grid","cards":[{"type":"heading"},
		{"type":"gauge","entity":"sensor.deye_pv_power_kw"}]},
		{"type":"grid","cards":[{"type":"gauge","entity":"sensor.deye_sun_30k_today_production"},
		{"type":"custom:apexcharts-card","series":[{"entity":"sensor.energy_schema_battery_days"}]},
		{"type":"custom:apexcharts-card","series":[{"entity":"sensor.energy_schema_battery_days"}]},
		{"type":"markdown"}]}]}]}`), &cfg)
	if ch, _ := patchGauges(cfg); !ch {
		t.Fatal("not patched")
	}
	if n := len(sectionCards(cfg, 0)); n != 2 {
		t.Fatalf("PV grid got %d cards", n)
	}
	c := sectionCards(cfg, 1)
	if len(c) != 3 || c[1].(map[string]any)["type"] != "custom:energy-schema-gauge" {
		t.Fatalf("cards: %v", c)
	}
	// размер, заданный пользователем, не перетирается
	c[1].(map[string]any)["grid_options"] = map[string]any{"columns": 12}
	cfg = roundTrip(cfg)
	if ch, _ := patchGauges(cfg); ch {
		t.Fatal("user grid_options must be kept")
	}
}
