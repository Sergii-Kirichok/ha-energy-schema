package solar

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// redirectRT rewrites every request onto the test server so FetchOpenMeteo's
// hard-coded api.open-meteo.com URL needs no production hook.
type redirectRT struct{ base *url.URL }

func (r redirectRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme, req.URL.Host = r.base.Scheme, r.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

// omServer serves a 3-hour Open-Meteo fixture whose GTI is offset per tilt so
// each array's response is distinguishable; it records every query string.
func omServer(t *testing.T, status int) (*http.Client, *[]url.Values) {
	t.Helper()
	var mu sync.Mutex
	var seen []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		mu.Lock()
		seen = append(seen, q)
		mu.Unlock()
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		tilt := 0.0
		fmt.Sscanf(q.Get("tilt"), "%f", &tilt)
		fmt.Fprintf(w, `{"hourly":{
			"time":["2026-06-10T06:00","2026-06-10T07:00","2026-06-10T08:00"],
			"temperature_2m":[14.1,15.3,16.8],
			"cloud_cover":[20,45,null],
			"global_tilted_irradiance":[null,%g,%g]}}`, 100+tilt, 250+tilt)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return &http.Client{Transport: redirectRT{u}}, &seen
}

func TestFetchOpenMeteoPerArrayRequestsAndMerge(t *testing.T) {
	httpc, seen := omServer(t, 200)
	arrays := []Array{
		{Name: "south", TiltDeg: 30, AzDeg: 180},
		{Name: "east", TiltDeg: 60, AzDeg: 90},
	}
	hours, err := FetchOpenMeteo(Location{Lat: 50.45, Lon: 30.52}, arrays, "Europe/Kyiv", httpc)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 {
		t.Fatalf("requests = %d, want one per array", len(*seen))
	}
	// azimuth is converted from compass to Open-Meteo's 0=S convention
	want := []struct{ tilt, az string }{{"30", "0"}, {"60", "-90"}}
	for i, q := range *seen {
		if q.Get("tilt") != want[i].tilt || q.Get("azimuth") != want[i].az {
			t.Errorf("req %d tilt=%q azimuth=%q, want %q/%q", i, q.Get("tilt"), q.Get("azimuth"), want[i].tilt, want[i].az)
		}
		if q.Get("latitude") != "50.45000" || q.Get("longitude") != "30.52000" || q.Get("timezone") != "Europe/Kyiv" {
			t.Errorf("req %d lat/lon/tz = %q/%q/%q", i, q.Get("latitude"), q.Get("longitude"), q.Get("timezone"))
		}
		if !strings.Contains(q.Get("hourly"), "global_tilted_irradiance") {
			t.Errorf("req %d hourly=%q lacks GTI", i, q.Get("hourly"))
		}
	}
	if len(hours) != 3 {
		t.Fatalf("hours = %d, want 3", len(hours))
	}
	// label 07:00 describes the preceding hour → HourStart 06:00 local
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	if want := time.Date(2026, 6, 10, 6, 0, 0, 0, kyiv); !hours[1].HourStart.Equal(want) {
		t.Errorf("HourStart[1] = %v, want %v", hours[1].HourStart, want)
	}
	// GTI per array in its own slot (fixture = base + tilt); null → 0
	if g := hours[0].GTI; g[0] != 0 || g[1] != 0 {
		t.Errorf("null GTI hour = %v, want zeros", g)
	}
	if g := hours[1].GTI; g[0] != 130 || g[1] != 160 {
		t.Errorf("GTI[1] = %v, want [130 160]", g)
	}
	if g := hours[2].GTI; g[0] != 280 || g[1] != 310 {
		t.Errorf("GTI[2] = %v, want [280 310]", g)
	}
	// temp/cloud come from the first array's response; null cloud → 0
	if hours[1].TempC != 15.3 || hours[1].CloudPct != 45 {
		t.Errorf("hour[1] temp=%v cloud=%v, want 15.3/45", hours[1].TempC, hours[1].CloudPct)
	}
	if hours[2].CloudPct != 0 {
		t.Errorf("null cloud = %v, want 0", hours[2].CloudPct)
	}
}

func TestFetchOpenMeteoNon200(t *testing.T) {
	httpc, seen := omServer(t, 503)
	_, err := FetchOpenMeteo(Location{}, []Array{{TiltDeg: 30, AzDeg: 180}}, "", httpc)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v, want status 503 mentioned", err)
	}
	if (*seen)[0].Get("timezone") != "auto" {
		t.Errorf("empty tz should fall back to auto, got %q", (*seen)[0].Get("timezone"))
	}
}

func TestFetchOpenMeteoNoArrays(t *testing.T) {
	if _, err := FetchOpenMeteo(Location{}, nil, "", http.DefaultClient); err == nil {
		t.Fatal("expected error for no arrays")
	}
}
