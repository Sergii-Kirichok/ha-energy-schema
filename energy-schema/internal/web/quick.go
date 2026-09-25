package web

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"energy-schema/internal/scada"
)

// quickParam выполняет действие с оборота карточки АКБ: val = "<key>:<±delta>"
// для чисел или "<key>:toggle" для переключателей. Границы и шаг — из
// scada.QuickParams; шаг клиента не доверяем, |delta| ≤ 4×Step.
func (s *Server) quickParam(val string) (string, error) {
	key, op, ok := strings.Cut(val, ":")
	q := scada.QuickParamByKey(key)
	if !ok || q == nil {
		return "", fmt.Errorf("неизвестный параметр %q", key)
	}
	if !s.store.Available(q.Entity) {
		return "", fmt.Errorf("%s ещё не создан в HA", q.Entity)
	}
	if q.Bool {
		if op != "toggle" {
			return "", fmt.Errorf("для %s допустимо только toggle", key)
		}
		if err := s.client.CallService("input_boolean", "toggle", map[string]any{"entity_id": q.Entity}); err != nil {
			return "", err
		}
		return key + " toggle", nil
	}
	d, err := strconv.ParseFloat(op, 64)
	if err != nil || math.Abs(d) > q.Step*4 || d == 0 {
		return "", fmt.Errorf("недопустимый шаг %q для %s", op, key)
	}
	nv := math.Round(math.Min(q.Max, math.Max(q.Min, s.store.Num(q.Entity)+d)))
	if err := s.client.CallService("input_number", "set_value", map[string]any{"entity_id": q.Entity, "value": nv}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s -> %g", key, nv), nil
}
