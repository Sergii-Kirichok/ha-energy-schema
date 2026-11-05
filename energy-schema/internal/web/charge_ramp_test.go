package web

import (
	"path/filepath"
	"testing"
	"time"
)

var rampBase map[string]string

func rampSetup(t *testing.T, states map[string]string) (*Server, *chargeState) {
	t.Helper()
	prev := chargeFile
	chargeFile = filepath.Join(t.TempDir(), "charge.json")
	t.Cleanup(func() { chargeFile = prev })
	rampBase = map[string]string{gridChargeSwitch: "on", gridUpEntity: "off",
		gridVoltEntity(1): "230", gridVoltEntity(2): "230", gridVoltEntity(3): "230"}
	for k, v := range states {
		rampBase[k] = v
	}
	s, _ := tickSetup(t, rampBase, map[int]int{gridVLowReg: 1800}) // нижний порог 180 В
	st := recentState()
	return s, &st
}

func setStates(s *Server, kv map[string]string) {
	for k, v := range kv {
		rampBase[k] = v
	}
	s.store.ReplaceStates(tickStates(rampBase))
}

func TestRampPresetsWhileGridDownThenSteps(t *testing.T) {
	s, st := rampSetup(t, nil)
	if g := s.rampTick(st, 15); g != 2 || st.RampVLow != 180 {
		t.Fatalf("grid down: g=%v vlow=%v, want 2 A preset, 180 V", g, st.RampVLow)
	}
	setStates(s, map[string]string{gridUpEntity: "on"})
	if g := s.rampTick(st, 15); g != 2 {
		t.Fatalf("just reconnected: g=%v, want hold 2 A until the step pause", g)
	}
	st.RampAt = time.Now().Add(-2 * time.Minute)
	if g := s.rampTick(st, 15); g != 4 { // 230 В ≥ 180+10 — прибавка
		t.Fatalf("step up: g=%v, want 4", g)
	}
	setStates(s, map[string]string{gridVoltEntity(2): "184"}) // 184 < 180+5 — шаг назад
	st.RampAt = time.Now().Add(-2 * time.Minute)
	if g := s.rampTick(st, 15); g != 2 {
		t.Fatalf("sag: g=%v, want back to 2", g)
	}
	setStates(s, map[string]string{gridVoltEntity(2): "187"}) // между запасом и половиной — держим
	st.RampAt = time.Now().Add(-2 * time.Minute)
	if g := s.rampTick(st, 15); g != 2 {
		t.Fatalf("hold: g=%v, want 2", g)
	}
}

func TestRampFinishesAtGridLimit(t *testing.T) {
	s, st := rampSetup(t, map[string]string{gridUpEntity: "on"})
	st.RampA, st.RampVLow, st.RampAt = 4, 180, time.Now().Add(-2*time.Minute)
	if g := s.rampTick(st, 5); g != 5 || st.rampActive() {
		t.Fatalf("done: g=%v active=%v, want full 5 A and ramp over", g, st.rampActive())
	}
}

func TestRampOffWithoutGridCharge(t *testing.T) {
	s, st := rampSetup(t, map[string]string{gridChargeSwitch: "off"})
	if g := s.rampTick(st, 15); g != 15 || st.rampActive() {
		t.Fatalf("grid charge off: g=%v, want untouched 15", g)
	}
}
