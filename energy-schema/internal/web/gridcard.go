package web

import "encoding/json"

// Карточка «Сеть и фазы» — entities-card. Строки фаз L1..L3 кликабельны:
// tap_action открывает more-info сенсора grid_outage_lN (состояния норма /
// нет / низкое), то есть цветную полосу «когда был или не было напряжение».
// Цвет иконки фазы по текущему напряжению задаёт card-mod (если установлен —
// HACS; без него строка работает, иконка просто серая): 200–253 В зелёная,
// >253 красная, 20–200 оранжевая, <20 красная. Пороги — как в scada/theme.go.
//
// Значение строки — текстовый сенсор sensor.energy_schema_grid_lN
// ("0 W · 200.0 V", числа в атрибутах), имя строки берётся из его
// friendly_name (сводка отключений за 24 ч, см. gridrows.go / gridoutage.go).

// gridCardMark — маркер прежней markdown-версии карточки (конвертируем из неё).
const gridCardMark = "'sensor.energy_schema_grid_l' ~ n"

// gridIconStyle — card-mod стиль: цвет иконки строки фазы по напряжению.
func gridIconStyle(ph int) string {
	return ":host {\n  --paper-item-icon-color:\n    {% set v = states('" +
		gridVoltEntity(ph) + "') | float(0) %}\n" +
		"    {{ '#ef4444' if v < 20 or v > 253 else ('#f59e0b' if v < 200 else '#22c55e') }};\n}"
}

// gridPhaseRow — строка фазы: имя из friendly_name сенсора, клик → полоса
// отключений, цвет иконки через card-mod.
func gridPhaseRow(ph int) map[string]any {
	return map[string]any{
		"entity":     gridRowEntity(ph),
		"icon":       "mdi:transmission-tower",
		"tap_action": map[string]any{"action": "more-info", "entity": gridOutageEntity(ph)},
		"card_mod":   map[string]any{"style": gridIconStyle(ph)},
	}
}

// gridEntityRows — канонический список строк карточки: мощность, частота, фазы.
func gridEntityRows() []any {
	rows := []any{
		map[string]any{"entity": "sensor.deye_sun_30k_grid_power", "name": "Мощность сети", "icon": "mdi:transmission-tower"},
		map[string]any{"entity": "sensor.deye_sun_30k_grid_frequency", "name": "Частота", "icon": "mdi:sine-wave"},
	}
	for ph := 1; ph <= 3; ph++ {
		rows = append(rows, gridPhaseRow(ph))
	}
	return rows
}

// findGridCard находит карточку «Сеть и фазы» в любой прежней форме:
// markdown (0.6.57+), entities на grid_lN, или исходная на grid_lN_power.
func findGridCard(node any) map[string]any {
	if c := findMarkdownWith(node, gridCardMark); c != nil {
		return c
	}
	if c := findCardWith(node, gridRowEntity(1)); c != nil {
		return c
	}
	return findCardWith(node, gridPowerEntity(1))
}

// patchGridCard приводит карточку «Сеть и фазы» к entities-виду с кликабельными
// строками фаз на месте (title, visibility, позиция сохраняются; markdown-
// content и старые ключах entities-карточки отбрасываются). Идемпотентно.
func patchGridCard(node any) (bool, error) {
	card := findGridCard(node)
	if card == nil {
		return false, nil
	}
	rows := gridEntityRows()
	want, _ := json.Marshal(rows)
	if card["type"] == "entities" {
		if cur, _ := json.Marshal(card["entities"]); string(cur) == string(want) {
			return false, nil
		}
	}
	for _, k := range []string{"content", "state_color", "show_header_toggle"} {
		delete(card, k)
	}
	card["type"], card["entities"] = "entities", rows
	return true, nil
}
