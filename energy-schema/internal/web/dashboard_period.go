package web

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Один переключатель периода (кнопки 1/4/8/12/24 + «Сглаж.») на все графики:
// пользовательский блок — условные карточки (input_select.diapazon_grafika ×
// input_boolean.sglazhivanie_grafikov), в каждой vertical-stack из «Последний
// час» и «Батарея». Сюда же третьим графиком кладём «Ячейки» с тем же периодом
// и сглаживанием, отдельный суточный убираем. И выравниваем поля осей у всех
// трёх (минимальная ширина подписей осей), чтобы одно время
// приходилось на одну вертикаль.

const (
	periodSelect = "input_select.diapazon_grafika"
	axisLabelW   = 44 // минимум px подписей каждой оси — одинаково у всех графиков
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
		s := map[string]any{"entity": entity, "name": name, "yaxis_id": "v", "color": color, "stroke_width": 2, "type": "line", "float_precision": 3}
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
			line("sensor.energy_schema_bms_cell_max", "▲ макс", "#f59e0b", "max"),
			line("sensor.energy_schema_bms_cell_min", "▼ мин", "#60a5fa", "min"),
			map[string]any{"entity": "sensor.energy_schema_bms_cell_delta", "name": "Δ разбег", "yaxis_id": "d", "type": "column", "color": "#a78bfa",
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
			if m, ok := c.(map[string]any); ok && m["type"] == "custom:apexcharts-card" && stepLines(m) { // до alignAxes: копии серий берут curve
				changed = true
			}
			if m, ok := c.(map[string]any); ok && m["type"] == "custom:apexcharts-card" && startWithLast(m) {
				changed = true
			}
			if m, ok := c.(map[string]any); ok && m["type"] == "custom:apexcharts-card" && alignAxes(m) {
				changed = true
			}
			if m, ok := c.(map[string]any); ok && renameChart(m) {
				changed = true
			}
			if m, ok := c.(map[string]any); ok && cellsPrecision(m) {
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

// alignAxes — одинаковые поля у всех графиков стека: подписи каждой оси не
// уже axisLabelW (без maxWidth — иначе подписи и заголовок обрезаются), и у
// каждого графика две оси. Графику с одной осью («Последний час») — правая
// ось-зеркало mirrorAxis: невидимые копии его серий на ней (без легенды,
// заголовка и подсказки) дают ровно тот же диапазон, что слева. Без правой оси
// ApexCharts раскладывает поля иначе и время уезжает с вертикали (15.10).
const mirrorAxis = "kw_r"

func alignAxes(chart map[string]any) bool {
	ss, _ := chart["series"].([]any)
	if len(ss) == 0 {
		return false
	}
	before, _ := json.Marshal(chart)
	ys, _ := chart["yaxis"].([]any)
	if len(ys) == 0 {
		ys = []any{map[string]any{"id": "kw", "decimals": 1, "apex_config": map[string]any{"title": map[string]any{"text": "кВт"}}}}
		for _, s := range ss {
			if m, ok := s.(map[string]any); ok {
				m["yaxis_id"] = "kw"
			}
		}
	}
	// зеркало строится заново из текущих серий — сначала убрать прежнее
	var keptS, keptY []any
	for _, s := range ss {
		if m, ok := s.(map[string]any); !ok || m["yaxis_id"] != mirrorAxis {
			keptS = append(keptS, s)
		}
	}
	for _, y := range ys {
		m, ok := y.(map[string]any)
		if !ok {
			keptY = append(keptY, y)
			continue
		}
		if m["id"] == mirrorAxis {
			continue
		}
		keptY = append(keptY, y)
		ac, _ := m["apex_config"].(map[string]any)
		if ac == nil {
			ac = map[string]any{}
			m["apex_config"] = ac
		}
		l, _ := ac["labels"].(map[string]any)
		if l == nil {
			l = map[string]any{}
			ac["labels"] = l
		}
		delete(l, "maxWidth")
		delete(l, "offsetX") // поправки mirrorFix ставятся заново
		if t, ok := ac["title"].(map[string]any); ok {
			delete(t, "offsetX")
		}
		if l["minWidth"] == nil {
			l["minWidth"] = axisLabelW
		}
	}
	if y0, ok := keptY[0].(map[string]any); ok && len(keptY) == 1 && y0["opposite"] != true {
		keptS, keptY = mirror(chart, y0, keptS, keptY)
	}
	chart["series"], chart["yaxis"] = keptS, keptY
	after, _ := json.Marshal(chart)
	return !bytes.Equal(before, after)
}

// mirrorFix — поправки раскладки под число серий n: каждая скрытая правая ось
// (apexcharts-card 2.2.3 заводит по оси на серию) отнимает место слева, и поле
// графика уезжает вправо. Отрицательный отступ возвращает поле, сдвиг подписей
// и заголовка левой оси — их место. Замерено в браузере на карточке 836 px:
// n=3 → поле 97/98, подписи и заголовок как у «Ячеек»; n=1 сходится без
// поправок. ponytail: только замеренные n; новое число серий — замерить и дописать.
var mirrorFix = map[int]struct{ padLeft, labelsX, titleX int }{3: {-80, -92, -9}}

// mirror — правая ось-копия y0 и невидимые (opacity 0; «transparent» карточка
// красит в чёрный) копии серий на ней; подсказка — только по настоящим.
func mirror(chart, y0 map[string]any, ss, ys []any) ([]any, []any) {
	cp := func(v any) map[string]any {
		var m map[string]any
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &m)
		return m
	}
	sub := func(m map[string]any, k string) map[string]any {
		s, _ := m[k].(map[string]any)
		if s == nil {
			s = map[string]any{}
			m[k] = s
		}
		return s
	}
	ax := cp(y0)
	ax["id"], ax["opposite"] = mirrorAxis, true
	ys = append(ys, ax)
	n := len(ss)
	on := make([]any, n)
	for i := 0; i < n; i++ {
		on[i] = i
		g := cp(ss[i])
		g["yaxis_id"], g["opacity"] = mirrorAxis, 0
		g["show"] = map[string]any{"in_header": false, "in_legend": false, "legend_value": false}
		ss = append(ss, g)
	}
	ac := sub(chart, "apex_config")
	sub(ac, "tooltip")["enabledOnSeries"] = on
	if f, ok := mirrorFix[n]; ok {
		sub(sub(ac, "grid"), "padding")["left"] = f.padLeft
		yac := sub(y0, "apex_config")
		sub(yac, "labels")["offsetX"] = f.labelsX
		sub(yac, "title")["offsetX"] = f.titleX
	}
	return ss, ys
}
