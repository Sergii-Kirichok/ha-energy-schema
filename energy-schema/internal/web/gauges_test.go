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

func TestPatchGauges(t *testing.T) {
	var cfg any
	_ = json.Unmarshal([]byte(`{"views":[{"sections":[{"type":"grid","cards":[{"type":"heading"},
		{"type":"gauge","entity":"sensor.deye_pv_power_kw"},
		{"type":"gauge","entity":"sensor.energy_schema_battery_days"}]},
		{"type":"grid","cards":[{"type":"gauge","entity":"sensor.energy_schema_grid_off_hours"},{"type":"markdown"}]}]}]}`), &cfg)
	cards := func() []any {
		return cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[0].(map[string]any)["cards"].([]any)
	}
	if ch, _ := patchGauges(cfg); !ch {
		t.Fatal("not patched")
	}
	c := cards()
	if len(c) != 6 || c[2].(map[string]any)["type"] != "custom:apexcharts-card" || c[4].(map[string]any)["visibility"] == nil || c[5].(map[string]any)["entity"] != pvTodayEntity {
		t.Fatalf("cards: %v", c)
	}
	if other := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[1].(map[string]any)["cards"].([]any); len(other) != 1 {
		t.Fatalf("old gauge in another section not removed: %v", other)
	}
	// после сохранения в HA числа приходят float64 — повтор всё равно без изменений
	b, _ := json.Marshal(cfg)
	cfg = nil
	_ = json.Unmarshal(b, &cfg)
	if ch, _ := patchGauges(cfg); ch {
		t.Fatal("second patch must be a no-op")
	}
	// устаревший конфиг apexcharts обновляется на месте
	cards()[2].(map[string]any)["apex_config"] = map[string]any{}
	if ch, _ := patchGauges(cfg); !ch || len(cards()) != 6 {
		t.Fatal("stale apex card not refreshed")
	}
}
