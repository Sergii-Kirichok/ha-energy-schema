package hass

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchStates(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"entity_id":"sensor.a","state":"1.5","last_changed":"2026-06-09T17:42:00+00:00","attributes":{"unit":"kW"}},
			{"entity_id":"switch.b","state":"on"}
		]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/api", "TOK42")
	m, err := c.FetchStates()
	if err != nil {
		t.Fatalf("FetchStates: %v", err)
	}
	if gotPath != "/api/states" {
		t.Errorf("path = %q, want /api/states", gotPath)
	}
	if gotAuth != "Bearer TOK42" {
		t.Errorf("auth = %q, want Bearer TOK42", gotAuth)
	}
	if m["sensor.a"].State != "1.5" || m["switch.b"].State != "on" {
		t.Errorf("parsed map = %v", m)
	}
	if m["sensor.a"].LastChanged.IsZero() {
		t.Error("last_changed not parsed for sensor.a")
	}
	if len(m) != 2 {
		t.Errorf("len = %d, want 2", len(m))
	}
}

func TestFetchStatesBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`401: Unauthorized`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/api", "")
	if _, err := c.FetchStates(); err == nil {
		t.Error("expected error on non-JSON body, got nil")
	}
}

// HA answers 401/500 with a JSON object; before doJSON that decoded "fine"
// into zero values (tz="" / lat=lon=0) with err == nil.
func TestTimeZoneNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/api", "")
	if tz, err := c.TimeZone(); err == nil {
		t.Errorf("expected error on 401, got tz=%q err=nil", tz)
	}
	if _, _, _, err := c.Location(); err == nil {
		t.Error("expected error on 401 from Location, got nil")
	}
}

func TestReadHoldingRegisters(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/template":
			_, _ = w.Write([]byte("abc123"))
		case "/api/services/solarman/read_holding_registers":
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"changed_states":[],"service_response":{"10006":97}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/api", "T")
	regs, err := c.ReadHoldingRegisters("sensor.x", 10006, 1)
	if err != nil {
		t.Fatalf("ReadHoldingRegisters: %v", err)
	}
	if regs[10006] != 97 {
		t.Errorf("regs = %v, want 10006=97", regs)
	}
	if !strings.Contains(gotBody, `"device":"abc123"`) || !strings.Contains(gotBody, `"address":10006`) {
		t.Errorf("service body = %s", gotBody)
	}
}

func TestStoreVirtualSurvivesReplace(t *testing.T) {
	s := NewStore()
	s.SetVirtual("sensor.v", "97")
	s.ReplaceStates(map[string]string{"sensor.a": "1"})
	if s.State("sensor.v") != "97" || !s.Available("sensor.v") {
		t.Errorf("virtual lost after Replace: %q", s.State("sensor.v"))
	}
	s.ReplaceStates(map[string]string{"sensor.v": "5"})
	if s.Num("sensor.v") != 5 {
		t.Error("real entity must override virtual")
	}
}
