package web

import (
	"encoding/json"
	"fmt"
	"testing"
)

const visDash = `{"views":[{"type":"sections","sections":[{"type":"grid","cards":[
  {"type":"entities","title":"Заряд АКБ","entities":[{"entity":"input_boolean.energy_schema_charge_auto"},"sensor.x"]},
  {"type":"entities","title":"Сеть","entities":["sensor.grid"]},
  {"type":"vertical-stack","cards":[{"type":"tile","entity":"input_select.diapazon_grafika",
    "tap_action":{"action":"perform-action","perform_action":"input_select.select_option","target":{"entity_id":"input_select.diapazon_grafika"}}},
    {"type":"tile","entity":"input_boolean.sglazhivanie_grafikov"}]},
  {"type":"conditional","conditions":[{"entity":"input_boolean.energy_schema_charge_balance","state":"on"}],"card":{"type":"markdown"}}]}],
  "cards":[{"type":"tile","entity":"switch.pump","visibility":[{"condition":"screen","media_query":"(min-width: 0px)"}]}]}]}`

func visOf(card any) string {
	b, _ := json.Marshal(card.(map[string]any)["visibility"])
	return string(b)
}

func TestPatchControlVisibility(t *testing.T) {
	var cfg any
	if err := json.Unmarshal([]byte(visDash), &cfg); err != nil {
		t.Fatal(err)
	}
	view := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)
	sec := view["sections"].([]any)[0].(map[string]any)["cards"].([]any)
	pump := view["cards"].([]any)[0]
	user := `{"condition":"user","users":["own1"]}`
	for run, want := range []bool{true, false} {
		if got := patchControlVisibility(cfg, []string{"own1"}); got != want {
			t.Fatalf("run %d: changed=%v", run, got)
		}
	}
	if visOf(sec[0]) != "["+user+"]" {
		t.Errorf("charge card vis = %s", visOf(sec[0]))
	}
	for i := 1; i < 4; i++ {
		if visOf(sec[i]) != "null" {
			t.Errorf("card %d must stay visible: %s", i, visOf(sec[i]))
		}
	}
	if visOf(pump) != `[{"condition":"screen","media_query":"(min-width: 0px)"},`+user+`]` {
		t.Errorf("pump vis = %s", visOf(pump))
	}
	// пустой control_users — условие снимается, чужие условия остаются
	for run, want := range []bool{true, false} {
		if got := patchControlVisibility(cfg, nil); got != want {
			t.Fatalf("clear run %d: changed=%v", run, got)
		}
	}
	if visOf(sec[0]) != "null" || visOf(pump) != `[{"condition":"screen","media_query":"(min-width: 0px)"}]` {
		t.Errorf("after clear: %s / %s", visOf(sec[0]), visOf(pump))
	}
}

// Карточка «Заряд АКБ», созданная аддоном, сразу получает условие с id владельца
// (имена из control_users → id через config/auth/list); повтор — без записи.
func TestEnsureDashboardControlVisibility(t *testing.T) {
	s, d := newFakeDash(t, `{"views":[{"sections":[{"cards":[
	  {"type":"entities","entities":["sensor.deye_sun_30k_battery_soh"]}]}]}]}`)
	s.cfg.ControlUsers = []string{"stari40k"}
	s.ensureDashboard("home-energy")
	s.ensureDashboard("home-energy")
	saves, cfg := d.snapshot()
	if saves != 1 {
		t.Fatalf("saves = %d, want 1", saves)
	}
	if got := visOf(findCardWith(cfg, dashChargeMarker)); got != `[{"condition":"user","users":["own1"]}]` {
		t.Errorf("charge card vis = %s", got)
	}
	if _, ok := dashChargeCard["visibility"]; ok {
		t.Error("template card must not be mutated")
	}
}

func TestControlUserIDs(t *testing.T) {
	s, _ := newFakeDash(t, `{}`)
	for _, c := range []struct {
		users []string
		want  string
	}{{nil, "[] <nil>"}, {[]string{" Светуля ", "own1"}, "[v1 own1] <nil>"}, {[]string{"archy", "Supervisor"}, "[] control_users"}} {
		s.cfg.ControlUsers = c.users
		ids, err := s.controlUserIDs()
		got := fmt.Sprint(ids, " ", err)
		if len(got) < len(c.want) || got[:len(c.want)] != c.want {
			t.Errorf("%v: got %q, want prefix %q", c.users, got, c.want)
		}
	}
}
