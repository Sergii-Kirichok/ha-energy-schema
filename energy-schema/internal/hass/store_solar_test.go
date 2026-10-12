package hass

import (
	"math"
	"testing"
	"time"
)

func TestSolarForecastEmptyStore(t *testing.T) {
	s := NewStore()
	if _, ok := s.SolarForecast(); ok {
		t.Error("SolarForecast ok on empty store")
	}
	if a, b, c, src, ok := s.SolarTotals(); ok || a != 0 || b != 0 || c != 0 || src != "" {
		t.Errorf("SolarTotals empty = %v %v %v %q %v", a, b, c, src, ok)
	}
	if _, ok := s.SolarCloudNow(); ok {
		t.Error("SolarCloudNow ok on empty store")
	}
	if p, sd, up, ok := s.SolarProfile(); ok || p != [72]float64{} || !sd.IsZero() || !up.IsZero() {
		t.Error("SolarProfile must be zero/false on empty store")
	}
}

func TestSolarForecastRoundTrip(t *testing.T) {
	s := NewStore()
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)
	upd := start.Add(9 * time.Hour)
	snap := SolarForecastSnapshot{StartDay: start, TodayKWh: 31.5, TodayLeftKWh: 12.25, TomorrowKWh: 40,
		Source: "open-meteo", CloudNow: 0, UpdatedAt: upd}
	snap.HourlyKWh[12], snap.HourlyKWh[71] = 4.5, 0.1
	s.SetSolarForecast(snap)

	got, ok := s.SolarForecast()
	if !ok || got != snap {
		t.Errorf("SolarForecast = %+v ok=%v", got, ok)
	}
	if a, b, c, src, ok := s.SolarTotals(); !ok || a != 31.5 || b != 12.25 || c != 40 || src != "open-meteo" {
		t.Errorf("SolarTotals = %v %v %v %q %v", a, b, c, src, ok)
	}
	// 0 % cloud is a real datum (clear sky), not "missing".
	if c, ok := s.SolarCloudNow(); !ok || c != 0 {
		t.Errorf("SolarCloudNow = %v ok=%v, want 0/true", c, ok)
	}
	p, sd, up, ok := s.SolarProfile()
	if !ok || p[12] != 4.5 || p[71] != 0.1 || !sd.Equal(start) || !up.Equal(upd) {
		t.Errorf("SolarProfile = p12=%v p71=%v start=%v upd=%v ok=%v", p[12], p[71], sd, up, ok)
	}

	snap.CloudNow = -1 // нет данных Open-Meteo
	snap.Source = "clearsky"
	s.SetSolarForecast(snap)
	if _, ok := s.SolarCloudNow(); ok {
		t.Error("CloudNow=-1 must report ok=false")
	}
	if _, _, _, src, _ := s.SolarTotals(); src != "clearsky" {
		t.Errorf("later snapshot must replace earlier, source=%q", src)
	}
}

// hourAgo returns the start of a whole UTC hour n hours back (unix multiple of 3600).
func hourAgo(n int) time.Time {
	return time.Now().Truncate(time.Hour).Add(-time.Duration(n) * time.Hour)
}

func TestHourlyEnergy(t *testing.T) {
	s := NewStore()
	h := hourAgo(3)
	s.SeedRoll("sensor.pv_power", h, 1200)                    // bucket 0: avg 1800 W
	s.SeedRoll("sensor.pv_power", h.Add(time.Minute), 2400)   // same bucket
	s.SeedRoll("sensor.pv_power", h.Add(25*time.Minute), 600) // bucket 5
	s.SeedRoll("sensor.pv_power", h.Add(-time.Second), 9999)  // previous hour — excluded
	s.SeedRoll("sensor.pv_power", h.Add(time.Hour), 9999)     // next hour — excluded
	kwh, cov := s.HourlyEnergy("sensor.pv_power", h.Unix())
	// 1800 W × 5 мин + 600 W × 5 мин = 0.15 + 0.05 кВт·ч
	if !approx(kwh, 0.2) || cov != 2 {
		t.Errorf("HourlyEnergy = %v kWh cov=%d, want 0.2/2", kwh, cov)
	}
	if kwh, cov := s.HourlyEnergy("sensor.none", h.Unix()); kwh != 0 || cov != 0 {
		t.Errorf("unknown entity = %v/%d", kwh, cov)
	}
	// a bucket from 24h earlier occupies the same ring slot but must not count
	s.SeedRoll("sensor.stale", h.Add(-24*time.Hour), 5000)
	if kwh, cov := s.HourlyEnergy("sensor.stale", h.Unix()); kwh != 0 || cov != 0 || math.IsNaN(kwh) {
		t.Errorf("stale slot counted: %v/%d", kwh, cov)
	}
}

func TestHourlyMax(t *testing.T) {
	s := NewStore()
	h := hourAgo(2)
	s.SeedRoll("sensor.soc", h.Add(5*time.Minute), -7)
	s.SeedRoll("sensor.soc", h.Add(50*time.Minute), -3)
	s.SeedRoll("sensor.soc", h.Add(time.Hour), 100) // next hour
	if m, ok := s.HourlyMax("sensor.soc", h.Unix()); !ok || m != -3 {
		t.Errorf("HourlyMax = %v ok=%v, want -3/true (negatives must not clamp to 0)", m, ok)
	}
	if _, ok := s.HourlyMax("sensor.soc", hourAgo(5).Unix()); ok {
		t.Error("hour without buckets must report ok=false")
	}
	if _, ok := s.HourlyMax("sensor.none", h.Unix()); ok {
		t.Error("unknown entity must report ok=false")
	}
}

// localAt builds local hour h of the day `ahead` days from today.
func localAt(ahead, h int) time.Time {
	y, m, d := time.Now().AddDate(0, 0, ahead).Date()
	return time.Date(y, m, d, h, 0, 0, 0, time.Local)
}

func TestHourlyCloud(t *testing.T) {
	s := NewStore()
	if _, ok := s.HourlyCloud(0); ok != [24]bool{} {
		t.Error("empty hourly forecast must have empty mask")
	}
	s.SetHourly([]ForecastDay{
		{Time: localAt(1, 3).UTC(), Cloud: 80}, // stored in UTC, bucketed by local hour
		{Time: localAt(1, 12), Cloud: 15},
		{Time: localAt(0, 12), Cloud: 99}, // today — must not leak into tomorrow
		{Time: localAt(2, 0), Cloud: 50},  // day after's midnight
	})
	out, ok := s.HourlyCloud(1)
	if !ok[3] || out[3] != 80 || !ok[12] || out[12] != 15 {
		t.Errorf("tomorrow: out=%v ok=%v", out, ok)
	}
	n := 0
	for _, b := range ok {
		if b {
			n++
		}
	}
	if n != 2 {
		t.Errorf("tomorrow mask has %d hours, want 2", n)
	}
	if out, ok := s.HourlyCloud(2); !ok[0] || out[0] != 50 {
		t.Errorf("day+2 midnight: out0=%v ok0=%v", out[0], ok[0])
	}
}

func TestCloudForDay(t *testing.T) {
	s := NewStore()
	if _, ok := s.CloudForDay(0); ok {
		t.Error("CloudForDay ok with no hourly data")
	}
	s.SetHourly([]ForecastDay{
		{Time: localAt(1, 7), Cloud: 100}, // before 08:00 — excluded
		{Time: localAt(1, 8), Cloud: 20},  // 08:00 inclusive
		{Time: localAt(1, 12), Cloud: 40},
		{Time: localAt(1, 17), Cloud: 60},
		{Time: localAt(1, 18), Cloud: 100}, // 18:00 exclusive
		{Time: localAt(0, 23), Cloud: 100}, // today late evening
		{Time: localAt(2, 2), Cloud: 0},    // only night data for day+2
	})
	if c, ok := s.CloudForDay(1); !ok || !approx(c, 40) {
		t.Errorf("CloudForDay(1) = %v ok=%v, want 40", c, ok)
	}
	if _, ok := s.CloudForDay(2); ok {
		t.Error("night-only day must report ok=false")
	}
	if _, ok := s.CloudForDay(0); ok {
		t.Error("today has only 23:00 — no daylight data")
	}
}

func TestForecastInfo(t *testing.T) {
	s := NewStore()
	if _, _, ok := s.ForecastInfo(0); ok {
		t.Error("ForecastInfo ok with no forecast")
	}
	s.SetForecast([]ForecastDay{
		{Time: localAt(0, 12).UTC(), Condition: "sunny", Cloud: 5},
		{Time: localAt(1, 0), Condition: "rainy", Cloud: 90},
	})
	if c, cond, ok := s.ForecastInfo(1); !ok || c != 90 || cond != "rainy" {
		t.Errorf("ForecastInfo(1) = %v %q %v", c, cond, ok)
	}
	if c, cond, ok := s.ForecastInfo(0); !ok || c != 5 || cond != "sunny" {
		t.Errorf("ForecastInfo(0) = %v %q %v", c, cond, ok)
	}
	for _, d := range []int{-1, 2, 7} {
		if _, _, ok := s.ForecastInfo(d); ok {
			t.Errorf("ForecastInfo(%d) must be ok=false", d)
		}
	}
	// без валидных дат — запасной путь по индексу
	s.SetForecast([]ForecastDay{{Condition: "cloudy", Cloud: 70}, {Condition: "fog", Cloud: 100}})
	if c, cond, ok := s.ForecastInfo(1); !ok || c != 100 || cond != "fog" {
		t.Errorf("index fallback = %v %q %v", c, cond, ok)
	}
}
