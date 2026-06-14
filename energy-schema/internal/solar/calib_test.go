package solar

import (
	"math"
	"testing"
	"time"
)

// Холодный старт: без истории поправка ≈ 1, прогноз = сырой.
func TestCalibColdStart(t *testing.T) {
	c := NewCalibrator(t.TempDir()+"/c.json", "g")
	if got := c.Apply("om", 5.0, 6.0); math.Abs(got-5.0) > 0.01 {
		t.Fatalf("холодный старт должен вернуть ~raw (5.0), получили %.2f", got)
	}
}

// Сходимость: если в корзине погоды прогноз стабильно 10, а факт 7 — после
// обучения Apply для этой корзины тянется к ~7.
func TestCalibConvergence(t *testing.T) {
	c := NewCalibrator(t.TempDir()+"/c.json", "g")
	loc := time.UTC
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, loc) // полдень, час с генерацией
	for day := 0; day < 25; day++ {
		h := base.AddDate(0, 0, day)
		c.Freeze(h.Unix(), 10.0, 11.0, "om") // raw 10, потолок 11 → kp≈0.91
		c.ObserveDue(h.Add(70*time.Minute),
			func(int64) (float64, int) { return 7.0, 12 },
			func(int64) (float64, bool) { return 50.0, true })
	}
	got := c.Apply("om", 10.0, 11.0)
	if got < 6.5 || got > 8.5 {
		t.Fatalf("после обучения ожидали ~7, получили %.2f", got)
	}
}

// Гейт полной батареи: часы с SOC≥98 не учим (PV могла резаться).
func TestCalibFullBatteryGate(t *testing.T) {
	c := NewCalibrator(t.TempDir()+"/c.json", "g")
	loc := time.UTC
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, loc)
	for day := 0; day < 25; day++ {
		h := base.AddDate(0, 0, day)
		c.Freeze(h.Unix(), 10.0, 11.0, "om")
		c.ObserveDue(h.Add(70*time.Minute),
			func(int64) (float64, int) { return 3.0, 12 }, // факт занижен (батарея резала)
			func(int64) (float64, bool) { return 99.0, true })
	}
	if got := c.Apply("om", 10.0, 11.0); got < 9.0 {
		t.Fatalf("часы с полной батареей не должны обучать модель, но поправка сработала: %.2f", got)
	}
}
