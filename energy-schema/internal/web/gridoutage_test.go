package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"energy-schema/internal/hass"
)

func TestOutageStatsOf(t *testing.T) {
	since := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	now := since.Add(24 * time.Hour)
	at := func(d time.Duration) time.Time { return since.Add(d) }
	h, m := time.Hour, time.Minute
	type pt struct {
		t time.Time
		s string
	}
	cases := []struct {
		name               string
		sts                []pt
		text               string
		offStart, offEnd   time.Time
		offCount, lowCount int
	}{
		{"no outages", []pt{{at(0), "230"}}, "норма", time.Time{}, time.Time{}, 0, 0},
		{"one in progress", []pt{{at(0), "230"}, {now.Add(-35 * m), "0"}}, "нет 35 мин ×1",
			now.Add(-35 * m), time.Time{}, 1, 0},
		{"several", []pt{{at(0), "230"}, {at(h), "0"}, {at(h + 30*m), "230"}, {at(2 * h), "0.4"},
			{at(3 * h), "231"}, {at(5 * h), "0"}, {at(5*h + 15*m), "229"}}, "нет 1 ч 45 мин ×3",
			at(5 * h), at(5*h + 15*m), 3, 0},
		{"unavailable gaps", []pt{{at(0), "230"}, {at(h), "unavailable"}, {at(2 * h), "230"},
			{at(3 * h), "0"}, {at(3*h + 10*m), "unknown"}, {at(4 * h), "0"}, {at(4*h + 20*m), "230"}},
			"нет 30 мин ×1", at(3 * h), at(4*h + 20*m), 1, 0},
		{"window start mid-outage", []pt{{since.Add(-2 * h), "0"}, {at(30 * m), "230"}},
			"нет 30 мин ×1", since, at(30 * m), 1, 0},
		{"dip off then low", []pt{{at(0), "230"}, {at(h), "0"}, {at(h + 20*m), "120"}, {at(h + 30*m), "230"}},
			"нет 20 мин ×1 · низкое 10 мин ×1", at(h), at(h + 20*m), 1, 1},
		{"long low", []pt{{at(0), "140"}}, "низкое 24 ч ×1", time.Time{}, time.Time{}, 0, 1},
		{"no data", []pt{{at(0), "unavailable"}}, "нет данных", time.Time{}, time.Time{}, 0, 0},
		{"empty", nil, "нет данных", time.Time{}, time.Time{}, 0, 0},
	}
	for _, c := range cases {
		var sts []hass.HistState
		for _, p := range c.sts {
			sts = append(sts, hass.HistState{Time: p.t, State: p.s})
		}
		st := outageStatsOf(sts, since, now)
		if got := outageText(st); got != c.text {
			t.Errorf("%s: text %q, want %q", c.name, got, c.text)
		}
		if st.Off.Count != c.offCount || st.Low.Count != c.lowCount {
			t.Errorf("%s: counts off=%d low=%d", c.name, st.Off.Count, st.Low.Count)
		}
		if !st.Off.LastStart.Equal(c.offStart) || !st.Off.LastEnd.Equal(c.offEnd) {
			t.Errorf("%s: off last %v..%v", c.name, st.Off.LastStart, st.Off.LastEnd)
		}
	}
}

func TestGridRowName(t *testing.T) {
	for _, c := range []struct {
		st   outageStats
		want string
	}{
		{outageStats{Known: true}, "L1"},
		{outageStats{Low: outageCat{Minutes: 10, Count: 2}}, "L1"},
		{outageStats{Off: outageCat{Minutes: 861, Count: 4}, Low: outageCat{Minutes: 1, Count: 3}}, "L1 · нет 14 ч 21 м ×4"},
		{outageStats{Off: outageCat{Minutes: 35, Count: 1}}, "L1 · нет 35 м ×1"},
		{outageStats{Off: outageCat{Minutes: 0.2, Count: 1}}, "L1 · нет <1 м ×1"},
	} {
		if got := gridRowName(1, c.st); got != c.want {
			t.Errorf("gridRowName(%+v) = %q, want %q", c.st, got, c.want)
		}
	}
}

func TestPatchGridRowNames(t *testing.T) {
	// дашборд после 0.6.56: отдельные строки отключений + статичные имена L1..L3
	src := `{"views":[{"cards":[{"type":"entities","title":"Сеть и фазы","entities":[
	  {"entity":"sensor.deye_sun_30k_grid_power"},
	  {"entity":"sensor.energy_schema_grid_l1","name":"L1"},
	  {"entity":"sensor.energy_schema_grid_l2","name":"L2"},
	  {"entity":"sensor.energy_schema_grid_l3","name":"L3"},
	  {"entity":"sensor.energy_schema_grid_outage_l1"},
	  {"entity":"sensor.energy_schema_grid_outage_l2"},
	  {"entity":"sensor.energy_schema_grid_outage_l3"},
	  {"entity":"sensor.deye_sun_30k_grid_frequency"}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	if changed, err := patchGridRowNames(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchGridRowNames(cfg); changed {
		t.Error("second run must be a no-op")
	}
	b, _ := json.Marshal(cfg)
	want := `{"entity":"sensor.energy_schema_grid_l1"},{"entity":"sensor.energy_schema_grid_l2"},` +
		`{"entity":"sensor.energy_schema_grid_l3"},{"entity":"sensor.deye_sun_30k_grid_frequency"}`
	if !strings.Contains(string(b), want) || strings.Contains(string(b), "grid_outage") {
		t.Errorf("rows not migrated: %s", b)
	}
}
