package web

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"energy-schema/internal/scada"
)

// quickSave — «Сохранить» с оборота карточки АКБ: val = "max_a=4,auto=1,…".
// Всё проверяется до записи; числа ставятся в хелперы и сразу применяются
// (как «Применить» на дашборде), тумблеры — turn_on/turn_off.
func (s *Server) quickSave(val string) (string, error) {
	type set struct {
		q *scada.QuickParam
		v float64
	}
	var sets []set
	for _, kv := range strings.Split(val, ",") {
		k, vs, ok := strings.Cut(kv, "=")
		q := scada.QuickParamByKey(k)
		v, err := strconv.ParseFloat(vs, 64)
		if !ok || q == nil || err != nil || math.IsNaN(v) {
			return "", fmt.Errorf("неверное значение %q", kv)
		}
		if q.Bool && v != 0 && v != 1 || !q.Bool && (v < q.Min || v > q.Max) {
			return "", fmt.Errorf("%s: %g вне диапазона", k, v)
		}
		sets = append(sets, set{q, v})
	}
	nums := map[string]float64{}
	for _, x := range sets {
		var err error
		if x.q.Bool {
			svc := map[bool]string{true: "turn_on", false: "turn_off"}[x.v == 1]
			err = s.client.CallService("input_boolean", svc, map[string]any{"entity_id": x.q.Entity})
		} else {
			err = s.client.CallService("input_number", "set_value", map[string]any{"entity_id": x.q.Entity, "value": x.v})
			nums[x.q.Entity] = x.v
		}
		if err != nil {
			return "", err
		}
	}
	// применяем сразу, не дожидаясь, пока опрос HA принесёт новые значения хелперов
	draft := func(e string) float64 {
		if v, ok := nums[e]; ok {
			return v
		}
		return s.store.Num(e)
	}
	return fmt.Sprintf("saved %s, applied %v", val, s.applied.apply(draft)), nil
}
