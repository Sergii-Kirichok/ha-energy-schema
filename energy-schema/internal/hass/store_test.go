package hass

import (
	"testing"
	"time"
)

func TestStoreLastGoodAndLost(t *testing.T) {
	s := NewStore()
	s.ReplaceStates(map[string]string{"sensor.x": "5"})
	if !s.Available("sensor.x") || s.LastNum("sensor.x") != 5 {
		t.Fatalf("after real value: avail=%v last=%v", s.Available("sensor.x"), s.LastNum("sensor.x"))
	}
	if s.LostInfo("sensor.x") != "" {
		t.Errorf("LostInfo while available = %q, want empty", s.LostInfo("sensor.x"))
	}
	// device drops out: state unavailable, last changed 12 min ago
	s.Replace(map[string]Entity{
		"sensor.x": {State: "unavailable", LastChanged: time.Now().Add(-12 * time.Minute)},
	})
	if s.Available("sensor.x") {
		t.Error("Available should be false when unavailable")
	}
	if s.LastNum("sensor.x") != 5 {
		t.Errorf("LastNum after drop = %v, want 5 (last-good preserved)", s.LastNum("sensor.x"))
	}
	if got := s.LostInfo("sensor.x"); got != "12 мин" {
		t.Errorf("LostInfo = %q, want '12 мин'", got)
	}
}

func TestStoreAccessors(t *testing.T) {
	s := NewStore()
	s.ReplaceStates(map[string]string{
		"sensor.v":   " 12.5 ",
		"sensor.i":   "3.6",
		"switch.x":   "on",
		"switch.y":   "off",
		"sensor.bad": "n/a",
	})

	if got := s.State("sensor.v"); got != " 12.5 " {
		t.Errorf("State = %q", got)
	}
	if got := s.Num("sensor.v"); got != 12.5 { // trims whitespace
		t.Errorf("Num = %v, want 12.5", got)
	}
	if got := s.Int("sensor.i"); got != 4 { // rounds
		t.Errorf("Int = %d, want 4", got)
	}
	if !s.On("switch.x") {
		t.Error("On(switch.x) = false, want true")
	}
	if s.On("switch.y") {
		t.Error("On(switch.y) = true, want false")
	}
	// Unknown / unparseable => zero values.
	if got := s.Num("sensor.bad"); got != 0 {
		t.Errorf("Num(bad) = %v, want 0", got)
	}
	if got := s.Num("sensor.missing"); got != 0 {
		t.Errorf("Num(missing) = %v, want 0", got)
	}
	if s.On("switch.missing") {
		t.Error("On(missing) = true, want false")
	}
	if got := s.State("nope"); got != "" {
		t.Errorf("State(nope) = %q, want empty", got)
	}
}

func TestStoreReplaceIsAtomicSwap(t *testing.T) {
	s := NewStore()
	s.ReplaceStates(map[string]string{"a": "1"})
	if s.Num("a") != 1 {
		t.Fatal("first replace failed")
	}
	s.ReplaceStates(map[string]string{"b": "2"})
	if s.Num("a") != 0 {
		t.Error("old key still present after replace")
	}
	if s.Num("b") != 2 {
		t.Error("new key missing after replace")
	}
}
