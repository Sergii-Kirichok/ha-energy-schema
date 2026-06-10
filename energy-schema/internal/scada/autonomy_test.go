package scada

import (
	"testing"
	"time"

	"energy-schema/internal/hass"
)

// nightStore: сейчас ночь, восход через 6ч, закат завтра через 22ч (день 16ч).
func nightStore(t *testing.T, tomorrowCloud float64) *hass.Store {
	t.Helper()
	now := time.Now()
	s := hass.NewStore()
	s.Replace(map[string]hass.Entity{
		"sun.sun": {State: "below_horizon", Attrs: map[string]string{
			"next_rising":  now.Add(6 * time.Hour).Format(time.RFC3339Nano),
			"next_setting": now.Add(22 * time.Hour).Format(time.RFC3339Nano),
		}},
	})
	s.SetForecast([]hass.ForecastDay{
		{Time: now, Cloud: 50},
		{Time: now.AddDate(0, 0, 1), Cloud: tomorrowCloud},
	})
	return s
}

// Ночь, запаса хватает до восхода, завтра ясно (100 кВт·ч > 48 кВт·ч расхода)
// → батарея дозарядится, автономия перекрывает горизонт прогноза (48ч+).
func TestAutonomyBridgesNightWithClearTomorrow(t *testing.T) {
	st := nightStore(t, 0)
	h, note := simulateAutonomy(st, 20, 50, 2, 0, 100)
	if h < 48 {
		t.Errorf("expected 48h+ (bridge to tomorrow's generation), got %.1f (%s)", h, note)
	}
}

// Ночь, запаса 8 кВт·ч при 2 кВт — сядем через ~4ч, до восхода (6ч) не дотянем.
func TestAutonomyDiesBeforeSunrise(t *testing.T) {
	st := nightStore(t, 0)
	h, note := simulateAutonomy(st, 8, 50, 2, 0, 100)
	if h < 3.5 || h > 4.5 {
		t.Errorf("expected ~4h, got %.1f (%s)", h, note)
	}
}

// Ночь, до восхода дотягиваем, но завтра сплошная облачность (генерация ~30 кВт·ч
// при расходе 48/сут) — день продержимся на PV+остатке, но к концу суток сядем.
func TestAutonomyCloudyTomorrowRunsOut(t *testing.T) {
	st := nightStore(t, 100)
	h, _ := simulateAutonomy(st, 20, 50, 2, 0, 100)
	if h >= 48 {
		t.Errorf("expected finite autonomy with overcast tomorrow, got %.1f", h)
	}
	if h < 6 {
		t.Errorf("should at least survive the night (6h), got %.1f", h)
	}
}

// Нет данных солнца — простая оценка usable/load.
func TestAutonomyNoSunData(t *testing.T) {
	s := hass.NewStore()
	h, _ := simulateAutonomy(s, 30, 50, 2, 0, 100)
	if h != 15 {
		t.Errorf("fallback = %.1f, want 15", h)
	}
}
