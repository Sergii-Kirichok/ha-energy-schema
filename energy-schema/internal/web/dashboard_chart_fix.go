package web

import "fmt"

// renameChart — «Последний час (живое)» вводил в заблуждение: период задаёт
// переключатель над графиками, поэтому название — по содержанию.
func renameChart(m map[string]any) bool {
	h, _ := m["header"].(map[string]any)
	if h == nil || h["title"] != "Последний час (живое)" {
		return false
	}
	h["title"] = "Мощность: сеть, солнце, нагрузка"
	return true
}

// cellsPrecision — напряжения ячеек в заголовке и легенде до тысячных
// (по умолчанию карточка округляет до десятых: «3,3 V»).
func cellsPrecision(m map[string]any) bool {
	changed := false
	ss, _ := m["series"].([]any)
	for _, s := range ss {
		sm, ok := s.(map[string]any)
		if ok && (sm["entity"] == "sensor.energy_schema_bms_cell_max" || sm["entity"] == "sensor.energy_schema_bms_cell_min") &&
			fmt.Sprint(sm["float_precision"]) != "3" {
			sm["float_precision"], changed = 3, true
		}
	}
	return changed
}
