package web

import (
	"fmt"
	"time"
)

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
		smooth := g != nil && g["func"] != nil && g["func"] != "raw" || sm["statistics"] != nil
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

// startWithLast — группировке нужно значение на начало окна: без него редко
// меняющиеся серии (SOC, напряжения ячеек) начинались с первого изменения
// внутри окна, левая часть графика была пустой (17.10, проверено в браузере).
func startWithLast(m map[string]any) bool {
	changed := false
	ss, _ := m["series"].([]any)
	for _, s := range ss {
		sm, _ := s.(map[string]any)
		g, _ := sm["group_by"].(map[string]any)
		if g != nil && g["start_with_last"] != true {
			g["start_with_last"], changed = true, true
		}
	}
	return changed
}

// statsSmooth — сглаживание на 4–24 ч из 5-минутной статистики HA, а не
// группировкой истории: группировка не видит значение, державшееся с момента
// до начала окна (17.10: сеть стояла на 0 с 09:48 до 16:21 — на 12 ч линии не
// было 6 ч), а статистика пишется каждые 5 мин независимо от изменений.
// Проверено в браузере: линии с края окна, поля осей 97/98. Час остаётся на
// группировке — статистики там лишь 12 точек и последние 5–10 мин ещё нет.
func statsSmooth(m map[string]any) bool {
	span, _ := m["graph_span"].(string)
	if h, err := time.ParseDuration(span); err != nil || h < 4*time.Hour {
		return false
	}
	changed := false
	ss, _ := m["series"].([]any)
	for _, s := range ss {
		sm, _ := s.(map[string]any)
		g, _ := sm["group_by"].(map[string]any)
		fn, _ := g["func"].(string)
		if g == nil || fn == "" || fn == "raw" {
			continue
		}
		typ := map[string]string{"max": "max", "min": "min"}[fn]
		if typ == "" {
			typ = "mean"
		}
		sm["statistics"] = map[string]any{"type": typ, "period": "5minute"}
		delete(sm, "group_by")
		changed = true
	}
	return changed
}
