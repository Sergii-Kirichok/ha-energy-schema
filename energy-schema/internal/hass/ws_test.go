package hass

import (
	"strings"
	"testing"

	"energy-schema/internal/hass/hatest"
)

const wsEntity = "sensor.deye_sun_30k_daily_production"

// statsHandler serves a 3-row statistics_during_period result and forwards
// every request to reqs.
func statsHandler(reqs chan map[string]any) hatest.Handler {
	return func(req map[string]any) (any, bool, string) {
		reqs <- req
		rows := []map[string]any{
			{"start": 1.0, "change": 100.0}, // baseline row, dropped
			{"start": 2.0, "change": 12.5},
			{"start": 3.0, "change": 30.0},
		}
		return map[string]any{wsEntity: rows}, true, ""
	}
}

func TestDailyProduction(t *testing.T) {
	reqs := make(chan map[string]any, 4)
	srv := hatest.NewServer(t, statsHandler(reqs))
	got, err := NewClient(srv.URL+"/api", hatest.Token).DailyProduction(wsEntity, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 12.5 || got[1] != 30 {
		t.Errorf("got %v, want [12.5 30]", got)
	}
	req := <-reqs
	ids, _ := req["statistic_ids"].([]any)
	if req["type"] != "recorder/statistics_during_period" || req["period"] != "day" || len(ids) != 1 || ids[0] != wsEntity {
		t.Errorf("request = %v", req)
	}
}

func TestDailyProductionAuthFail(t *testing.T) {
	srv := hatest.NewServer(t, statsHandler(make(chan map[string]any, 4)))
	_, err := NewClient(srv.URL+"/api", "X").DailyProduction(wsEntity, 7)
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Errorf("err = %v, want auth failure", err)
	}
}

func TestDailyProductionResultError(t *testing.T) {
	srv := hatest.NewServer(t, func(map[string]any) (any, bool, string) {
		return nil, false, "statistic not found"
	})
	_, err := NewClient(srv.URL+"/api", hatest.Token).DailyProduction(wsEntity, 7)
	if err == nil || !strings.Contains(err.Error(), "statistic not found") {
		t.Errorf("err = %v", err)
	}
}

// Ping + fragmented reply + payloads crossing the 126 and 65535 length thresholds.
func TestReadMessageFramesAndLengths(t *testing.T) {
	echo := func(req map[string]any) (any, bool, string) {
		n, _ := req["n"].(float64)
		return strings.Repeat("x", int(n)), true, ""
	}
	srv := hatest.NewServerOpts(t, echo, hatest.Opts{Ping: true, Fragment: true})
	w, err := NewClient(srv.URL+"/api", hatest.Token).wsAuth()
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	for i, n := range []int{10, 200, 70000} {
		var resp struct {
			Result string `json:"result"`
		}
		if err := w.call(i+1, map[string]any{"type": "echo", "n": n}, &resp); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if len(resp.Result) != n || strings.Trim(resp.Result, "x") != "" {
			t.Errorf("n=%d: got %d bytes", n, len(resp.Result))
		}
	}
}

func TestCallSkipsOtherIDs(t *testing.T) {
	reqs := make(chan map[string]any, 4)
	srv := hatest.NewServerOpts(t, statsHandler(reqs), hatest.Opts{
		Before: map[string]any{"id": 99, "type": "result", "success": false, "error": map[string]any{"message": "wrong id"}},
	})
	got, err := NewClient(srv.URL+"/api", hatest.Token).DailyProduction(wsEntity, 7)
	if err != nil || len(got) != 2 {
		t.Errorf("got %v err %v", got, err)
	}
}
