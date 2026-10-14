package hass

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"energy-schema/internal/hass/hatest"
)

func TestCallService(t *testing.T) {
	srv, seen := recServer(t, func(*http.Request, string) (int, string) { return 200, `[]` })
	err := NewClient(srv.URL+"/api", "TK").CallService("input_number", "set_value", map[string]any{"entity_id": "input_number.x", "value": 12.5})
	if err != nil {
		t.Fatal(err)
	}
	r := seen()[0]
	if r.Method != "POST" || r.Path != "/api/services/input_number/set_value" || r.Auth != "Bearer TK" || r.CT != "application/json" {
		t.Errorf("request = %+v", r)
	}
	jsonEq(t, r.Body, map[string]any{"entity_id": "input_number.x", "value": 12.5})
}

func TestCallServiceError(t *testing.T) {
	srv, _ := recServer(t, func(*http.Request, string) (int, string) { return 500, "boom" })
	err := NewClient(srv.URL+"/api", "TK").CallService("light", "turn_on", nil)
	if err == nil || !strings.Contains(err.Error(), "light.turn_on") || !strings.Contains(err.Error(), "status=500") {
		t.Errorf("err = %v", err)
	}
}

func TestHasEntity(t *testing.T) {
	srv, seen := recServer(t, func(r *http.Request, _ string) (int, string) {
		switch r.URL.Path {
		case "/api/states/sensor.yes":
			return 200, `{}`
		case "/api/states/sensor.no":
			return 404, `{"message":"Entity not found."}`
		}
		return 500, ""
	})
	c := NewClient(srv.URL+"/api", "TK")
	if ok, err := c.HasEntity("sensor.yes"); !ok || err != nil {
		t.Errorf("yes: %v %v", ok, err)
	}
	if ok, err := c.HasEntity("sensor.no"); ok || err != nil {
		t.Errorf("no: %v %v", ok, err)
	}
	if ok, err := c.HasEntity("sensor.err"); ok || err == nil || !strings.Contains(err.Error(), "status=500") {
		t.Errorf("err: %v %v", ok, err)
	}
	if r := seen()[0]; r.Method != "GET" || r.Auth != "Bearer TK" {
		t.Errorf("request = %+v", r)
	}
}

// haServer serves GET /api/states/<id> with stateCode and proxies the
// websocket to the hatest fake; reqs receives every WS request.
func haServer(t *testing.T, stateCode int, handle hatest.Handler) *httptest.Server {
	ws := hatest.NewServer(t, handle)
	target, _ := url.Parse(ws.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/websocket" {
			proxy.ServeHTTP(w, r)
			return
		}
		w.WriteHeader(stateCode)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsRec records WS requests; fail maps request type → error message.
func wsRec(reqs chan map[string]any, fail map[string]string) hatest.Handler {
	return func(req map[string]any) (any, bool, string) {
		reqs <- req
		if msg, bad := fail[req["type"].(string)]; bad {
			return nil, false, msg
		}
		return map[string]any{}, true, ""
	}
}

func TestEnsureHelperCreatesNumber(t *testing.T) {
	reqs := make(chan map[string]any, 4)
	srv := haServer(t, 404, wsRec(reqs, nil))
	h := Helper{Domain: "input_number", ID: "es_soc_min", Name: " Мин SOC ", Min: 10, Max: 90, Step: 1, Initial: 20, Unit: "%", Icon: "mdi:battery"}
	created, err := NewClient(srv.URL+"/api", hatest.Token).EnsureHelper(h)
	if !created || err != nil {
		t.Fatalf("created=%v err=%v", created, err)
	}
	create, rename := <-reqs, <-reqs
	want := map[string]any{"type": "input_number/create", "name": "es_soc_min", "icon": "mdi:battery", "min": 10.0,
		"max": 90.0, "step": 1.0, "initial": 20.0, "mode": "box", "unit_of_measurement": "%"}
	for k, v := range want {
		if create[k] != v {
			t.Errorf("create[%s] = %v, want %v", k, create[k], v)
		}
	}
	if rename["type"] != "config/entity_registry/update" || rename["entity_id"] != "input_number.es_soc_min" || rename["name"] != "Мин SOC" {
		t.Errorf("rename = %v", rename)
	}
	if create["id"] == rename["id"] {
		t.Error("WS ids must differ")
	}
}

func TestEnsureHelperCreatesBoolean(t *testing.T) {
	reqs := make(chan map[string]any, 4)
	srv := haServer(t, 404, wsRec(reqs, nil))
	h := Helper{Domain: "input_boolean", ID: "es_auto", Name: "Авто", Initial: 1, Max: 5}
	if created, err := NewClient(srv.URL+"/api", hatest.Token).EnsureHelper(h); !created || err != nil {
		t.Fatalf("created=%v err=%v", created, err)
	}
	create := <-reqs
	if create["type"] != "input_boolean/create" || create["initial"] != true {
		t.Errorf("create = %v", create)
	}
	for _, k := range []string{"min", "max", "step", "mode", "unit_of_measurement"} {
		if _, has := create[k]; has {
			t.Errorf("input_boolean create must not carry %q", k)
		}
	}
}

func TestEnsureHelperExistingOrHTTPError(t *testing.T) {
	for code, wantErr := range map[int]bool{200: false, 500: true} {
		reqs := make(chan map[string]any, 4)
		srv := haServer(t, code, wsRec(reqs, nil))
		created, err := NewClient(srv.URL+"/api", hatest.Token).EnsureHelper(Helper{Domain: "input_number", ID: "x"})
		if created || (err != nil) != wantErr {
			t.Errorf("status %d: created=%v err=%v", code, created, err)
		}
		if len(reqs) != 0 {
			t.Errorf("status %d: WS must not be touched", code)
		}
	}
}

func TestEnsureHelperWSFailures(t *testing.T) {
	cases := []struct {
		fail        map[string]string
		token       string
		wantCreated bool
		wantErr     string
	}{
		{map[string]string{"input_number/create": "boom"}, hatest.Token, false, "create input_number.x"},
		{map[string]string{"config/entity_registry/update": "nope"}, hatest.Token, true, "rename input_number.x"},
		{nil, "bad", false, "auth"},
	}
	for _, tc := range cases {
		srv := haServer(t, 404, wsRec(make(chan map[string]any, 4), tc.fail))
		created, err := NewClient(srv.URL+"/api", tc.token).EnsureHelper(Helper{Domain: "input_number", ID: "x"})
		if created != tc.wantCreated || err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: created=%v err=%v", tc.wantErr, created, err)
		}
	}
}

func TestEnsureNumberMode(t *testing.T) {
	// already in the target mode → no network at all (unroutable base would fail otherwise)
	if ch, err := NewClient("http://127.0.0.1:1/api", "T").EnsureNumberMode(Helper{ID: "x"}, inputNumberMode); ch || err != nil {
		t.Errorf("slider: changed=%v err=%v", ch, err)
	}
	reqs := make(chan map[string]any, 4)
	srv := hatest.NewServer(t, wsRec(reqs, nil))
	h := Helper{ID: "es_cap", Min: 0, Max: 100, Step: 5, Unit: "A", Icon: "mdi:flash"}
	if ch, err := NewClient(srv.URL+"/api", hatest.Token).EnsureNumberMode(h, "slider"); !ch || err != nil {
		t.Fatalf("slider→box: changed=%v err=%v", ch, err)
	}
	got := <-reqs
	want := map[string]any{"type": "input_number/update", "input_number_id": "es_cap", "name": "es_cap", "min": 0.0,
		"max": 100.0, "step": 5.0, "mode": "box", "icon": "mdi:flash", "unit_of_measurement": "A"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("update[%s] = %v, want %v", k, got[k], v)
		}
	}

	bad := hatest.NewServer(t, wsRec(make(chan map[string]any, 4), map[string]string{"input_number/update": "invalid"}))
	if ch, err := NewClient(bad.URL+"/api", hatest.Token).EnsureNumberMode(h, ""); ch || err == nil || !strings.Contains(err.Error(), "mode es_cap") {
		t.Errorf("fail: changed=%v err=%v", ch, err)
	}
}
