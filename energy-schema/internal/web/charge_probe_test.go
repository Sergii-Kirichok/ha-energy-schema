package web

import (
	"path/filepath"
	"testing"
	"time"
)

func probeSetup(t *testing.T, states map[string]string) (*Server, *haFake, *chargeState) {
	t.Helper()
	prev := chargeFile
	chargeFile = filepath.Join(t.TempDir(), "charge.json")
	t.Cleanup(func() { chargeFile = prev })
	base := map[string]string{chargeProbeEntity: "on", chargeAutoHelper: "on"}
	for k, v := range states {
		base[k] = v
	}
	s, f := tickSetup(t, base, nil)
	st := recentState()
	return s, f, &st
}

func TestProbeStartsHighThenLow(t *testing.T) {
	s, _, st := probeSetup(t, nil)
	a, m, ok := s.probeTick(st, 70, 24, true)
	if !ok || m != "probe/high" || a != 10 { // ток шага 10 А (дефолт)
		t.Fatalf("start: a=%v m=%q ok=%v", a, m, ok)
	}
	if st.ProbePhase != probeHigh || st.ProbeCycle != 0 {
		t.Fatalf("state=%+v", st)
	}
	if a, m, _ := s.probeTick(st, 70, 24, true); m != "probe/high" || a != 10 {
		t.Fatalf("still high before timer: a=%v m=%q", a, m) // 3 мин не прошло
	}
	st.ProbeAt = time.Now().Add(-4 * time.Minute) // промотали время шага
	if a, m, _ := s.probeTick(st, 70, 24, true); st.ProbePhase != probeLow || m != "probe/low" || a != 2 {
		t.Fatalf("to low: phase=%q a=%v m=%q", st.ProbePhase, a, m) // пауза 2 А
	}
}

func TestProbeCountsCycleAndFinishes(t *testing.T) {
	s, f, st := probeSetup(t, map[string]string{probeCyclesEntity: "2"})
	st.ProbePhase, st.ProbeCycle = probeLow, 0
	st.ProbeAt = time.Now().Add(-6 * time.Minute) // конец паузы (5 мин)
	s.probeTick(st, 70, 24, true)
	if st.ProbeCycle != 1 || st.ProbePhase != probeHigh {
		t.Fatalf("cycle1: %+v", st)
	}
	st.ProbePhase, st.ProbeAt = probeLow, time.Now().Add(-6*time.Minute) // второй цикл добивает до 2
	if _, _, ok := s.probeTick(st, 70, 24, true); ok || st.ProbePhase != "" {
		t.Fatalf("finish: ok=%v phase=%q", ok, st.ProbePhase)
	}
	if len(f.find("/services/input_boolean/turn_off")) == 0 {
		t.Error("finished run must turn the probe helper off")
	}
	if st.ProbeNote == "" {
		t.Error("finished run must leave a note")
	}
}

func TestProbeStopsAtTop(t *testing.T) {
	s, _, st := probeSetup(t, nil)
	st.ProbePhase, st.ProbeAt = probeHigh, time.Now()
	if _, _, ok := s.probeTick(st, 100, 24, true); ok || st.ProbePhase != "" { // SOC 100 — стоп даже на шаге
		t.Fatalf("soc100 must stop: ok=%v phase=%q", ok, st.ProbePhase)
	}
}

func TestProbePausesWhenAutoOff(t *testing.T) {
	s, _, st := probeSetup(t, nil)
	if _, _, ok := s.probeTick(st, 70, 24, false); ok { // авто выключен → прогон на паузе
		t.Error("auto off → probe must pause (ok=false)")
	}
}

func TestProbeCapsStepToGeneralLimit(t *testing.T) {
	s, _, st := probeSetup(t, map[string]string{probeHighAEntity: "40"})
	if a, _, _ := s.probeTick(st, 70, 15, true); a != 15 { // ток шага не выше общего лимита
		t.Errorf("step a=%v, want capped to 15", a)
	}
}
