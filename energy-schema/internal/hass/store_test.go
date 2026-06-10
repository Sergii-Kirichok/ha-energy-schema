package hass

import (
	"testing"
	"time"
)

func TestStoreAttrs(t *testing.T) {
	s := NewStore()
	s.Replace(map[string]Entity{
		"weather.x": {State: "partlycloudy", Attrs: map[string]string{"temperature": "17.7", "cloud_coverage": "13"}},
		"sun.sun":   {State: "above_horizon", Attrs: map[string]string{"next_setting": time.Now().Add(3 * time.Hour).Format(time.RFC3339Nano)}},
	})
	if got := s.Attr("weather.x", "temperature"); got != "17.7" {
		t.Errorf("Attr = %q", got)
	}
	if got := s.AttrNum("weather.x", "cloud_coverage"); got != 13 {
		t.Errorf("AttrNum = %v", got)
	}
	if h := s.HoursUntil("sun.sun", "next_setting"); h < 2.9 || h > 3.1 {
		t.Errorf("HoursUntil = %v, want ~3", h)
	}
	if s.HoursUntil("weather.x", "nope") != 0 {
		t.Error("HoursUntil for missing attr should be 0")
	}
	if s.Attr("weather.x", "nope") != "" {
		t.Error("missing attr should be empty")
	}
}

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

// rollMax must report the peak within the trailing 24h and forget older values.
func TestRollMax24h(t *testing.T) {
	r := &rollMax{}
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r.add(base.Add(-30*time.Hour), 95) // 30h ago — must fall out of the window
	r.add(base.Add(-10*time.Hour), 60)
	r.add(base.Add(-2*time.Hour), 72)
	if got := r.max(base); got != 72 {
		t.Errorf("max over last 24h = %.0f, want 72 (the 95 is 30h old)", got)
	}
	r.add(base, 80) // a fresh higher value dominates
	if got := r.max(base); got != 80 {
		t.Errorf("max = %.0f, want 80", got)
	}
}

// rollMax also tracks the trough; values older than 24h drop out of min too.
func TestRollMin24h(t *testing.T) {
	r := &rollMax{}
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r.add(base.Add(-30*time.Hour), 5) // 30h ago — must fall out
	r.add(base.Add(-3*time.Hour), 40)
	r.add(base.Add(-1*time.Hour), 25)
	if m, ok := r.min(base); !ok || m != 25 {
		t.Errorf("min over 24h = %.0f ok=%v, want 25 (the 5 is 30h old)", m, ok)
	}
	empty := &rollMax{}
	if _, ok := empty.min(base); ok {
		t.Error("min on empty window should report ok=false")
	}
}

// rollMax averages time-weighted over the window (each 5-min bucket equal).
func TestRollAvg24h(t *testing.T) {
	r := &rollMax{}
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r.add(base.Add(-30*time.Hour), 100) // 30h ago — must drop out of the average
	r.add(base.Add(-2*time.Hour), 2)    // bucket A
	r.add(base.Add(-1*time.Hour), 4)    // bucket B
	if a, ok := r.avg(base); !ok || a < 2.9 || a > 3.1 {
		t.Errorf("avg over 24h = %.2f ok=%v, want ~3 (mean of 2 and 4)", a, ok)
	}
	if _, ok := (&rollMax{}).avg(base); ok {
		t.Error("avg on empty window should report ok=false")
	}
}

// Within one 5-minute bucket the peak is kept, not the latest value.
func TestRollMaxBucketPeak(t *testing.T) {
	r := &rollMax{}
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r.add(base, 50)
	r.add(base.Add(1*time.Minute), 70)
	r.add(base.Add(2*time.Minute), 40)
	if got := r.max(base.Add(2 * time.Minute)); got != 70 {
		t.Errorf("bucket peak = %.0f, want 70", got)
	}
}

// Reconnect countdown: ticks down, restarts on a failed attempt, resets on outage.
func TestReconnectCountdownAndRetry(t *testing.T) {
	s := NewStore()
	t0 := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	// сеть вернулась, инвертор ещё не подключён → отсчёт пошёл с 70, попытка 1
	s.updateReconnectAt(t0, true, true, 70)
	rem, total, active, att := s.reconnectInfoAt(t0)
	if !active || total != 70 || rem < 69.9 || att != 1 {
		t.Fatalf("start: rem=%.1f total=%.0f active=%v att=%d", rem, total, active, att)
	}
	if rem, _, _, _ = s.reconnectInfoAt(t0.Add(30 * time.Second)); rem < 39.5 || rem > 40.5 {
		t.Errorf("after 30s rem=%.1f, want ~40", rem)
	}
	// подключился (bonded) → не активен
	s.updateReconnectAt(t0.Add(40*time.Second), false, true, 70)
	if _, _, a, _ := s.reconnectInfoAt(t0.Add(41 * time.Second)); a {
		t.Error("should be inactive once bonded")
	}
	// сорвался через 2 c → снова ожидание → отсчёт сначала, попытка 2
	s.updateReconnectAt(t0.Add(42*time.Second), true, true, 70)
	if rem, _, active, att = s.reconnectInfoAt(t0.Add(42 * time.Second)); !active || rem < 69.9 || att != 2 {
		t.Errorf("retry: rem=%.1f active=%v att=%d, want ~70/true/2", rem, active, att)
	}
	// сеть пропала полностью → не активен, счётчик попыток сброшен
	s.updateReconnectAt(t0.Add(50*time.Second), false, false, 70)
	if _, _, a, att := s.reconnectInfoAt(t0.Add(50 * time.Second)); a || att != 0 {
		t.Errorf("grid gone: active=%v att=%d, want false/0", a, att)
	}
}

// Store.Max24h tracks numeric entities fed through Replace; PV stats round-trip.
func TestStoreMax24hAndPVStats(t *testing.T) {
	s := NewStore()
	s.ReplaceStates(map[string]string{"sensor.x": "40"})
	s.ReplaceStates(map[string]string{"sensor.x": "65"})
	s.ReplaceStates(map[string]string{"sensor.x": "55"})
	if got := s.Max24h("sensor.x"); got != 65 {
		t.Errorf("Max24h = %.0f, want 65", got)
	}
	if got := s.Max24h("sensor.unknown"); got != 0 {
		t.Errorf("unknown Max24h = %.0f, want 0", got)
	}
	s.SetPVStats(49, 41, 2)
	if s.PVClearDayKWh() != 49 {
		t.Errorf("PVClearDayKWh = %.0f, want 49", s.PVClearDayKWh())
	}
	if avg, n := s.PVRecent(); avg != 41 || n != 2 {
		t.Errorf("PVRecent = %.0f,%d want 41,2", avg, n)
	}
}
