package hass

import (
	"encoding/json"
	"strings"
	"testing"

	"energy-schema/internal/hass/hatest"
)

func TestLovelaceConfig(t *testing.T) {
	const want = `{"views":[{"title":"Energy","cards":[]}]}`
	reqs := make(chan map[string]any, 4)
	srv := hatest.NewServer(t, func(req map[string]any) (any, bool, string) {
		reqs <- req
		return json.RawMessage(want), true, ""
	})
	got, err := NewClient(srv.URL+"/api", hatest.Token).LovelaceConfig("home-energy")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got %s", got)
	}
	req := <-reqs
	if req["type"] != "lovelace/config" || req["url_path"] != "home-energy" {
		t.Errorf("request = %v", req)
	}
}

// Sizes cross the 126 / 65535 client-side frame length thresholds (writeText).
func TestSaveLovelaceConfig(t *testing.T) {
	reqs := make(chan map[string]any, 4)
	srv := hatest.NewServer(t, func(req map[string]any) (any, bool, string) {
		reqs <- req
		return nil, true, ""
	})
	c := NewClient(srv.URL+"/api", hatest.Token)
	for _, n := range []int{10, 300, 70000} {
		pad := strings.Repeat("a", n)
		cfg, _ := json.Marshal(map[string]any{"views": []any{}, "pad": pad})
		if err := c.SaveLovelaceConfig("home-energy", cfg); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		req := <-reqs
		got, _ := req["config"].(map[string]any)
		if req["type"] != "lovelace/config/save" || req["url_path"] != "home-energy" || got["pad"] != pad {
			t.Errorf("n=%d: request type=%v url_path=%v config=%v", n, req["type"], req["url_path"], got != nil)
		}
	}
}
