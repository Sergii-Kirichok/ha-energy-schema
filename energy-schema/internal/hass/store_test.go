package hass

import "testing"

func TestStoreAccessors(t *testing.T) {
	s := NewStore()
	s.Replace(map[string]string{
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
	s.Replace(map[string]string{"a": "1"})
	if s.Num("a") != 1 {
		t.Fatal("first replace failed")
	}
	s.Replace(map[string]string{"b": "2"})
	if s.Num("a") != 0 {
		t.Error("old key still present after replace")
	}
	if s.Num("b") != 2 {
		t.Error("new key missing after replace")
	}
}
