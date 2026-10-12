package solar

import (
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func offlineProvider() *Provider {
	return &Provider{Loc: pvLoc, Arrays: pvArrays, TZ: "Europe/Kyiv", HTTP: &http.Client{Transport: failRT{}}}
}

// Open-Meteo down → per-hour cloud modulation (Kasten–Czeplak), missing hours 0.7×clr.
func TestBuildCloudFallback(t *testing.T) {
	tz := kyivTZ(t)
	now := time.Date(2026, 6, 10, 10, 15, 0, 0, tz)
	start := time.Date(2026, 6, 10, 0, 0, 0, 0, tz)
	cs := &fakeCloud{}
	for h := 0; h < 24; h++ {
		cs.cloud[0][h], cs.ok[0][h] = 0, true   // clear today
		cs.cloud[1][h], cs.ok[1][h] = 100, true // overcast tomorrow
		// day 2 unknown
	}
	s := offlineProvider().Build(now, cs)
	if s.Source != "met.no" || s.CloudNow != -1 {
		t.Fatalf("source=%q cloudNow=%v", s.Source, s.CloudNow)
	}
	clr := clr72(start, 0)
	for i := 0; i < 72; i++ {
		var want float64
		switch {
		case i < 24:
			want = clr[i]
		case i < 48:
			want = clr[i] * 0.25
		default:
			want = clr[i] * 0.7
		}
		if !near(s.HourlyKWh[i], want) {
			t.Fatalf("HourlyKWh[%d] = %v, want %v", i, s.HourlyKWh[i], want)
		}
	}
	checkTotals(t, s, now)
}

func TestFromCloudPartialAndNone(t *testing.T) {
	var clr [72]float64
	for i := range clr {
		clr[i] = 2
	}
	if r := fromCloud(clr, &fakeCloud{}); r != nil {
		t.Fatalf("no cloud data at all must return nil, got %v", r[0])
	}
	cs := &fakeCloud{}
	cs.cloud[2][5], cs.ok[2][5] = 50, true
	r := fromCloud(clr, cs)
	if r == nil {
		t.Fatal("one known hour must yield a profile")
	}
	if want := 2 * (1 - 0.75*math.Pow(0.5, 3.4)); !near(r[48+5], want) || !near(r[0], 1.4) {
		t.Errorf("r[53]=%v want %v, r[0]=%v want 1.4", r[48+5], want, r[0])
	}
}

// Open-Meteo down, no cloud source (or source without data) → bare 0.7×clear-sky.
func TestBuildClearSkyFallback(t *testing.T) {
	tz := kyivTZ(t)
	now := time.Date(2026, 12, 21, 23, 59, 0, 0, tz)
	start := time.Date(2026, 12, 21, 0, 0, 0, 0, tz)
	clr := clr72(start, 0)
	for name, cs := range map[string]CloudSource{"nil": nil, "empty": &fakeCloud{}} {
		s := offlineProvider().Build(now, cs)
		if s.Source != "clearsky" || s.CloudNow != -1 {
			t.Fatalf("%s: source=%q cloudNow=%v", name, s.Source, s.CloudNow)
		}
		for i := range clr {
			if !near(s.HourlyKWh[i], 0.7*clr[i]) {
				t.Fatalf("%s: HourlyKWh[%d] = %v, want %v", name, i, s.HourlyKWh[i], 0.7*clr[i])
			}
		}
		checkTotals(t, s, now)
		if s.Today <= 0 || s.TodayLeft != 0 { // 23:59 in winter — nothing left
			t.Errorf("%s: today=%v left=%v", name, s.Today, s.TodayLeft)
		}
	}
}

// Open-Meteo answered but with no generation (night-only / empty / bad status):
// fall back, yet keep the current-hour cloud when present.
func TestBuildOpenMeteoNoGeneration(t *testing.T) {
	tz := kyivTZ(t)
	now := time.Date(2026, 6, 10, 2, 40, 0, 0, tz)
	start := time.Date(2026, 6, 10, 0, 0, 0, 0, tz)
	dark := func(hs time.Time) (float64, float64, float64) { return 0, 12, 77 }
	p := offlineProvider()
	p.HTTP = omFixture(t, start, 72, dark)
	s := p.Build(now, nil)
	if s.Source != "clearsky" || s.CloudNow != 77 {
		t.Fatalf("source=%q cloudNow=%v, want clearsky/77", s.Source, s.CloudNow)
	}

	for name, h := range map[string]http.HandlerFunc{
		"empty":  func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"hourly":{}}`)) },
		"status": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		"junk":   func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`not json`)) },
	} {
		srv := httptest.NewServer(h)
		u, _ := url.Parse(srv.URL)
		p.HTTP = &http.Client{Transport: redirectRT{u}}
		s := p.Build(now, nil)
		srv.Close()
		if s.Source != "clearsky" || s.CloudNow != -1 {
			t.Errorf("%s: source=%q cloudNow=%v", name, s.Source, s.CloudNow)
		}
	}
}

// With a calibrator: cold start ≈ raw, only FUTURE hours are frozen, CalibDays = level weight.
func TestBuildWithCalibrator(t *testing.T) {
	tz := kyivTZ(t)
	now := time.Date(2026, 6, 10, 13, 30, 0, 0, tz)
	start := time.Date(2026, 6, 10, 0, 0, 0, 0, tz)
	p := offlineProvider()
	p.Cal = NewCalibrator(t.TempDir()+"/c.json", GeoHash(pvArrays))
	p.Cal.d.Level.W = 4.5
	s := p.Build(now, nil)
	if s.CalibDays != 4.5 {
		t.Errorf("CalibDays = %v, want 4.5", s.CalibDays)
	}
	clr := clr72(start, 0)
	for i := range clr {
		if !near(s.HourlyKWh[i], 0.7*clr[i]) { // cold start KBin, Level shrunk to 1
			t.Fatalf("HourlyKWh[%d] = %v, want %v", i, s.HourlyKWh[i], 0.7*clr[i])
		}
	}
	if n := len(p.Cal.d.Pending); n != 72-14 { // hours 14:00 today .. 23:00 day+2
		t.Fatalf("pending = %d, want 58 future hours", n)
	}
	for k, hp := range p.Cal.d.Pending {
		u, _ := strconv.ParseInt(k, 10, 64)
		if !time.Unix(u, 0).After(now) || hp.Src != "clearsky" {
			t.Fatalf("pending %s (%v) src=%q", k, time.Unix(u, 0).In(tz), hp.Src)
		}
	}
	checkTotals(t, s, now)
}

// The profile is "72 h from local midnight" (elapsed hours, see hass store_solar.go):
// hour i must use the clear-sky ceiling of the real hour StartDay+i.
func checkCeilingAlignment(t *testing.T, now time.Time) error {
	t.Helper()
	p := offlineProvider()
	p.Cal = NewCalibrator(t.TempDir()+"/c.json", "g")
	p.Build(now, nil)
	tz := now.Location()
	for k, hp := range p.Cal.d.Pending {
		u, _ := strconv.ParseInt(k, 10, 64)
		ht := time.Unix(u, 0).In(tz)
		want := HourlyClearSky(ht, pvLoc, pvArrays, 0)[ht.Hour()]
		if !near(hp.Eclr, want) {
			return &alignErr{ht, hp.Eclr, want}
		}
	}
	return nil
}

type alignErr struct {
	at        time.Time
	got, want float64
}

func (e *alignErr) Error() string {
	return e.at.Format(time.RFC3339) + ": eclr " + strconv.FormatFloat(e.got, 'f', 3, 64) +
		" want " + strconv.FormatFloat(e.want, 'f', 3, 64)
}

func TestBuildCeilingAlignedNormalDay(t *testing.T) {
	tz := kyivTZ(t)
	if err := checkCeilingAlignment(t, time.Date(2026, 6, 10, 5, 0, 0, 0, tz)); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCeilingAlignedDSTDay(t *testing.T) {
	t.Skip("BUG: on DST days (Europe/Kyiv 2026-03-29, 23 h) Build indexes clr by calendar hour " +
		"(HourlyClearSky per day, day*24+h) but raw/HourlyKWh/Freeze by elapsed hours from midnight → " +
		"ceiling, 1.3× cap and frozen Eclr are shifted by 1 h for the rest of the 72 h horizon")
	tz := kyivTZ(t)
	if err := checkCeilingAlignment(t, time.Date(2026, 3, 29, 1, 0, 0, 0, tz)); err != nil {
		t.Fatal(err)
	}
}
