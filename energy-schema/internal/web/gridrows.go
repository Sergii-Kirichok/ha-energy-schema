package web

import (
	"fmt"
	"log"
	"time"
)

// Карточка «Сеть и фазы»: по каждой фазе одной строкой мощность и напряжение.
// Нативная entities-строка показывает одну сущность, поэтому публикуем
// текстовый сенсор sensor.energy_schema_grid_lN = "40 W · 237.3 V" (числа —
// в атрибутах) и переключаем на него строки L1..L3. Исходные сенсоры Deye
// не трогаем — их история и графики остаются.
func gridRowEntity(ph int) string { return fmt.Sprintf("sensor.energy_schema_grid_l%d", ph) }

func gridPowerEntity(ph int) string { return fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_power", ph) }

func gridVoltEntity(ph int) string { return fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_voltage", ph) }

// gridRowText — текст строки фазы; ok=false, если хотя бы одно значение недоступно.
func (s *Server) gridRowText(ph int) (string, float64, float64, bool) {
	pe, ve := gridPowerEntity(ph), gridVoltEntity(ph)
	if !s.store.Available(pe) || !s.store.Available(ve) {
		return "", 0, 0, false
	}
	p, v := s.store.Num(pe), s.store.Num(ve)
	return fmt.Sprintf("%.0f W · %.1f V", p, v), p, v, true
}

// loopGridRows публикует строки фаз раз в 5 с, только при изменении.
func (s *Server) loopGridRows() {
	last := map[int]string{}
	for {
		for ph := 1; ph <= 3; ph++ {
			txt, p, v, ok := s.gridRowText(ph)
			if !ok {
				txt = "нет данных"
			}
			if txt == last[ph] {
				continue
			}
			attrs := map[string]any{"friendly_name": fmt.Sprintf("L%d", ph), "icon": "mdi:transmission-tower",
				"power_w": p, "voltage_v": v}
			if err := s.client.SetState(gridRowEntity(ph), txt, attrs); err != nil {
				log.Printf("grid rows: L%d: %v", ph, err)
				continue
			}
			last[ph] = txt
		}
		time.Sleep(5 * time.Second)
	}
}

// patchGridRows switches the L1..L3 rows (grid_lN_power) of any entities card
// to the combined sensors, keeping the row names.
func patchGridRows(node any) (bool, error) {
	changed := false
	for ph := 1; ph <= 3; ph++ {
		card := findCardWith(node, gridPowerEntity(ph))
		if card == nil {
			continue
		}
		ents, _ := card["entities"].([]any)
		for i, e := range ents {
			if entityID(e) != gridPowerEntity(ph) {
				continue
			}
			if m, ok := e.(map[string]any); ok {
				m["entity"] = gridRowEntity(ph)
			} else {
				ents[i] = map[string]any{"entity": gridRowEntity(ph), "name": fmt.Sprintf("L%d", ph)}
			}
			changed = true
		}
	}
	return changed, nil
}
