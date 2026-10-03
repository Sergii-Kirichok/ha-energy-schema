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
	b, err := appendCardOnce(node, chartHas(node), dashBMSChart)
	return a || b, err
}

// chartHas — уже есть график ячеек (ищем по сущности разбега в apexcharts).
func chartHas(node any) bool {
	var walk func(any) bool
	walk = func(n any) bool {
		switch v := n.(type) {
		case map[string]any:
			if v["type"] == "custom:apexcharts-card" {
				if ss, ok := v["series"].([]any); ok {
					for _, s := range ss {
						if m, ok := s.(map[string]any); ok && m["entity"] == "sensor.energy_schema_bms_cell_delta" {
							return true
						}
					}
				}
			}
			for _, c := range v {
				if walk(c) {
					return true
				}
			}
		case []any:
			for _, c := range v {
				if walk(c) {
					return true
				}
			}
		}
		return false
	}
	return walk(node)
}
