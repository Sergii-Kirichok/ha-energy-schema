package hass

import "time"

// SolarForecastSnapshot — готовый прогноз генерации, который провайдер кладёт в
// Store, а рендер/автономия читают (как SetForecast/SetHourly). HourlyKWh —
// почасовой профиль кВт·ч на 72ч от локальной полуночи StartDay (день[0]=сегодня).
type SolarForecastSnapshot struct {
	StartDay                            time.Time
	HourlyKWh                           [72]float64
	TodayKWh, TodayLeftKWh, TomorrowKWh float64
	Source                              string  // "open-meteo" | "met.no" | "clearsky"
	CloudNow                            float64 // облачность Open-Meteo на текущий час, % (-1 нет)
	UpdatedAt                           time.Time
}

// SetSolarForecast stores the latest solar generation forecast snapshot.
func (s *Store) SetSolarForecast(snap SolarForecastSnapshot) {
	s.mu.Lock()
	s.solarFc, s.solarFcOK = snap, true
	s.mu.Unlock()
}

// SolarForecast returns the latest forecast snapshot (ok=false if none yet).
func (s *Store) SolarForecast() (SolarForecastSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.solarFc, s.solarFcOK
}

// SolarTotals exposes the forecast day totals as primitives (for scada.State,
// keeping the renderer decoupled from the snapshot type).
func (s *Store) SolarTotals() (today, todayLeft, tomorrow float64, source string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.solarFcOK {
		return 0, 0, 0, "", false
	}
	f := s.solarFc
	return f.TodayKWh, f.TodayLeftKWh, f.TomorrowKWh, f.Source, true
}

// SolarCloudNow returns the Open-Meteo cloud cover (%) for the current hour from
// the latest snapshot (ok=false if no snapshot or no Open-Meteo cloud datum).
func (s *Store) SolarCloudNow() (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.solarFcOK || s.solarFc.CloudNow < 0 {
		return 0, false
	}
	return s.solarFc.CloudNow, true
}

// SolarProfile exposes the 72h hourly kWh profile + its start day and update time.
func (s *Store) SolarProfile() (profile [72]float64, startDay, updated time.Time, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.solarFcOK {
		return profile, time.Time{}, time.Time{}, false
	}
	return s.solarFc.HourlyKWh, s.solarFc.StartDay, s.solarFc.UpdatedAt, true
}

// HourlyEnergy integrates the entity's energy (kWh) over the hour starting at
// hourUnix from its 5-min roll buckets, plus how many of the 12 buckets had data
// (coverage). Used to learn the calibration on real generation per hour.
func (s *Store) HourlyEnergy(entity string, hourUnix int64) (float64, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := s.roll[entity]
	if r == nil {
		return 0, 0
	}
	p0 := hourUnix / 300
	kwh := 0.0
	cov := 0
	for k := int64(0); k < 12; k++ {
		p := p0 + k
		i := p % 288
		if r.stamp[i] == p && r.cnt[i] > 0 {
			kwh += (r.sum[i] / r.cnt[i]) / 1000.0 / 12.0 // средняя мощность Вт × (5 мин = 1/12 ч)
			cov++
		}
	}
	return kwh, cov
}

// HourlyMax returns the peak value of an entity over the hour starting at
// hourUnix from the roll buckets (ok=false if no data) — e.g. max SOC for the
// full-battery calibration gate.
func (s *Store) HourlyMax(entity string, hourUnix int64) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := s.roll[entity]
	if r == nil {
		return 0, false
	}
	p0 := hourUnix / 300
	m := 0.0
	ok := false
	for k := int64(0); k < 12; k++ {
		p := p0 + k
		i := p % 288
		if r.stamp[i] == p && (!ok || r.hi[i] > m) {
			m, ok = r.hi[i], true
		}
	}
	return m, ok
}

// HourlyCloud returns per-local-hour cloud coverage (%) for the day daysAhead
// from the hourly forecast, plus a presence mask — for the Met.no fallback.
func (s *Store) HourlyCloud(daysAhead int) ([24]float64, [24]bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out [24]float64
	var ok [24]bool
	ty, tm, td := time.Now().AddDate(0, 0, daysAhead).Date()
	for _, h := range s.hourly {
		lt := h.Time.Local()
		if y, m, d := lt.Date(); y == ty && m == tm && d == td {
			out[lt.Hour()] = h.Cloud
			ok[lt.Hour()] = true
		}
	}
	return out, ok
}

// SetPVStats stores the empirical generation baseline derived from long-term
// statistics: best day (clear-day proxy), average day, and sample size.
func (s *Store) SetPVStats(clearKWh, avgKWh float64, n int) {
	s.mu.Lock()
	s.pvClearKWh, s.pvAvgKWh, s.pvDaysN = clearKWh, avgKWh, n
	s.mu.Unlock()
}

// PVClearDayKWh returns the empirical clear-day generation (best recent day),
// or 0 if no statistics have been loaded yet.
func (s *Store) PVClearDayKWh() float64 {
	s.mu.RLock()
	v := s.pvClearKWh
	s.mu.RUnlock()
	return v
}

// PVRecent returns the average recent daily generation and the sample size.
func (s *Store) PVRecent() (float64, int) {
	s.mu.RLock()
	a, n := s.pvAvgKWh, s.pvDaysN
	s.mu.RUnlock()
	return a, n
}

// SetForecast stores the daily weather forecast (from Client.DailyForecast).
func (s *Store) SetForecast(days []ForecastDay) {
	s.mu.Lock()
	s.forecast = days
	s.mu.Unlock()
}

// SetHourly stores the hourly forecast (carries cloud_coverage).
func (s *Store) SetHourly(h []ForecastDay) {
	s.mu.Lock()
	s.hourly = h
	s.mu.Unlock()
}

// CloudForDay returns the average cloud coverage (%) over the daylight hours
// (local 08:00–18:00) of the day daysAhead from now, from the hourly forecast.
// ok=false when the hourly forecast has no daytime data for that day — this is
// far more accurate than mapping a single daily condition (e.g. a "rainy" day
// that's actually clear in the morning) to a flat cloud %.
func (s *Store) CloudForDay(daysAhead int) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ty, tm, td := time.Now().AddDate(0, 0, daysAhead).Date()
	sum, n := 0.0, 0
	for _, h := range s.hourly {
		lt := h.Time.Local()
		y, m, d := lt.Date()
		if y == ty && m == tm && d == td && lt.Hour() >= 8 && lt.Hour() < 18 {
			sum += h.Cloud
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// ForecastInfo returns the forecast cloud coverage (%) and condition daysAhead
// days from now (0 = today, 1 = tomorrow). ok=false when no forecast covers
// that day. Some providers omit cloud_coverage in daily forecasts (0 + condition).
func (s *Store) ForecastInfo(daysAhead int) (float64, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wy, wm, wd := time.Now().AddDate(0, 0, daysAhead).Date()
	for _, d := range s.forecast {
		ly, lm, ld := d.Time.Local().Date()
		if ly == wy && lm == wm && ld == wd {
			return d.Cloud, d.Condition, true
		}
	}
	if daysAhead >= 0 && daysAhead < len(s.forecast) { // запасной путь: список с сегодня по порядку
		return s.forecast[daysAhead].Cloud, s.forecast[daysAhead].Condition, true
	}
	return 0, "", false
}
