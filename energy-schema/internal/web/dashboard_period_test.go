package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// Блок выбора периода: «Ячейки» встают в каждый вариант со своим периодом и
// сглаживанием, отдельный суточный график уходит, оси выравниваются; повтор — no-op.
func TestPatchPeriodCharts(t *testing.T) {
	variant := func(span, cond, sm string, grouped bool) string {
		g := ""
		if grouped {
			g = `,"group_by":{"duration":"5m","func":"avg"}`
		}
		return `{"type":"conditional","conditions":[{"entity":"input_select.diapazon_grafika","state":"` + cond + `"},{"entity":"input_boolean.sglazhivanie_grafikov","state":"` + sm + `"}],
		  "card":{"type":"vertical-stack","cards":[
		   {"type":"custom:apexcharts-card","graph_span":"` + span + `","series":[{"entity":"sensor.deye_sun_30k_grid_power"` + g + `}]},
		   {"type":"custom:apexcharts-card","graph_span":"` + span + `","yaxis":[{"id":"power"},{"id":"soc","opposite":true}],"series":[{"entity":"sensor.deye_sun_30k_battery_power","yaxis_id":"power"}]}]}}`
	}
	src := `{"views":[{"sections":[{"cards":[{"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"}]},
	  {"type":"entities","entities":[{"entity":"sensor.energy_schema_bms_weak_cell"}]}]},
	  {"cards":[{"type":"vertical-stack","cards":[` + variant("12h", "12", "on", true) + `,` + variant("1h", "1", "off", false) + `]},
	  {"type":"custom:apexcharts-card","graph_span":"24h","series":[{"entity":"sensor.energy_schema_bms_cell_delta"}]}]}]}]}`
	var cfg any
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if changed, err := patchBMSCards(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	stacks := periodStacks(cfg)
	for i, want := range []struct{ span, linesGroup string }{{"12h", "5m"}, {"1h", ""}} {
		cards := stacks[i]["cards"].([]any)
		cells := cards[len(cards)-1].(map[string]any)
		if len(cards) != 3 || cells["graph_span"] != want.span {
			t.Fatalf("stack %d: %d cards, span %v", i, len(cards), cells["graph_span"])
		}
		g, _ := cells["series"].([]any)[0].(map[string]any)["group_by"].(map[string]any)
		if (want.linesGroup == "") != (g == nil) || g != nil && g["duration"] != want.linesGroup {
			t.Errorf("stack %d: smoothing mismatch %v", i, g)
		}
	}
	b, _ := json.Marshal(cfg)
	if strings.Count(string(b), "energy_schema_bms_cell_delta") != 2 {
		t.Errorf("standalone 24h chart must be gone, 1 per stack expected")
	}
	if strings.Contains(string(b), "kw_r") || strings.Contains(string(b), "maxWidth") || !strings.Contains(string(b), `"id":"kw"`) || strings.Count(string(b), `"minWidth":44`) < 10 {
		t.Errorf("axes not aligned: %s", b)
	}
	if changed, _ := patchBMSCards(cfg); changed {
		t.Error("second run must be a no-op")
	}
}

// Старый вид: призрачная правая ось kw_r с невидимой серией и maxWidth в
// подписях — убираются; повтор — no-op.
func TestAlignAxesMigratesGhostAxis(t *testing.T) {
	src := `{"yaxis":[{"id":"kw","apex_config":{"labels":{"minWidth":44,"maxWidth":44}}},{"id":"kw_r","opposite":true}],
	  "series":[{"entity":"a","yaxis_id":"kw"},{"entity":"a","yaxis_id":"kw_r","name":" ","opacity":0}]}`
	var chart map[string]any
	if err := json.Unmarshal([]byte(src), &chart); err != nil {
		t.Fatal(err)
	}
	if !alignAxes(chart) {
		t.Fatal("must change")
	}
	b, _ := json.Marshal(chart)
	if strings.Contains(string(b), "kw_r") || strings.Contains(string(b), "maxWidth") || len(chart["series"].([]any)) != 1 || !strings.Contains(string(b), `"minWidth":44`) {
		t.Errorf("not migrated: %s", b)
	}
	if alignAxes(chart) {
		t.Error("second run must be a no-op")
	}
}
