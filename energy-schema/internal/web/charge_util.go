package web

import (
	"fmt"
	"math"
	"time"

	"energy-schema/internal/config"
)

// regValue — значение регистра для желаемого фактического тока: делим на
// множитель, округляем, не ниже 1 (0 у Deye = «не заряжать», не хотим).
func regValue(amps, factor float64) float64 {
	if factor < 1 {
		factor = 1
	}
	if amps <= 0 {
		return 0 // «стоп заряда»
	}
	return math.Max(1, math.Round(amps/factor))
}

// zeroVerdict — итог самопроверки «108 = 0 останавливает заряд»: ждём 2 мин
// (инвертор применяет не сразу) и только при testable (SOC<99, излишек PV);
// дальше заряд > 3 А → не работает, ≤ 1 А → ок.
func zeroVerdict(since time.Duration, chargingA float64, testable bool) string {
	switch {
	case since < 2*time.Minute || !testable:
		return "wait"
	case chargingA > 3:
		return "broken"
	case chargingA <= 1:
		return "ok"
	}
	return "wait"
}

// publishLimit — сенсор «Текущее ограничение тока заряда»: ФАКТИЧЕСКОЕ значение
// регистра 108 (прочитанное/записанное) × множитель каналов; расчёт регулятора
// и режим — в атрибутах.
func (s *Server) publishLimit(factor, reg108, target float64, mode string, soc float64, fullDue, auto bool) {
	if factor < 1 {
		factor = 1
	}
	attrs := map[string]any{"friendly_name": "Текущее ограничение тока заряда", "unit_of_measurement": "A",
		"state_class": "measurement", "icon": "mdi:current-dc", "mode": mode, "target_a": target,
		"reg108": reg108, "channels": factor, "soc": soc, "full_due": fullDue, "auto": auto}
	_ = s.client.SetState("sensor.energy_schema_charge_setpoint", fmt.Sprintf("%.0f", reg108*factor), attrs)
}

// nightHyst — ночь, если PV ниже NightBelowW; снова день, когда PV выше
// DayAboveW (разрыв — гистерезис против вечерних облаков).
func nightHyst(pvW float64, wasNight bool, t config.ChargeTuning) bool {
	if wasNight {
		return pvW <= t.DayAboveW
	}
	return pvW < t.NightBelowW
}

// pvBucket — грубая корзина генерации для отпечатка входов регулятора:
// смена корзины = событие (переход день/ночь), дрожание внутри — нет.
func pvBucket(pvW float64, t config.ChargeTuning) string {
	switch {
	case pvW < t.NightBelowW:
		return "n"
	case pvW > t.DayAboveW:
		return "d"
	}
	return "m"
}
