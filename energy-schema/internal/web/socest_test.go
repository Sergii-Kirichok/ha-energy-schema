package web

import (
	"math"
	"testing"
	"time"
)

func TestSocEst(t *testing.T) {
	var e socEst
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if f := e.step(82, -10, 315, t0); f != 82 {
		t.Fatalf("first sample = BMS value, got %v", f)
	}
	// заряд 31,5 А 6 мин = 3,15 А·ч = 1 % от 315 А·ч, но окно BMS ±0,5 %
	f := e.step(82, -31.5, 315, t0.Add(3*time.Minute))
	if math.Abs(f-82.5) > 1e-9 {
		t.Errorf("3 min × 31.5 A = +0.5 %%, got %v", f)
	}
	if f = e.step(82, -31.5, 315, t0.Add(6*time.Minute)); f != 82.5 {
		t.Errorf("clamped to BMS+0.5, got %v", f)
	}
	if f = e.step(83, 31.5, 315, t0.Add(7*time.Minute)); f != 82.5 {
		t.Errorf("BMS says 83 → window 82.5..83.5, 82.33 lifted to 82.5, got %v", f)
	}
	if f = e.step(70, 0, 315, t0.Add(3*time.Hour)); f != 70 {
		t.Errorf("long gap → resync to BMS, got %v", f)
	}
}
