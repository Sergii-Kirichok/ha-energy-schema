package hass

import (
	"encoding/json"
	"os"
	"time"
)

// rollMax keeps a rolling peak AND trough over the last 24h in 5-minute buckets
// (288 slots). Each slot stores the max/min seen during one 5-min period and the
// period index it currently represents, so stale slots are ignored on read.
type rollMax struct {
	hi    [288]float64
	lo    [288]float64
	sum   [288]float64 // сумма значений в корзине — для среднего
	cnt   [288]float64 // число замеров в корзине
	stamp [288]int64
}

func (r *rollMax) add(now time.Time, v float64) {
	p := now.Unix() / 300 // current 5-min period index
	i := p % 288
	if r.stamp[i] != p { // slot belongs to an older period — reset it
		r.stamp[i] = p
		r.hi[i], r.lo[i] = v, v
		r.sum[i], r.cnt[i] = v, 1
	} else {
		if v > r.hi[i] {
			r.hi[i] = v
		}
		if v < r.lo[i] {
			r.lo[i] = v
		}
		r.sum[i] += v
		r.cnt[i]++
	}
}

// avg returns the time-weighted mean over the window (each active 5-min bucket
// counts equally). ok=false if the window has no data.
func (r *rollMax) avg(now time.Time) (float64, bool) {
	cutoff := now.Unix()/300 - 288
	total := 0.0
	n := 0
	for i := 0; i < 288; i++ {
		if r.stamp[i] > cutoff && r.cnt[i] > 0 {
			total += r.sum[i] / r.cnt[i] // среднее корзины
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return total / float64(n), true
}

func (r *rollMax) max(now time.Time) float64 { return r.maxWindow(now, 288) }

// maxWindow returns the peak over the last `buckets` 5-min buckets (288=24h, 144=12h).
func (r *rollMax) maxWindow(now time.Time, buckets int64) float64 {
	cutoff := now.Unix()/300 - buckets
	m := 0.0
	for i := 0; i < 288; i++ {
		if r.stamp[i] > cutoff && r.hi[i] > m {
			m = r.hi[i]
		}
	}
	return m
}

// min returns the trough over 24h; ok=false if the window has no data.
func (r *rollMax) min(now time.Time) (float64, bool) { return r.minWindow(now, 288) }

// minWindow returns the trough over the last `buckets` 5-min buckets (288=24h,
// 144=12h); ok=false if the window has no data.
func (r *rollMax) minWindow(now time.Time, buckets int64) (float64, bool) {
	cutoff := now.Unix()/300 - buckets
	m, ok := 0.0, false
	for i := 0; i < 288; i++ {
		if r.stamp[i] > cutoff && (!ok || r.lo[i] < m) {
			m, ok = r.lo[i], true
		}
	}
	return m, ok
}

// rollDTO is the JSON-serialisable form of a rollMax for persistence. The HA
// recorder often misses the real peaks our 5s poll catches, so we save the
// rolling buffers to disk ourselves — the 24h min/avg/max then survive restarts.
type rollDTO struct {
	Hi    []float64 `json:"hi"`
	Lo    []float64 `json:"lo"`
	Sum   []float64 `json:"sum"`
	Cnt   []float64 `json:"cnt"`
	Stamp []int64   `json:"st"`
}

func (r *rollMax) toDTO() rollDTO {
	d := rollDTO{Hi: make([]float64, 288), Lo: make([]float64, 288), Sum: make([]float64, 288), Cnt: make([]float64, 288), Stamp: make([]int64, 288)}
	for i := 0; i < 288; i++ {
		d.Hi[i], d.Lo[i], d.Sum[i], d.Cnt[i], d.Stamp[i] = r.hi[i], r.lo[i], r.sum[i], r.cnt[i], r.stamp[i]
	}
	return d
}

func (d rollDTO) into(r *rollMax) {
	for i := 0; i < 288 && i < len(d.Stamp); i++ {
		r.stamp[i] = d.Stamp[i]
		if i < len(d.Hi) {
			r.hi[i] = d.Hi[i]
		}
		if i < len(d.Lo) {
			r.lo[i] = d.Lo[i]
		}
		if i < len(d.Sum) {
			r.sum[i] = d.Sum[i]
		}
		if i < len(d.Cnt) {
			r.cnt[i] = d.Cnt[i]
		}
	}
}

// SaveRoll atomically writes the rolling 24h buffers of the given entities to a
// JSON file so their min/avg/max survive add-on restarts.
func (s *Store) SaveRoll(path string, entities []string) error {
	s.mu.RLock()
	m := make(map[string]rollDTO, len(entities))
	for _, e := range entities {
		if r := s.roll[e]; r != nil {
			m[e] = r.toDTO()
		}
	}
	s.mu.RUnlock()
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", b, 0644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// LoadRoll restores rolling buffers persisted by SaveRoll (no-op if file absent).
func (s *Store) LoadRoll(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]rollDTO
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	s.mu.Lock()
	for e, d := range m {
		r := &rollMax{}
		d.into(r)
		s.roll[e] = r
	}
	s.mu.Unlock()
	return nil
}

// DayEnergy returns the energy (kWh) integrated for a *_power entity since the
// start of the current local day (0 for others / before midnight rollover).
func (s *Store) DayEnergy(entity string) float64 {
	s.mu.RLock()
	v := s.dayEnergy[entity]
	s.mu.RUnlock()
	return v
}

// SetDayEnergy seeds today's accumulated energy (kWh) from history at startup.
func (s *Store) SetDayEnergy(entity string, kwh float64) {
	s.mu.Lock()
	s.dayEnergy[entity] = kwh
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

// Max12h returns the entity's peak over the last 12 hours.
func (s *Store) Max12h(entity string) float64 {
	s.mu.RLock()
	r := s.roll[entity]
	var v float64
	if r != nil {
		v = r.maxWindow(time.Now(), 144)
	}
	s.mu.RUnlock()
	return v
}

// Min12h returns the 12h trough for an entity (ok=false if no data yet).
func (s *Store) Min12h(entity string) (float64, bool) {
	s.mu.RLock()
	r := s.roll[entity]
	var v float64
	ok := false
	if r != nil {
		v, ok = r.minWindow(time.Now(), 144)
	}
	s.mu.RUnlock()
	return v, ok
}

// SeedDayMax seeds today's peak from a historical sample so the "max today"
// figure (e.g. sun generation) survives a restart instead of resetting to the
// values seen since boot. Only samples from the current local day count.
func (s *Store) SeedDayMax(entity string, t time.Time, v float64) {
	if t.Local().Format("2006-01-02") != time.Now().Format("2006-01-02") {
		return
	}
	s.mu.Lock()
	if v > s.dayMax[entity] {
		s.dayMax[entity] = v
	}
	s.mu.Unlock()
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

// Avg24h returns the entity's time-weighted average over the last 24 hours.
// ok=false if the entity has no data in the window yet.
func (s *Store) Avg24h(entity string) (float64, bool) {
	s.mu.RLock()
	r := s.roll[entity]
	var v float64
	ok := false
	if r != nil {
		v, ok = r.avg(time.Now())
	}
	s.mu.RUnlock()
	return v, ok
}

// DayMax returns the entity's peak numeric value seen today (0 if none yet).
func (s *Store) DayMax(entity string) float64 {
	s.mu.RLock()
	v := s.dayMax[entity]
	s.mu.RUnlock()
	return v
}
