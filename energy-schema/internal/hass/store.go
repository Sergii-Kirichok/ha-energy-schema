// Package hass provides a concurrency-safe snapshot of Home Assistant entity
// states and a small REST client that refreshes it.
package hass

import (
	"math"
	"strconv"
	"strings"
	"sync"
)

// Store is a concurrency-safe snapshot of HA entity states (entity_id -> state).
type Store struct {
	mu     sync.RWMutex
	states map[string]string
}

// NewStore returns an empty Store ready for use.
func NewStore() *Store {
	return &Store{states: map[string]string{}}
}

// Replace atomically swaps the whole state map.
func (s *Store) Replace(m map[string]string) {
	s.mu.Lock()
	s.states = m
	s.mu.Unlock()
}

// State returns the raw state string for an entity ("" if unknown).
func (s *Store) State(entity string) string {
	s.mu.RLock()
	v := s.states[entity]
	s.mu.RUnlock()
	return v
}

// Num parses the entity state as float64 (0 on error / unknown).
func (s *Store) Num(entity string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s.State(entity)), 64)
	if err != nil {
		return 0
	}
	return f
}

// Int returns Num rounded to the nearest integer.
func (s *Store) Int(entity string) int { return int(math.Round(s.Num(entity))) }

// On reports whether the entity state is exactly "on".
func (s *Store) On(entity string) bool { return s.State(entity) == "on" }
