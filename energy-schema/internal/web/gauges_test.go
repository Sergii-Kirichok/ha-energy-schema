package web

import (
	"encoding/json"
	"testing"
	"time"

	"energy-schema/internal/hass"
)

func TestLastFullGrid(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	at := func(m int, s string) hass.HistState {
		return hass.HistState{Time: t0.Add(time.Duration(m) * time.Minute), State: s}
	}
	now := t0.Add(5 * time.Hour)
	since := t0.Add(-time.Hour)
	all := func(m int, s string) [3][]hass.HistState {
		return [3][]hass.HistState{{at(0, "230"), at(m, s)}, {at(0, "231"), at(m, s)}, {at(0, "229"), at(m, s)}}
	}
	cases := []struct {
		name string
		hist [3][]hass.HistState
		want time.Time
	}{
		{"всё время есть", all(30, "232"), now},
		{"пропал в 10:30", all(30, "0"), t0.Add(30 * time.Minute)},
		{"L3 раньше, потом все", [3][]hass.HistState{{at(0, "230"), at(60, "0")}, {at(0, "230"), at(60, "0")}, {at(0, "230"), at(20, "1")}},
			t0.Add(20 * time.Minute)},
		{"вернулся", [3][]hass.HistState{{at(0, "0"), at(90, "230")}, {at(0, "0"), at(90, "230")}, {at(0, "0"), at(90, "230")}}, now},
		{"unavailable не отключение", all(30, "unavailable"), now},
		{"за окно не было", [3][]hass.HistState{{at(0, "0")}, {at(0, "0")}, {at(0, "0")}}, since},
	}
	for _, c := range cases {
		if got := lastFullGrid(c.hist, since, now); !got.Equal(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestGridOffNameAndDays(t *testing.T) {
	now := time.Now()
	if n := gridOffName([]bool{true, true, true}, now, now); n != "Свет есть" {
		t.Fatal(n)
	}
	since := now.Add(-time.Minute)
	if n := gridOffName([]bool{false, false, false}, since, now); n != "Нет света · с "+since.Local().Format("15:04") &&
		n != "Нет света · с "+since.Local().Format("02.01 15:04") {
		t.Fatal(n)
	}
	if n := gridOffName([]bool{true, true, false}, since, now); n[:len("Нет L3")] != "Нет L3" {
		t.Fatal(n)
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
		{"type":"gauge","entity":"sensor.deye_grid_power_kw"},{"type":"gauge","entity":"sensor.deye_pv_power_kw"}]}]}]}`), &cfg)
	if ch, _ := patchGauges(cfg); !ch {
		t.Fatal("gauges not added")
	}
	cards := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[0].(map[string]any)["cards"].([]any)
	if len(cards) != 5 || cards[3].(map[string]any)["entity"] != battDaysEntity || cards[4].(map[string]any)["entity"] != gridOffEntity {
		t.Fatalf("cards: %v", cards)
	}
	if ch, _ := patchGauges(cfg); ch {
		t.Fatal("second patch must be a no-op")
	}
}
