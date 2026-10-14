package web

import "fmt"

// Один переключатель периода (кнопки 1/4/8/12/24 + «Сглаж.») на все графики:
// пользовательский блок — условные карточки (input_select.diapazon_grafika ×
// input_boolean.sglazhivanie_grafikov), в каждой vertical-stack из «Последний
// час» и «Батарея». Сюда же третьим графиком кладём «Ячейки» с тем же периодом
// и сглаживанием, отдельный суточный убираем. И выравниваем поля осей у всех
// трёх (фиксированная ширина подписей слева и справа), чтобы одно время
// приходилось на одну вертикаль.

const (
	periodSelect = "input_select.diapazon_grafika"
	axisLabelW   = 44 // px подписей каждой оси — одинаково у всех графиков
)

// periodStacks — vertical-stack внутри условных карточек выбора периода.
func periodStacks(node any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["type"] == "conditional" && condOn(v, periodSelect) {
				if st, ok := v["card"].(map[string]any); ok && st["type"] == "vertical-stack" {
					out = append(out, st)
					return
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(node)
	return out
}

func condOn(card map[string]any, entity string) bool {
	cs, _ := card["conditions"].([]any)
	for _, c := range cs {
		if m, ok := c.(map[string]any); ok && m["entity"] == entity {
			return true
		}
	}
	return false
}

// spanGroup — период и шаг сглаживания варианта (по первому графику стека).
func spanGroup(stack map[string]any) (span, dur string) {
	cards, _ := stack["cards"].([]any)
	for _, c := range cards {
		m, ok := c.(map[string]any)
		if !ok || m["type"] != "custom:apexcharts-card" {
			continue
		}
		span, _ = m["graph_span"].(string)
		if ss, ok := m["series"].([]any); ok && len(ss) > 0 {
			if g, ok := ss[0].(map[string]any)["group_by"].(map[string]any); ok {
				dur, _ = g["duration"].(string)
			}
		}
		return
	}
	return
}

// cellsChartFor — «Ячейки» под период span; dur — сглаживание линий ("" — без).
func cellsChartFor(span, dur string) map[string]any {
	col := map[string]string{"1h": "1m", "4h": "2m", "8h": "5m", "12h": "5m", "24h": "10m"}[span]
	if col == "" {
		col = "5m"
	}
	line := func(entity, name, color, fn string) map[string]any {
		s := map[string]any{"entity": entity, "name": name, "yaxis_id": "v", "color": color, "stroke_width": 2, "type": "line"}
		if dur != "" {
			s["group_by"] = map[string]any{"func": fn, "duration": dur} // max/min — пики не съедаются
		}
		return s
	}
	return map[string]any{
		"type": "custom:apexcharts-card", "graph_span": span,
		"header":      map[string]any{"show": true, "title": "Ячейки", "show_states": true, "colorize_states": true},
		"apex_config": map[string]any{"chart": map[string]any{"height": 230}, "stroke": map[string]any{"width": 2}},
		"yaxis": []any{
			map[string]any{"id": "v", "decimals": 3, "apex_config": map[string]any{"title": map[string]any{"text": "В"}}},
			map[string]any{"id": "d", "opposite": true, "min": 0, "decimals": 0, "apex_config": map[string]any{"title": map[string]any{"text": "мВ"}}},
		},
		"series": []any{
			line("sensor.energy_schema_bms_cell_max", "макс", "#f59e0b", "max"),
			line("sensor.energy_schema_bms_cell_min", "мин", "#60a5fa", "min"),
			map[string]any{"entity": "sensor.energy_schema_bms_cell_delta", "name": "разбег", "yaxis_id": "d", "type": "column", "color": "#a78bfa",
				"group_by": map[string]any{"func": "max", "duration": col}},
		},
	}
}

// patchPeriodCharts — «Ячейки» в каждый вариант периода, ровные оси, без
// отдельного суточного графика. false, если блока выбора периода нет.
func patchPeriodCharts(node any) bool {
	stacks := periodStacks(node)
	if len(stacks) == 0 {
		return false
	}
	changed := false
	in := map[string]bool{}
	for _, st := range stacks {
		in[ptr(st)] = true
		if findChart(st) == nil {
			span, dur := spanGroup(st)
			st["cards"] = append(st["cards"].([]any), cellsChartFor(span, dur))
			changed = true
		}
		for _, c := range st["cards"].([]any) {
			if m, ok := c.(map[string]any); ok && m["type"] == "custom:apexcharts-card" && alignAxes(m) {
				changed = true
			}
		}
	}
	// отдельный (суточный) график ячеек вне блока периода — убрать
	var loose []map[string]any
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if in[ptr(v)] {
				return
			}
			if findChartSelf(v) {
				loose = append(loose, v)
				return
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(node)
	for _, c := range loose {
		changed = removeCard(node, c) || changed
	}
	return changed
}

func ptr(m map[string]any) string { return fmt.Sprintf("%p", m) }

// findChartSelf — сама карточка — график ячеек (без обхода вглубь).
func findChartSelf(m map[string]any) bool {
	if m["type"] != "custom:apexcharts-card" {
		return false
	}
	c := findChart(m)
	return c != nil && sameMap(c, m)
}

// alignAxes — одинаковые поля: подписи каждой оси фиксированной ширины, две оси
// (слева и справа) у каждого графика. У графика без осей («Последний час»)
// заводим «кВт» слева и справа; правую держит невидимая копия первой серии.
func alignAxes(chart map[string]any) bool {
	labels := map[string]any{"minWidth": axisLabelW, "maxWidth": axisLabelW}
	ys, _ := chart["yaxis"].([]any)
	if len(ys) == 0 {
		ss, _ := chart["series"].([]any)
		if len(ss) == 0 {
			return false
		}
		ax := func(id string, right bool) map[string]any {
			return map[string]any{"id": id, "opposite": right, "decimals": 1,
				"apex_config": map[string]any{"title": map[string]any{"text": "кВт"}, "labels": labels}}
		}
		chart["yaxis"] = []any{ax("kw", false), ax("kw_r", true)}
		for _, s := range ss {
			if m, ok := s.(map[string]any); ok {
				m["yaxis_id"] = "kw"
			}
		}
		ghost := map[string]any{}
		for k, v := range ss[0].(map[string]any) {
			ghost[k] = v
		}
		ghost["yaxis_id"], ghost["name"], ghost["opacity"], ghost["stroke_width"] = "kw_r", " ", 0, 0
		ghost["show"] = map[string]any{"in_header": false, "in_legend": false}
		chart["series"] = append(ss, ghost)
		return true
	}
	changed := false
	for _, y := range ys {
		m, ok := y.(map[string]any)
		if !ok {
			continue
		}
		ac, _ := m["apex_config"].(map[string]any)
		if ac == nil {
			ac = map[string]any{}
			m["apex_config"] = ac
		}
		if l, ok := ac["labels"].(map[string]any); ok && l["minWidth"] != nil {
			continue
		}
		ac["labels"], changed = labels, true
	}
	return changed
}
