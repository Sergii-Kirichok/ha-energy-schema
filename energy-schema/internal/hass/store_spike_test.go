package hass

import "testing"

// Одиночный выброс мощности не попадает в пики; подтверждённый следующим
// опросом — попадает; не-мощности и плавный рост — как раньше.
func TestStoreSpikeGuard(t *testing.T) {
	s := NewStore()
	feed := func(v string) { s.ReplaceStates(map[string]string{"sensor.x_load_power": v}) }
	feed("1200")
	feed("25300") // выброс
	feed("1300")  // не подтвердил
	if m := s.Max24h("sensor.x_load_power"); m != 1300 {
		t.Fatalf("spike leaked into the 24 h peak: %v", m)
	}
	if m := s.DayMax("sensor.x_load_power"); m != 1300 {
		t.Fatalf("spike leaked into the day peak: %v", m)
	}
	feed("24000") // скачок
	feed("23000") // подтвердил
	if m := s.Max24h("sensor.x_load_power"); m != 24000 {
		t.Errorf("confirmed jump must count: %v", m)
	}
	var g spikeGuard
	g.peaks("sensor.cell_voltage", 1)
	if out := g.peaks("sensor.cell_voltage", 50000); len(out) != 1 {
		t.Errorf("only *_power is guarded: %v", out)
	}
}
