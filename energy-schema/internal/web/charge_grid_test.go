package web

import "testing"

func TestSyncGridCharge(t *testing.T) {
	cases := []struct {
		name        string
		sw, block   string
		night       bool
		soc         float64
		wantService string // "" — вызова быть не должно
	}{
		{"day, block on → off", "on", "on", false, 50, "turn_off"},
		{"night → on", "off", "on", true, 50, "turn_on"},
		{"day, soc below shutdown+5 → on", "off", "on", false, 24, "turn_on"},
		{"day, block off → on", "off", "off", false, 50, "turn_on"},
		{"already off by day", "off", "on", false, 50, ""},
		{"already on at night", "on", "on", true, 50, ""},
		{"switch unavailable", "unavailable", "on", true, 50, ""},
		{"switch missing", "", "on", true, 50, ""},
	}
	for _, c := range cases {
		f := &haFake{}
		s := newHAFakeServer(t, f)
		st := map[string]string{noGridDayHelper: c.block, shutdownSOCNum: "20"}
		if c.sw != "" {
			st[gridChargeSwitch] = c.sw
		}
		s.store.ReplaceStates(st)
		s.syncGridCharge(c.night, c.soc)
		var got []string
		for _, svc := range []string{"turn_on", "turn_off"} {
			for _, call := range f.find("/services/switch/" + svc) {
				if call.body["entity_id"] != gridChargeSwitch {
					t.Errorf("%s: entity %v", c.name, call.body["entity_id"])
				}
				got = append(got, svc)
			}
		}
		switch {
		case c.wantService == "" && len(got) != 0:
			t.Errorf("%s: unexpected calls %v", c.name, got)
		case c.wantService != "" && (len(got) != 1 || got[0] != c.wantService):
			t.Errorf("%s: calls %v, want %s", c.name, got, c.wantService)
		}
	}
}

// Ошибка сервиса не роняет регулятор (только лог).
func TestSyncGridChargeServiceError(t *testing.T) {
	f := &haFake{fail: "/services/switch/"}
	s := newHAFakeServer(t, f)
	s.store.ReplaceStates(map[string]string{gridChargeSwitch: "on", noGridDayHelper: "on", shutdownSOCNum: "20"})
	s.syncGridCharge(false, 50)
	if len(f.calls) != 0 {
		t.Errorf("failed call must not be recorded: %v", f.calls)
	}
}

// Через chargeTick: авто-регулятор днём выключает заряд от сети.
func TestChargeTickSyncsGridCharge(t *testing.T) {
	s, f := tickSetup(t, map[string]string{gridChargeSwitch: "on", noGridDayHelper: "on", shutdownSOCNum: "20"}, nil)
	st := recentState()
	runTick(s, &st)
	if len(f.find("/services/switch/turn_off")) != 1 {
		t.Errorf("auto on, day: want one turn_off, calls %v", f.calls)
	}
}
