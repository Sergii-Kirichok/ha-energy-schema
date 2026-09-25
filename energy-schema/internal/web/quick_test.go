package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"energy-schema/internal/hass"
)

func TestQuickParam(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.URL.Path+" "+string(b))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	s := &Server{client: hass.NewClient(srv.URL+"/api", "T"), store: hass.NewStore()}
	s.store.ReplaceStates(map[string]string{
		"input_number.energy_schema_charge_max_a":      "28",
		"input_number.energy_schema_charge_grid_a":     "1",
		"input_boolean.energy_schema_charge_auto":      "on",
		"input_number.energy_schema_charge_target_soc": "90",
	})
	cases := []struct {
		val, wantCall string
		wantErr       bool
	}{
		{"max_a:+5", `set_value {"entity_id":"input_number.energy_schema_charge_max_a","value":30}`, false},  // clamp to max
		{"grid_a:-1", `set_value {"entity_id":"input_number.energy_schema_charge_grid_a","value":1}`, false}, // clamp to min
		{"target_soc:-5", `"value":85}`, false},
		{"auto:toggle", `/api/services/input_boolean/toggle {"entity_id":"input_boolean.energy_schema_charge_auto"}`, false},
		{"auto:+1", "", true},          // bool needs toggle
		{"max_a:+50", "", true},        // step too big
		{"max_a:abc", "", true},        // not a number
		{"nope:+1", "", true},          // unknown key
		{"full_days:+1", "", true},     // entity missing in store
		{"taper_soc:toggle", "", true}, // number with toggle
	}
	for _, c := range cases {
		calls = nil
		_, err := s.quickParam(c.val)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.val, err, c.wantErr)
			continue
		}
		if c.wantErr {
			if len(calls) != 0 {
				t.Errorf("%s: must not call HA on error, got %v", c.val, calls)
			}
			continue
		}
		if len(calls) != 1 || !strings.Contains(calls[0], c.wantCall) {
			t.Errorf("%s: calls=%v want contains %q", c.val, calls, c.wantCall)
		}
	}
}
