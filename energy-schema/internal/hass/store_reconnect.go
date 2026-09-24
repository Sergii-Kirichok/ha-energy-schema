package hass

import "time"

// reconnTrack tracks the inverter→grid reconnection wait. The device gives only
// the configured delay (no live countdown), so we time it ourselves — but the
// retry is detected from the device: each time the inverter re-enters the wait
// (grid present, relay not yet bonded) the countdown restarts from the top.
type reconnTrack struct {
	active   bool
	start    time.Time
	attempts int
	total    float64
}

func (s *Store) updateReconnectAt(now time.Time, waiting, gridPresent bool, total float64) {
	s.mu.Lock()
	if !gridPresent {
		s.recon.attempts = 0 // новая авария сети — счётчик попыток с нуля
	}
	if waiting && !s.recon.active { // (пере)вошли в ожидание → отсчёт сначала
		s.recon.start = now
		s.recon.attempts++
	}
	s.recon.active = waiting
	s.recon.total = total
	s.mu.Unlock()
}

// UpdateReconnect feeds the reconnect state each poll. waiting = grid present but
// inverter not yet bonded to it; gridPresent resets the attempt counter on a new
// outage; total = configured reconnection delay (s).
func (s *Store) UpdateReconnect(waiting, gridPresent bool, total float64) {
	s.updateReconnectAt(time.Now(), waiting, gridPresent, total)
}

func (s *Store) reconnectInfoAt(now time.Time) (remaining, total float64, active bool, attempts int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.recon.active {
		return 0, s.recon.total, false, s.recon.attempts
	}
	rem := s.recon.total - now.Sub(s.recon.start).Seconds()
	if rem < 0 {
		rem = 0
	}
	return rem, s.recon.total, true, s.recon.attempts
}

// ReconnectInfo returns the live reconnect countdown: remaining seconds, the
// total delay, whether a reconnect is in progress, and the attempt number.
func (s *Store) ReconnectInfo() (remaining, total float64, active bool, attempts int) {
	return s.reconnectInfoAt(time.Now())
}
