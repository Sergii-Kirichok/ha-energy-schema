package solar

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var (
	pvLoc    = Location{Lat: 50.45, Lon: 30.52, AltM: 150}
	pvArrays = []Array{{Name: "south", KWp: 5, TiltDeg: 30, AzDeg: 180}}
)

func kyivTZ(t *testing.T) *time.Location {
	t.Helper()
	tz, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	return tz
}

// failRT guarantees the provider never reaches the real api.open-meteo.com.
type failRT struct{}

func (failRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }

// omHour describes one fixture hour by its local HourStart (label-1h).
type omHour func(hourStart time.Time) (gti, temp, cloud float64)

// omFixture serves n hourly Open-Meteo labels starting at firstLabel (local).
func omFixture(t *testing.T, firstLabel time.Time, n int, f omHour) *http.Client {
	t.Helper()
	var ts, g, tc, cc []string
	for k := 0; k < n; k++ {
		lbl := firstLabel.Add(time.Duration(k) * time.Hour)
		gti, temp, cloud := f(lbl.Add(-time.Hour))
		ts = append(ts, `"`+lbl.Format("2006-01-02T15:04")+`"`)
		g = append(g, fmt.Sprint(gti))
		tc = append(tc, fmt.Sprint(temp))
		cc = append(cc, fmt.Sprint(cloud))
	}
	body := fmt.Sprintf(`{"hourly":{"time":[%s],"global_tilted_irradiance":[%s],"temperature_2m":[%s],"cloud_cover":[%s]}}`,
		strings.Join(ts, ","), strings.Join(g, ","), strings.Join(tc, ","), strings.Join(cc, ","))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return &http.Client{Transport: redirectRT{u}}
}

// dayGTI: 500 W/m² between 06:00 and 19:00 local, cloud = local hour number.
func dayGTI(hs time.Time) (float64, float64, float64) {
	if h := hs.Hour(); h >= 6 && h < 19 {
		return 500, 25, float64(h)
	}
	return 0, 10, float64(hs.Hour())
}

type fakeCloud struct {
	cloud [3][24]float64
	ok    [3][24]bool
}

func (f *fakeCloud) HourlyCloud(d int) ([24]float64, [24]bool) { return f.cloud[d], f.ok[d] }

func clr72(start time.Time, ac float64) [72]float64 {
	var c [72]float64
	for d := 0; d < 3; d++ {
		h := HourlyClearSky(start.AddDate(0, 0, d), pvLoc, pvArrays, ac)
		copy(c[d*24:], h[:])
	}
	return c
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// checkTotals asserts Today/Tomorrow/TodayLeft are consistent with the profile
// and every value is finite and non-negative.
func checkTotals(t *testing.T, s Snapshot, now time.Time) {
	t.Helper()
	var today, tomorrow float64
	for i, v := range s.HourlyKWh {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			t.Fatalf("HourlyKWh[%d] = %v", i, v)
		}
		if i < 24 {
			today += v
		} else if i < 48 {
			tomorrow += v
		}
	}
	cur := now.Sub(s.StartDay).Hours()
	ch := int(cur)
	left := s.HourlyKWh[ch] * (1 - (cur - float64(ch)))
	for h := ch + 1; h < 24; h++ {
		left += s.HourlyKWh[h]
	}
	if !near(s.Today, today) || !near(s.Tomorrow, tomorrow) || !near(s.TodayLeft, left) {
		t.Fatalf("totals today=%v/%v tomorrow=%v/%v left=%v/%v", s.Today, today, s.Tomorrow, tomorrow, s.TodayLeft, left)
	}
	if s.TodayLeft > s.Today+1e-9 {
		t.Fatalf("TodayLeft %v > Today %v", s.TodayLeft, s.Today)
	}
}

func TestBuildOpenMeteo(t *testing.T) {
	tz := kyivTZ(t)
	now := time.Date(2026, 6, 10, 13, 30, 0, 0, tz)
	start := time.Date(2026, 6, 10, 0, 0, 0, 0, tz)
	p := &Provider{Loc: pvLoc, Arrays: pvArrays, TZ: "Europe/Kyiv", HTTP: omFixture(t, start, 72, dayGTI)}
	s := p.Build(now, nil)
	if s.Source != "open-meteo" || !s.StartDay.Equal(start) || s.CalibDays != 0 {
		t.Fatalf("source=%q start=%v calib=%v", s.Source, s.StartDay, s.CalibDays)
	}
	if s.CloudNow != 13 {
		t.Errorf("CloudNow = %v, want 13 (cloud of the 13:00 hour)", s.CloudNow)
	}
	want := ArrayPowerKW(pvArrays[0], 500, 25)
	clr := clr72(start, 0)
	sumToday := 0.0
	for i := 0; i < 72; i++ { // expected = GTI power, capped at 1.3× clear-sky
		e := 0.0
		if h := i % 24; h >= 6 && h < 19 {
			e = math.Min(want, 1.3*clr[i])
		}
		if !near(s.HourlyKWh[i], e) {
			t.Errorf("HourlyKWh[%d] = %v, want %v", i, s.HourlyKWh[i], e)
		}
		if i < 24 {
			sumToday += e
		}
	}
	if !near(s.HourlyKWh[13], want) { // midday is not capped
		t.Errorf("midday = %v, want uncapped %v", s.HourlyKWh[13], want)
	}
	checkTotals(t, s, now)
	if !near(s.Today, sumToday) {
		t.Errorf("Today = %v, want %v", s.Today, sumToday)
	}
}

// Unphysical GTI is capped at 1.3× clear-sky; AC limit clips the array sum.
func TestBuildCapsAndACLimit(t *testing.T) {
	tz := kyivTZ(t)
	now := time.Date(2026, 6, 10, 9, 0, 0, 0, tz)
	start := time.Date(2026, 6, 10, 0, 0, 0, 0, tz)
	huge := func(time.Time) (float64, float64, float64) { return 5000, 20, 0 }

	p := &Provider{Loc: pvLoc, Arrays: pvArrays, TZ: "Europe/Kyiv", HTTP: omFixture(t, start, 72, huge)}
	s := p.Build(now, nil)
	clr := clr72(start, 0)
	for i, v := range s.HourlyKWh {
		if clr[i] > 0 && v > 1.3*clr[i]+1e-9 { // clr==0 → no ceiling (lim>0 guard)
			t.Fatalf("HourlyKWh[%d] = %v above 1.3×clr %v", i, v, clr[i])
		}
	}
	if !near(s.HourlyKWh[12], 1.3*clr[12]) || clr[12] <= 0 {
		t.Errorf("noon = %v, want capped 1.3×%v", s.HourlyKWh[12], clr[12])
	}
	checkTotals(t, s, now)

	p.ACLimit = 2
	s = p.Build(now, nil)
	if !near(s.HourlyKWh[12], 2) {
		t.Errorf("AC-limited noon = %v, want 2", s.HourlyKWh[12])
	}
	for i, v := range s.HourlyKWh {
		if v > 2+1e-9 {
			t.Fatalf("HourlyKWh[%d] = %v above AC limit", i, v)
		}
	}
}

// Labels from the previous day are outside the horizon and must not leak into today.
func TestBuildIgnoresHoursOutsideHorizon(t *testing.T) {
	tz := kyivTZ(t)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, tz)
	yesterday := time.Date(2026, 6, 9, 0, 0, 0, 0, tz)
	onlyYesterday := func(hs time.Time) (float64, float64, float64) {
		if hs.Day() == 9 && hs.Hour() >= 6 && hs.Hour() < 19 {
			return 500, 20, 0
		}
		if hs.Day() == 10 && hs.Hour() == 12 {
			return 300, 20, 0
		}
		return 0, 20, 0
	}
	p := &Provider{Loc: pvLoc, Arrays: pvArrays, TZ: "Europe/Kyiv", HTTP: omFixture(t, yesterday, 96, onlyYesterday)}
	s := p.Build(now, nil)
	if s.Source != "open-meteo" {
		t.Fatalf("source = %q", s.Source)
	}
	want := ArrayPowerKW(pvArrays[0], 300, 20)
	if !near(s.Today, want) || !near(s.HourlyKWh[12], want) {
		t.Errorf("Today = %v, H[12] = %v, want only today's single hour %v", s.Today, s.HourlyKWh[12], want)
	}
}
