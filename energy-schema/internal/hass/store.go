// Package hass provides a concurrency-safe snapshot of Home Assistant entity
// states and a small REST client that refreshes it.
package hass

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Entity is one HA state plus when it last changed and its scalar attributes.
type Entity struct {
	State       string
	LastChanged time.Time
	Attrs       map[string]string // scalar attributes only (lists/objects skipped)
}

// real reports whether a state carries an actual value (not offline/empty).
func real(s string) bool { return s != "" && s != "unavailable" && s != "unknown" }

func parseF(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// parseFloatOK reports whether s is a real numeric value (and returns it).
func parseFloatOK(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}

// rollMax keeps a rolling peak AND trough over the last 24h in 5-minute buckets
// (288 slots). Each slot stores the max/min seen during one 5-min period and the
// period index it currently represents, so stale slots are ignored on read.
type rollMax struct {
	hi    [288]float64
	lo    [288]float64
	stamp [288]int64
}

func (r *rollMax) add(now time.Time, v float64) {
	p := now.Unix() / 300 // current 5-min period index
	i := p % 288
	if r.stamp[i] != p { // slot belongs to an older period — reset it
		r.stamp[i] = p
		r.hi[i], r.lo[i] = v, v
	} else {
		if v > r.hi[i] {
			r.hi[i] = v
		}
		if v < r.lo[i] {
			r.lo[i] = v
		}
	}
}

func (r *rollMax) max(now time.Time) float64 {
	cutoff := now.Unix()/300 - 288 // anything older than 24h is excluded
	m := 0.0
	for i := 0; i < 288; i++ {
		if r.stamp[i] > cutoff && r.hi[i] > m {
			m = r.hi[i]
		}
	}
	return m
}

// min returns the trough over the window; ok=false if the window has no data.
func (r *rollMax) min(now time.Time) (float64, bool) {
	cutoff := now.Unix()/300 - 288
	m, ok := 0.0, false
	for i := 0; i < 288; i++ {
		if r.stamp[i] > cutoff && (!ok || r.lo[i] < m) {
			m, ok = r.lo[i], true
		}
	}
	return m, ok
}

// Store is a concurrency-safe snapshot of HA entities. Alongside the current
// values it remembers, per entity, the last value that was actually present —
// so a device that went "unavailable" can still show its last known state and
// how long ago it dropped out (a stabilizer falling back to bypass/transit).
type Store struct {
	mu       sync.RWMutex
	cur      map[string]Entity
	lastGood map[string]Entity
	forecast []ForecastDay
	dayMax   map[string]float64
	dayYMD   string
	roll     map[string]*rollMax // rolling 24h peak per numeric entity
	// эмпирическая база генерации из долгосрочной статистики (последние ~10 дней)
	pvClearKWh float64 // лучший суточный день — оценка «ясного дня» сезона
	pvAvgKWh   float64 // средняя суточная генерация
	pvDaysN    int     // сколько суток в выборке
}

// NewStore returns an empty Store ready for use.
func NewStore() *Store {
	return &Store{cur: map[string]Entity{}, lastGood: map[string]Entity{}, dayMax: map[string]float64{}, roll: map[string]*rollMax{}}
}

// FromStates wraps a plain id->state map into entities (zero timestamps).
// Handy for tests; the live client builds entities with real last_changed.
func FromStates(m map[string]string) map[string]Entity {
	out := make(map[string]Entity, len(m))
	for k, v := range m {
		out[k] = Entity{State: v}
	}
	return out
}

// Replace atomically swaps the current snapshot, refreshes last-good values and
// tracks each numeric entity's peak for the current calendar day (reset at midnight).
func (s *Store) Replace(m map[string]Entity) {
	s.mu.Lock()
	now := time.Now()
	ymd := now.Format("2006-01-02")
	if ymd != s.dayYMD {
		s.dayMax = map[string]float64{}
		s.dayYMD = ymd
	}
	for id, e := range m {
		if real(e.State) {
			s.lastGood[id] = e
			if v, ok := parseFloatOK(e.State); ok {
				if v > s.dayMax[id] {
					s.dayMax[id] = v
				}
				r := s.roll[id]
				if r == nil {
					r = &rollMax{}
					s.roll[id] = r
				}
				r.add(now, v)
			}
		}
	}
	s.cur = m
	s.mu.Unlock()
}

// Max24h returns the entity's peak numeric value over the last 24 hours
// (rolling window, not the calendar day). 0 if never seen.
func (s *Store) Max24h(entity string) float64 {
	s.mu.RLock()
	r := s.roll[entity]
	var v float64
	if r != nil {
		v = r.max(time.Now())
	}
	s.mu.RUnlock()
	return v
}

// SeedRoll injects a historical sample (with its real timestamp) into an
// entity's rolling 24h window, so the min/max survive an add-on restart.
func (s *Store) SeedRoll(entity string, t time.Time, v float64) {
	s.mu.Lock()
	r := s.roll[entity]
	if r == nil {
		r = &rollMax{}
		s.roll[entity] = r
	}
	r.add(t, v)
	s.mu.Unlock()
}

// Min24h returns the entity's lowest numeric value over the last 24 hours.
// ok=false if the entity has no data in the window yet.
func (s *Store) Min24h(entity string) (float64, bool) {
	s.mu.RLock()
	r := s.roll[entity]
	var v float64
	ok := false
	if r != nil {
		v, ok = r.min(time.Now())
	}
	s.mu.RUnlock()
	return v, ok
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

// DayMax returns the entity's peak numeric value seen today (0 if none yet).
func (s *Store) DayMax(entity string) float64 {
	s.mu.RLock()
	v := s.dayMax[entity]
	s.mu.RUnlock()
	return v
}

// ReplaceStates is a convenience wrapper for plain id->state maps (tests).
func (s *Store) ReplaceStates(m map[string]string) { s.Replace(FromStates(m)) }

func (s *Store) state(e string) string {
	s.mu.RLock()
	v := s.cur[e].State
	s.mu.RUnlock()
	return v
}

// State returns the raw current state string for an entity ("" if unknown).
func (s *Store) State(entity string) string { return s.state(entity) }

// Num parses the current entity state as float64 (0 on error / unknown).
func (s *Store) Num(entity string) float64 { return parseF(s.state(entity)) }

// Int returns Num rounded to the nearest integer.
func (s *Store) Int(entity string) int { return int(math.Round(s.Num(entity))) }

// On reports whether the current entity state is exactly "on".
func (s *Store) On(entity string) bool { return s.state(entity) == "on" }

// Available reports whether the entity currently carries a real value.
func (s *Store) Available(entity string) bool { return real(s.state(entity)) }

// LastState returns the last value the entity actually had ("" if never seen).
func (s *Store) LastState(entity string) string {
	s.mu.RLock()
	v := s.lastGood[entity].State
	s.mu.RUnlock()
	return v
}

// LastNum / LastInt parse the last-good value.
func (s *Store) LastNum(entity string) float64 { return parseF(s.LastState(entity)) }
func (s *Store) LastInt(entity string) int     { return int(math.Round(s.LastNum(entity))) }

// LostInfo returns a short human duration since the entity went unavailable
// ("12 мин", "1ч 05м"), or "" if it is currently available / never tracked.
func (s *Store) LostInfo(entity string) string {
	s.mu.RLock()
	cur := s.cur[entity]
	s.mu.RUnlock()
	if real(cur.State) || cur.LastChanged.IsZero() {
		return ""
	}
	return humanDur(time.Since(cur.LastChanged))
}

func (s *Store) attr(entity, key string) string {
	s.mu.RLock()
	a := s.cur[entity].Attrs
	s.mu.RUnlock()
	if a == nil {
		return ""
	}
	return a[key]
}

// Attr returns a scalar attribute value ("" if absent).
func (s *Store) Attr(entity, key string) string { return s.attr(entity, key) }

// AttrNum parses a scalar attribute as float64 (0 on error).
func (s *Store) AttrNum(entity, key string) float64 { return parseF(s.attr(entity, key)) }

// HoursUntil parses an attribute as an RFC3339 timestamp and returns the hours
// from now until it (clamped to >=0, 0 if absent/unparseable).
func (s *Store) HoursUntil(entity, key string) float64 {
	t, err := time.Parse(time.RFC3339Nano, s.attr(entity, key))
	if err != nil {
		return 0
	}
	if h := time.Until(t).Hours(); h > 0 {
		return h
	}
	return 0
}

// SetForecast stores the daily weather forecast (from Client.DailyForecast).
func (s *Store) SetForecast(days []ForecastDay) {
	s.mu.Lock()
	s.forecast = days
	s.mu.Unlock()
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

func humanDur(d time.Duration) string {
	if d < time.Minute {
		return "<1 мин"
	}
	m := int(d.Minutes())
	if m < 60 {
		return fmt.Sprintf("%d мин", m)
	}
	return fmt.Sprintf("%dч %02dм", m/60, m%60)
}
