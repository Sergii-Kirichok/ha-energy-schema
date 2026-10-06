package web

// Карточки детализации BMS на дашборде: сводка стойки (где крайние ячейки,
// температуры, перезаряды, изоляция, лимиты, слабая ячейка по статистике) и
// график напряжений крайних ячеек + разбега за сутки (apexcharts-card стоит).
const dashBMSMarker = "sensor.energy_schema_bms_weak_cell"

var dashBMSDetailCard = map[string]any{
	"type": "entities", "title": "BMS · стойка (12 модулей)", "show_header_toggle": false,
	"entities": []any{
		map[string]any{"entity": "sensor.energy_schema_bms_cell_max", "name": "Ячейка макс"},
		map[string]any{"entity": "sensor.energy_schema_bms_cell_max_loc", "name": "… модуль · ячейка"},
		map[string]any{"entity": "sensor.energy_schema_bms_cell_min", "name": "Ячейка мин"},
		map[string]any{"entity": "sensor.energy_schema_bms_cell_min_loc", "name": "… модуль · ячейка"},
		map[string]any{"entity": "sensor.energy_schema_bms_cell_delta", "name": "Разбег ячеек"},
		map[string]any{"entity": dashBMSMarker, "name": "Слабая ячейка (статистика)"},
		map[string]any{"entity": "sensor.energy_schema_bms_temp_max", "name": "Темп. ячеек макс"},
		map[string]any{"entity": "sensor.energy_schema_bms_temp_min", "name": "Темп. ячеек мин"},
		map[string]any{"entity": "sensor.energy_schema_bms_limits", "name": "Лимиты BMS заряд / разряд"},
		map[string]any{"entity": "sensor.energy_schema_bms_overcharge", "name": "Перезарядов за жизнь"},
		map[string]any{"entity": "sensor.energy_schema_bms_insulation", "name": "Изоляция"},
	},
}

var dashBMSChart = map[string]any{
	"type": "custom:apexcharts-card", "graph_span": "24h",
	"header": map[string]any{"show": true, "title": "Ячейки · 24 ч", "show_states": true, "colorize_states": true},
	"yaxis": []any{
		map[string]any{"id": "v", "decimals": 3, "apex_config": map[string]any{"title": map[string]any{"text": "В"}}},
		map[string]any{"id": "d", "opposite": true, "min": 0, "decimals": 0, "apex_config": map[string]any{"title": map[string]any{"text": "мВ"}}},
	},
	"series": []any{
		map[string]any{"entity": "sensor.energy_schema_bms_cell_max", "name": "макс", "yaxis_id": "v", "color": "#f59e0b", "stroke_width": 2},
		map[string]any{"entity": "sensor.energy_schema_bms_cell_min", "name": "мин", "yaxis_id": "v", "color": "#60a5fa", "stroke_width": 2},
		map[string]any{"entity": "sensor.energy_schema_bms_cell_delta", "name": "разбег", "yaxis_id": "d", "type": "column", "color": "#a78bfa",
			"group_by": map[string]any{"func": "max", "duration": "30min"}},
	},
}

// patchBMSCards adds the BMS detail card and the cells chart next to the battery card once.
func patchBMSCards(node any) (bool, error) {
	a, err := appendCardOnce(node, findCardWith(node, dashBMSMarker) != nil, dashBMSDetailCard)
	if err != nil {
		return false, err
	}
	b, err := appendCardOnce(node, findChart(node) != nil, dashBMSChart)
	if err != nil {
		return a || b, err
	}
	c := layoutChart(node)
	return a || b || c, nil
}

// layoutChart дописывает графику недостающее, каждое — один раз: высоту как у
// остальных графиков (230) и, пока нет grid_options, всю ширину секции с
// переносом в конец её списка. Заданное пользователем не трогаем.
func layoutChart(node any) bool {
	chart := findChart(node)
	if chart == nil || chart["grid_options"] != nil && chart["apex_config"] != nil {
		return false
	}
	h := findCardsHolding(node, chart)
	if h == nil {
		return false
	}
	nc := map[string]any{}
	for k, v := range chart {
		nc[k] = v
	}
	if nc["apex_config"] == nil {
		nc["apex_config"] = map[string]any{"chart": map[string]any{"height": 230}}
	}
	holder, key := h[0].(map[string]any), h[1].(string)
	if nc["grid_options"] != nil { // только высота — на месте
		for i, c := range holder[key].([]any) {
			if cm, ok := c.(map[string]any); ok && sameMap(cm, chart) {
				holder[key].([]any)[i] = nc
			}
		}
		return true
	}
	nc["grid_options"] = map[string]any{"columns": "full"}
	removeCard(node, chart)
	holder[key] = append(holder[key].([]any), nc)
	return true
}

// findChart — график ячеек (apexcharts с сущностью разбега), nil если нет.
func findChart(node any) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == "custom:apexcharts-card" {
			if ss, ok := v["series"].([]any); ok {
				for _, s := range ss {
					if m, ok := s.(map[string]any); ok && m["entity"] == "sensor.energy_schema_bms_cell_delta" {
						return v
					}
				}
			}
		}
		for _, c := range v {
			if r := findChart(c); r != nil {
				return r
			}
		}
	case []any:
		for _, c := range v {
			if r := findChart(c); r != nil {
				return r
			}
		}
	}
	return nil
}

// Таблица «батарея — строка» как в BMS-приложениях. Инвертор отдаёт полный
// набор только на батарею (BMS1; BMS2 у нас пуст, 0 батарей) — строка одна.
// Помодульные строки возможны только при чтении BMS стойки напрямую.
const dashBMSTableKey = "| Батарея | U | I |"

var dashBMSTable = map[string]any{
	"type": "markdown",
	"content": "#### Батареи\n" + dashBMSTableKey + " SOC | SOH | Циклы | Ячейки | t | Δ |\n|---|--:|--:|--:|--:|--:|--:|--:|--:|\n" +
		"| 1 · стойка, 12 мод. | {{ states('sensor.deye_sun_30k_battery_voltage') | float(0) | round(1) }} В" +
		" | {{ states('sensor.deye_sun_30k_battery_current') | float(0) | round(1) }} А" +
		" | {{ states('sensor.deye_sun_30k_battery') }} %" +
		" | {{ states('sensor.energy_schema_bms_soh') }} %" +
		" | {{ states('sensor.energy_schema_bms_cycles') }}" +
		" | {{ states('sensor.energy_schema_bms_cell_min') }}–{{ states('sensor.energy_schema_bms_cell_max') }} В" +
		" | {{ states('sensor.energy_schema_bms_temp_min') }}–{{ states('sensor.energy_schema_bms_temp_max') }} °C" +
		" | {{ states('sensor.energy_schema_bms_cell_delta') }} мВ |\n\n" +
		"<small>мин: {{ states('sensor.energy_schema_bms_cell_min_loc') }} · макс: {{ states('sensor.energy_schema_bms_cell_max_loc') }} (модуль·ячейка)</small>",
}

func patchBMSTable(node any) (bool, error) {
	return appendCardOnce(node, findMarkdownWith(node, dashBMSTableKey) != nil, dashBMSTable)
}
