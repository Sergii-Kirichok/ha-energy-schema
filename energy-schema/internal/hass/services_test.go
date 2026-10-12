package hass

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// seenReq is one request captured by recServer.
type seenReq struct {
	Method, Path, Query, Auth, CT, Body string
}

// recServer records every request and answers via reply (status, body).
func recServer(t *testing.T, reply func(r *http.Request, body string) (int, string)) (*httptest.Server, func() []seenReq) {
	t.Helper()
	var mu sync.Mutex
	var seen []seenReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, seenReq{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b)})
		mu.Unlock()
		code, out := reply(r, string(b))
		w.WriteHeader(code)
		_, _ = w.Write([]byte(out))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seenReq {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenReq(nil), seen...)
	}
}

// jsonEq compares a JSON body against an expected value after normalising both.
func jsonEq(t *testing.T, body string, want any) {
	t.Helper()
	var got, exp any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	b, _ := json.Marshal(want)
	_ = json.Unmarshal(b, &exp)
	gb, _ := json.Marshal(got)
	eb, _ := json.Marshal(exp)
	if string(gb) != string(eb) {
		t.Errorf("body = %s, want %s", gb, eb)
	}
}

func TestSetState(t *testing.T) {
	srv, seen := recServer(t, func(*http.Request, string) (int, string) { return 201, `{"entity_id":"sensor.es_x"}` })
	c := NewClient(srv.URL+"/api", "TK")
	if err := c.SetState("sensor.es_x", "42", map[string]any{"unit_of_measurement": "W"}); err != nil {
		t.Fatal(err)
	}
	r := seen()[0]
	if r.Method != "POST" || r.Path != "/api/states/sensor.es_x" || r.Auth != "Bearer TK" || r.CT != "application/json" {
		t.Errorf("request = %+v", r)
	}
	jsonEq(t, r.Body, map[string]any{"state": "42", "attributes": map[string]any{"unit_of_measurement": "W"}})
}

func TestSetStateError(t *testing.T) {
	srv, _ := recServer(t, func(*http.Request, string) (int, string) { return 400, `{"message":"Invalid state"}` })
	err := NewClient(srv.URL+"/api", "TK").SetState("sensor.es_x", "", nil)
	if err == nil || !strings.Contains(err.Error(), "set state sensor.es_x") || !strings.Contains(err.Error(), "status=400") {
		t.Errorf("err = %v", err)
	}
}

// deviceReply answers /api/template with dev and the service with svcCode.
func deviceReply(dev string, svcCode int) func(*http.Request, string) (int, string) {
	return func(r *http.Request, _ string) (int, string) {
		if r.URL.Path == "/api/template" {
			return 200, dev
		}
		return svcCode, `[]`
	}
}

func TestWriteHoldingRegisters(t *testing.T) {
	srv, seen := recServer(t, deviceReply("dev1\n", 200))
	if err := NewClient(srv.URL+"/api", "TK").WriteHoldingRegisters("sensor.inv", 142, []int{1, 65535}); err != nil {
		t.Fatal(err)
	}
	reqs := seen()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if reqs[0].Path != "/api/template" || reqs[0].Auth != "Bearer TK" {
		t.Errorf("template req = %+v", reqs[0])
	}
	jsonEq(t, reqs[0].Body, map[string]string{"template": "{{ device_id('sensor.inv') }}"})
	w := reqs[1]
	if w.Method != "POST" || w.Path != "/api/services/solarman/write_multiple_holding_registers" || w.Auth != "Bearer TK" || w.CT != "application/json" {
		t.Errorf("write req = %+v", w)
	}
	jsonEq(t, w.Body, map[string]any{"device": "dev1", "register": 142, "values": []int{1, 65535}})
}

func TestWriteHoldingRegistersNoDevice(t *testing.T) {
	for _, dev := range []string{"None", "  "} {
		srv, seen := recServer(t, deviceReply(dev, 200))
		err := NewClient(srv.URL+"/api", "TK").WriteHoldingRegisters("sensor.inv", 142, []int{1})
		if err == nil || !strings.Contains(err.Error(), "device_id(sensor.inv)") {
			t.Errorf("dev %q: err = %v", dev, err)
		}
		if n := len(seen()); n != 1 {
			t.Errorf("dev %q: %d requests, service must not be called", dev, n)
		}
	}
}

func TestWriteHoldingRegistersServiceFails(t *testing.T) {
	srv, _ := recServer(t, deviceReply("dev1", 500))
	err := NewClient(srv.URL+"/api", "TK").WriteHoldingRegisters("sensor.inv", 142, []int{1})
	if err == nil || !strings.Contains(err.Error(), "solarman.write_multiple_holding_registers") {
		t.Errorf("err = %v", err)
	}
}
