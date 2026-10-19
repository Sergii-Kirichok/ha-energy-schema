package hass

import (
	"encoding/json"
	"testing"

	"energy-schema/internal/hass/hatest"
)

// Снимаем initial только у своих input_boolean и только если он задан;
// имя и иконка уходят как были (update заменяет запись целиком).
func TestDropBoolInitial(t *testing.T) {
	updates := make(chan map[string]any, 8)
	srv := hatest.NewServer(t, func(req map[string]any) (any, bool, string) {
		switch req["type"] {
		case "input_boolean/list":
			return json.RawMessage(`[{"id":"ours_a","name":"ours_a","icon":"mdi:x","initial":false},
			  {"id":"ours_b","name":"ours_b"},
			  {"id":"foreign","name":"foreign","initial":true}]`), true, ""
		case "input_boolean/update":
			updates <- req
			return nil, true, ""
		}
		return nil, false, "unknown command"
	})
	n, err := NewClient(srv.URL+"/api", hatest.Token).DropBoolInitial([]string{"ours_a", "ours_b"})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	u := <-updates
	if _, has := u["initial"]; has || u["input_boolean_id"] != "ours_a" || u["name"] != "ours_a" || u["icon"] != "mdi:x" {
		t.Errorf("update = %v", u)
	}
	if len(updates) != 0 {
		t.Error("only ours_a must be updated")
	}
}
