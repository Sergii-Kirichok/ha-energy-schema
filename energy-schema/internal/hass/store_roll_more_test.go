package hass

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRollDTORoundTrip(t *testing.T) {
	r := &rollMax{}
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r.add(base, 3)
	r.add(base.Add(time.Minute), -4)
	r.add(base.Add(time.Hour), 9)
	var back rollMax
	r.toDTO().into(&back)
	if back != *r {
		t.Error("toDTO→into must reproduce the buffer exactly")
	}
}

// A truncated/old-format DTO must not panic; Stamp bounds the copy.
func TestRollDTOIntoShortSlices(t *testing.T) {
	var r rollMax
	rollDTO{Hi: []float64{1, 2, 3}, Lo: nil, Stamp: []int64{5}}.into(&r)
	if r.stamp[0] != 5 || r.hi[0] != 1 || r.hi[1] != 0 || r.lo[0] != 0 || r.stamp[1] != 0 {
		t.Errorf("short DTO: stamp0=%d hi0=%v hi1=%v", r.stamp[0], r.hi[0], r.hi[1])
	}
	rollDTO{}.into(&r) // empty — no-op
}

func TestSaveLoadRoll(t *testing.T) {
	now := time.Now()
	s := NewStore()
	s.SeedRoll("sensor.p", now.Add(-2*time.Hour), 10)
	s.SeedRoll("sensor.p", now.Add(-2*time.Hour+time.Second), 30) // same bucket → avg 20
	s.SeedRoll("sensor.p", now.Add(-time.Hour), -5)
	s.SeedRoll("sensor.other", now.Add(-time.Hour), 1) // not in the save list
	path := filepath.Join(t.TempDir(), "roll.json")
	if err := s.SaveRoll(path, []string{"sensor.p", "sensor.missing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp left behind: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string][]float64
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["sensor.p"] == nil {
		t.Fatalf("saved keys = %v, want only sensor.p", keys(m))
	}
	for _, k := range []string{"hi", "lo", "sum", "cnt", "st"} {
		if len(m["sensor.p"][k]) != 288 {
			t.Errorf("field %q len=%d, want 288", k, len(m["sensor.p"][k]))
		}
	}

	s2 := NewStore()
	if err := s2.LoadRoll(path); err != nil {
		t.Fatal(err)
	}
	if v := s2.Max24h("sensor.p"); v != 30 {
		t.Errorf("restored Max24h = %v, want 30", v)
	}
	if v, ok := s2.Min24h("sensor.p"); !ok || v != -5 {
		t.Errorf("restored Min24h = %v %v, want -5", v, ok)
	}
	if v, ok := s2.Avg24h("sensor.p"); !ok || !approx(v, 7.5) { // (20 + -5) / 2
		t.Errorf("restored Avg24h = %v %v, want 7.5", v, ok)
	}
	if _, ok := s2.Min24h("sensor.other"); ok {
		t.Error("unsaved entity must not be restored")
	}
}

func keys(m map[string]map[string][]float64) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestLoadSaveRollErrors(t *testing.T) {
	dir := t.TempDir()
	s := NewStore()
	if err := s.LoadRoll(filepath.Join(dir, "absent.json")); !os.IsNotExist(err) {
		t.Errorf("missing file err = %v, want not-exist", err)
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{not json"), 0644)
	if err := s.LoadRoll(bad); err == nil {
		t.Error("corrupt file must error")
	}
	if err := s.SaveRoll(filepath.Join(dir, "no", "such", "dir.json"), nil); err == nil {
		t.Error("SaveRoll into missing dir must error")
	}
	empty := filepath.Join(dir, "empty.json")
	if err := s.SaveRoll(empty, []string{"sensor.x"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(empty); string(b) != "{}" {
		t.Errorf("empty save = %q, want {}", b)
	}
}

func TestRollWindows12h24h(t *testing.T) {
	now := time.Now()
	s := NewStore()
	s.SeedRoll("e", now.Add(-20*time.Hour), 100)
	s.SeedRoll("e", now.Add(-20*time.Hour), -50)
	s.SeedRoll("e", now.Add(-time.Hour), 5)
	if v := s.Max12h("e"); v != 5 {
		t.Errorf("Max12h = %v, want 5 (100 is 20h old)", v)
	}
	if v := s.Max24h("e"); v != 100 {
		t.Errorf("Max24h = %v, want 100", v)
	}
	if v, ok := s.Min12h("e"); !ok || v != 5 {
		t.Errorf("Min12h = %v %v, want 5", v, ok)
	}
	if v, ok := s.Min24h("e"); !ok || v != -50 {
		t.Errorf("Min24h = %v %v, want -50", v, ok)
	}
	if v, ok := s.Avg24h("e"); !ok || !approx(v, 15) { // (25 + 5) / 2
		t.Errorf("Avg24h = %v %v, want 15", v, ok)
	}
	// unknown entity / only out-of-window data
	s.SeedRoll("old", now.Add(-25*time.Hour), 7)
	for _, e := range []string{"none", "old"} {
		if s.Max12h(e) != 0 || s.Max24h(e) != 0 {
			t.Errorf("%s: max must be 0", e)
		}
		if _, ok := s.Min12h(e); ok {
			t.Errorf("%s: Min12h ok", e)
		}
		if _, ok := s.Min24h(e); ok {
			t.Errorf("%s: Min24h ok", e)
		}
		if _, ok := s.Avg24h(e); ok {
			t.Errorf("%s: Avg24h ok", e)
		}
	}
}

func TestReconnectPublic(t *testing.T) {
	s := NewStore()
	if rem, total, active, att := s.ReconnectInfo(); rem != 0 || total != 0 || active || att != 0 {
		t.Errorf("fresh = %v %v %v %d", rem, total, active, att)
	}
	s.UpdateReconnect(true, true, 60)
	s.UpdateReconnect(true, true, 60) // still waiting — same attempt
	rem, total, active, att := s.ReconnectInfo()
	if !active || total != 60 || rem <= 59 || rem > 60 || att != 1 {
		t.Errorf("waiting = rem %v total %v active %v att %d", rem, total, active, att)
	}
	s.UpdateReconnect(false, true, 90)
	if rem, total, active, att := s.ReconnectInfo(); active || rem != 0 || total != 90 || att != 1 {
		t.Errorf("bonded = rem %v total %v active %v att %d", rem, total, active, att)
	}
	// countdown never goes negative
	s.updateReconnectAt(time.Now().Add(-5*time.Minute), true, true, 60)
	if rem, _, active, att := s.ReconnectInfo(); !active || rem != 0 || att != 2 {
		t.Errorf("expired = rem %v active %v att %d, want 0/true/2", rem, active, att)
	}
}

func TestLastInt(t *testing.T) {
	s := NewStore()
	if s.LastInt("sensor.x") != 0 {
		t.Error("never seen must be 0")
	}
	for in, want := range map[string]int{"2.5": 3, "-2.5": -3, "2.49": 2, " 7 ": 7, "abc": 0} {
		s.ReplaceStates(map[string]string{"sensor.x": in})
		if got := s.LastInt("sensor.x"); got != want {
			t.Errorf("LastInt(%q) = %d, want %d", in, got, want)
		}
	}
	s.ReplaceStates(map[string]string{"sensor.x": "41.6"})
	s.ReplaceStates(map[string]string{"sensor.x": "unavailable"})
	if got := s.LastInt("sensor.x"); got != 42 {
		t.Errorf("LastInt after dropout = %d, want 42", got)
	}
}
