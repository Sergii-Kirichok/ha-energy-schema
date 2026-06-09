package hass

import (
	"net/http"
	"net/http/httptest"
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
