package web

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Применённые настройки регулятора. Хелперы HA (ползунки «Заряд АКБ») —
// черновик: регулятор берёт числа только отсюда, а сюда они попадают по
// «Применить» (input_button на дашборде) или «Сохранить» на обороте карточки.
// Так случайно задетый ползунок не уходит в инвертор. Тумблеры (авто, полный
// заряд) — сразу, это осознанные нажатия.
const (
	appliedFile   = "/data/charge_applied.json"
	chargeApplyBt = "input_button.energy_schema_charge_apply"
	chargePending = "sensor.energy_schema_charge_pending"
)

// chargeNumbers — числовые хелперы регулятора и их подписи.
var chargeNumbers = []struct{ entity, label string }{
	{"input_number.energy_schema_charge_max_a", "Общий лимит"},
	{"input_number.energy_schema_charge_grid_a", "Лимит от сети"},
	{"input_number.energy_schema_charge_target_soc", "Цель на ночь"},
	{"input_number.energy_schema_charge_full_days", "Полный раз в"},
}

type chargeApplied struct {
	mu   sync.Mutex
	path string
	V    map[string]float64 `json:"values"`
	At   time.Time          `json:"at"`
}

func loadApplied(path string) *chargeApplied {
	a := &chargeApplied{path: path, V: map[string]float64{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, a)
	}
	if a.V == nil {
		a.V = map[string]float64{}
	}
	return a
}

// num — применённое значение; пока его нет (первый запуск) — значение хелпера.
func (a *chargeApplied) num(entity string, draft func(string) float64) float64 {
	if a == nil {
		return draft(entity)
	}
	a.mu.Lock()
	v, ok := a.V[entity]
	a.mu.Unlock()
	if !ok {
		return draft(entity)
	}
	return v
}

// apply копирует черновик (хелперы) в применённые; возвращает изменённое.
func (a *chargeApplied) apply(draft func(string) float64) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ch []string
	for _, n := range chargeNumbers {
		v := draft(n.entity)
		if v <= 0 {
			continue // хелпер не прочитан — не затираем
		}
		if old, ok := a.V[n.entity]; !ok || old != v {
			ch = append(ch, fmt.Sprintf("%s %g→%g", n.label, old, v))
			a.V[n.entity] = v
		}
	}
	a.At = time.Now()
	if b, err := json.Marshal(a); err == nil {
		_ = writeAtomic(a.path, b)
	}
	return ch
}

// pending — чем черновик отличается от применённого (для строки на дашборде).
func (a *chargeApplied) pending(draft func(string) float64) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var d []string
	for _, n := range chargeNumbers {
		v := draft(n.entity)
		if old, ok := a.V[n.entity]; ok && v > 0 && old != v {
			d = append(d, fmt.Sprintf("%s %g→%g", n.label, old, v))
		}
	}
	sort.Strings(d)
	return d
}

// empty — ещё ни разу не применяли (первый запуск).
func (a *chargeApplied) empty() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.V) == 0
}

func (a *chargeApplied) at() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.At
}

// publishPending — сенсор «Заряд: изменения»: применено / не применено (что).
func (s *Server) publishPending() {
	d := s.applied.pending(s.store.Num)
	state, icon := "применено", "mdi:check-circle"
	if len(d) > 0 {
		state, icon = "не применено: "+strings.Join(d, ", "), "mdi:content-save-alert"
	}
	if r := []rune(state); len(r) > 250 { // предел состояния HA — 255 символов
		state = string(r[:250])
	}
	_ = s.client.SetState(chargePending, state, map[string]any{"friendly_name": "Заряд: изменения", "icon": icon,
		"pending": len(d), "applied_at": s.applied.at().Format(time.RFC3339)})
}
