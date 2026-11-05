package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var rampBase map[string]string

// нижний порог 180 В, запас 10, гистерезис 10: <185 сильно, <190 низко, 190–200 держим, ≥200 — вверх
func rampSetup(t *testing.T, states map[string]string) (*Server, *chargeState) {
	t.Helper()
	prevC, prevS := chargeFile, rampStatsFile
	chargeFile, rampStatsFile = filepath.Join(t.TempDir(), "charge.json"), filepath.Join(t.TempDir(), "ramp.jsonl")
	t.Cleanup(func() { chargeFile, rampStatsFile = prevC, prevS })
	rampBase = map[string]string{gridChargeSwitch: "on", gridUpEntity: "off",
		gridVoltEntity(1): "220", gridVoltEntity(2): "220", gridVoltEntity(3): "220"}
	for k, v := range states {
		rampBase[k] = v
	}
	s, _ := tickSetup(t, rampBase, map[int]int{gridVHighReg: 2530, gridVHighReg + 1: 1800})
	st := recentState()
	return s, &st
}

func setStates(s *Server, kv map[string]string) {
	for k, v := range kv {
		rampBase[k] = v
	}
	s.store.ReplaceStates(tickStates(rampBase))
}

// step — тик после паузы шага.
func step(s *Server, st *chargeState, grid float64) float64 {
	st.RampAt = time.Now().Add(-2 * time.Minute)
	return s.rampTick(st, grid)
}

func TestRampPresetsWhileGridDownThenSteps(t *testing.T) {
	s, st := rampSetup(t, nil)
	if g := s.rampTick(st, 15); g != 2 || st.RampVLow != 180 {
		t.Fatalf("grid down: g=%v low=%v, want 2 A preset, 180 V", g, st.RampVLow)
	}
	setStates(s, map[string]string{gridUpEntity: "on"})
	if g := s.rampTick(st, 15); g != 2 {
		t.Fatalf("just reconnected: g=%v, want hold 2 A until the step pause", g)
	}
	if g := step(s, st, 15); g != 4 {
		t.Fatalf("step up: g=%v, want 4", g)
	}
	setStates(s, map[string]string{gridVoltEntity(2): "195"}) // полоса гистерезиса — держим
	if g := step(s, st, 15); g != 4 {
		t.Fatalf("hold: g=%v, want 4", g)
	}
	setStates(s, map[string]string{gridVoltEntity(2): "188"}) // близко к низу — шаг вниз
	if g := step(s, st, 15); g != 2 {
		t.Fatalf("low: g=%v, want 2", g)
	}
	b, _ := os.ReadFile(rampStatsFile)
	if !strings.Contains(string(b), `"ev":"grid_down"`) || !strings.Contains(string(b), `"ev":"step"`) {
		t.Errorf("stats must log grid_down and steps:\n%s", b)
	}
}

func TestRampSevereSagDropsToMinAtOnce(t *testing.T) {
	s, st := rampSetup(t, map[string]string{gridUpEntity: "on"})
	st.RampA, st.RampAt = 14, time.Now() // пауза шага не прошла — всё равно сразу
	setStates(s, map[string]string{gridVoltEntity(3): "183"})
	if g := s.rampTick(st, 15); g != 2 {
		t.Fatalf("severe: g=%v, want minimum 2 A immediately", g)
	}
}

func TestRampNeverAboveSlider(t *testing.T) {
	s, st := rampSetup(t, map[string]string{gridUpEntity: "on", gridVoltEntity(1): "250"})
	st.RampA = 10
	if g := step(s, st, 10); g != 10 {
		t.Fatalf("high phase: g=%v, want slider 10 as the ceiling", g)
	}
	st.RampAt = time.Now()
	if g := s.rampTick(st, 6); g != 6 { // ползунок уменьшили — сразу
		t.Fatalf("slider lowered: g=%v, want 6 at once", g)
	}
}

func TestRampOffWithoutGridCharge(t *testing.T) {
	s, st := rampSetup(t, map[string]string{gridChargeSwitch: "off"})
	if g := s.rampTick(st, 15); g != 15 || st.rampActive() {
		t.Fatalf("grid charge off: g=%v, want untouched 15", g)
	}
}
