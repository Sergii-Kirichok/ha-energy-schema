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

// cellsNames — подписи серий «Ячеек» со значками (в шапке значение крупно,
// подпись под ним — значок перед числом карточка поставить не даёт).
var cellsNames = map[string]string{"sensor.energy_schema_bms_cell_max": "▲ макс",
	"sensor.energy_schema_bms_cell_min": "▼ мин", "sensor.energy_schema_bms_cell_delta": "Δ разбег"}

// cellsPrecision — серии «Ячеек»: напряжения в шапке и легенде до тысячных
// (по умолчанию карточка округляет до десятых: «3,3 V») и подписи со значками.
func cellsPrecision(m map[string]any) bool {
	changed := false
	ss, _ := m["series"].([]any)
	for _, s := range ss {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		e, _ := sm["entity"].(string)
		if n, ok := cellsNames[e]; ok && sm["name"] != n {
			sm["name"], changed = n, true
		}
		if (e == "sensor.energy_schema_bms_cell_max" || e == "sensor.energy_schema_bms_cell_min") && fmt.Sprint(sm["float_precision"]) != "3" {
			sm["float_precision"], changed = 3, true
		}
	}
	return changed
}

// stepLines — сырые линии ступенькой: состояние HA держится до следующего
// изменения, а сглаженная кривая между редкими точками рисовала то, чего не
// было (17.10: сеть «нарастала» 40 мин, пока света не было). Линии со
// сглаживанием (group_by с усреднением) — плавные, как было: ступенька
// превращала их в лесенку.
func stepLines(m map[string]any) bool {
	changed := false
	ss, _ := m["series"].([]any)
	for _, s := range ss {
		sm, ok := s.(map[string]any)
		if !ok || sm["type"] == "column" {
			continue
		}
		g, _ := sm["group_by"].(map[string]any)
		smooth := g != nil && g["func"] != nil && g["func"] != "raw"
		switch {
		case smooth && sm["curve"] == "stepline":
			delete(sm, "curve")
			changed = true
		case !smooth && sm["curve"] != "stepline":
			sm["curve"], changed = "stepline", true
		}
	}
	return changed
}
