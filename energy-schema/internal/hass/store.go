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

// Entity is one HA state plus when it last changed.
type Entity struct {
	State       string
	LastChanged time.Time
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

// Store is a concurrency-safe snapshot of HA entities. Alongside the current
// values it remembers, per entity, the last value that was actually present —
// so a device that went "unavailable" can still show its last known state and
// how long ago it dropped out (a stabilizer falling back to bypass/transit).
type Store struct {
	mu       sync.RWMutex
	cur      map[string]Entity
	lastGood map[string]Entity
}

// NewStore returns an empty Store ready for use.
func NewStore() *Store {
	return &Store{cur: map[string]Entity{}, lastGood: map[string]Entity{}}
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

// Replace atomically swaps the current snapshot and refreshes last-good values.
func (s *Store) Replace(m map[string]Entity) {
	s.mu.Lock()
	for id, e := range m {
		if real(e.State) {
			s.lastGood[id] = e
		}
	}
	s.cur = m
	s.mu.Unlock()
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
