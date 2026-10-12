package hass

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const fcReply = `{"changed_states":[],"service_response":{"weather.home":{"forecast":[
	{"datetime":"2026-10-12T09:00:00+00:00","condition":"sunny","cloud_coverage":12.5},
	{"datetime":"2026-10-13T09:00:00+00:00","condition":"rainy"},
	{"datetime":"garbage","condition":"fog","cloud_coverage":100}]}}}`

func TestDailyAndHourlyForecast(t *testing.T) {
	srv, seen := recServer(t, func(*http.Request, string) (int, string) { return 200, fcReply })
	c := NewClient(srv.URL+"/api", "TK")
	days, err := c.DailyForecast("weather.home")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 {
		t.Fatalf("days = %v", days)
	}
	want0 := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	if !days[0].Time.Equal(want0) || days[0].Condition != "sunny" || days[0].Cloud != 12.5 {
		t.Errorf("day0 = %+v", days[0])
	}
	if days[1].Cloud != 0 || days[1].Condition != "rainy" { // daily often omits cloud_coverage
		t.Errorf("day1 = %+v", days[1])
	}
	if !days[2].Time.IsZero() || days[2].Condition != "fog" { // unparsable datetime → zero time, kept
		t.Errorf("day2 = %+v", days[2])
	}
	if _, err := c.HourlyForecast("weather.home"); err != nil {
		t.Fatal(err)
	}
	reqs := seen()
	for i, ftype := range []string{"daily", "hourly"} {
		r := reqs[i]
		if r.Method != "POST" || r.Path != "/api/services/weather/get_forecasts" || r.Query != "return_response" || r.Auth != "Bearer TK" {
			t.Errorf("%s req = %+v", ftype, r)
		}
		jsonEq(t, r.Body, map[string]string{"entity_id": "weather.home", "type": ftype})
	}
}

func TestForecastErrors(t *testing.T) {
	srv, _ := recServer(t, func(*http.Request, string) (int, string) { return 200, fcReply })
	if _, err := NewClient(srv.URL+"/api", "TK").DailyForecast("weather.other"); err == nil || !strings.Contains(err.Error(), "no forecast for weather.other") {
		t.Errorf("missing entity err = %v", err)
	}
	bad, _ := recServer(t, func(*http.Request, string) (int, string) { return 500, `{"message":"err"}` })
	if _, err := NewClient(bad.URL+"/api", "TK").HourlyForecast("weather.home"); err == nil {
		t.Error("500 must error")
	}
}

func TestHistory(t *testing.T) {
	srv, seen := recServer(t, func(*http.Request, string) (int, string) {
		return 200, `[[
			{"state":"10.5","last_changed":"2026-10-12T08:00:00+00:00"},
			{"state":"unavailable","last_changed":"2026-10-12T08:01:00+00:00"},
			{"state":" 7 ","last_updated":"2026-10-12T08:02:00.123+00:00"},
			{"state":"3","last_changed":"bad time"}
		]]`
	})
	since := time.Date(2026, 10, 12, 11, 0, 0, 0, time.FixedZone("EEST", 3*3600))
	pts, err := NewClient(srv.URL+"/api", "TK").History("sensor.p", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 || pts[0].Value != 10.5 || pts[1].Value != 7 {
		t.Fatalf("points = %+v", pts)
	}
	if !pts[1].Time.Equal(time.Date(2026, 10, 12, 8, 2, 0, 123e6, time.UTC)) {
		t.Errorf("last_updated fallback time = %v", pts[1].Time)
	}
	r := seen()[0]
	if r.Method != "GET" || r.Path != "/api/history/period/2026-10-12T08:00:00" || r.Auth != "Bearer TK" {
		t.Errorf("request = %+v (since must be sent in UTC)", r)
	}
	q, _ := url.ParseQuery(r.Query)
	if q.Get("filter_entity_id") != "sensor.p" || !q.Has("minimal_response") {
		t.Errorf("query = %q", r.Query)
	}
}

func TestHistoryEmptyAndError(t *testing.T) {
	srv, _ := recServer(t, func(*http.Request, string) (int, string) { return 200, `[]` })
	if pts, err := NewClient(srv.URL+"/api", "TK").History("sensor.p", time.Now()); pts != nil || err != nil {
		t.Errorf("empty = %v %v", pts, err)
	}
	bad, _ := recServer(t, func(*http.Request, string) (int, string) { return 401, `{"message":"Unauthorized"}` })
	if _, err := NewClient(bad.URL+"/api", "TK").History("sensor.p", time.Now()); err == nil || !strings.Contains(err.Error(), "history") {
		t.Errorf("401 err = %v", err)
	}
}
