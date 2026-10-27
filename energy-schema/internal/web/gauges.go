package web

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"energy-schema/internal/hass"
	"energy-schema/internal/scada"
)

// Два гейджа в конце сетки гейджей «Сейчас» (рядом с «Солнце (PV)»):
//   - «АКБ хватит» — сколько проработаем только от батареи при СРЕДНЕЙ нагрузке
//     за последние сутки (мгновенная скачет: 8 кВт отопления давали «0,5 дн»).
//     Запас — как «Доступно» на схеме (scada.BattUsable). Значение «2 д 05 ч»
//     нативный gauge не умеет — карточка apexcharts radialBar со своей подписью;
//   - «Солнце сегодня» — выработка PV за день (кВт·ч), нативный gauge.
const (
	battDaysEntity  = "sensor.energy_schema_battery_days"
	gridOffEntity   = "sensor.energy_schema_grid_off_hours" // гейдж убран 0.6.75 — карточку вычищаем
	pvTodayEntity   = "sensor.deye_sun_30k_today_production"
	loadEntity      = "sensor.deye_sun_30k_load_power"
	battDaysMax     = 99.0 // нагрузки почти нет — «очень долго»
	battDaysGaugeTo = 7    // шкала гейджа, дней
)

// battDays — дни работы только от АКБ при нагрузке loadKW.
func battDays(usableKWh, loadKW float64) float64 {
	if loadKW < 0.05 {
		return battDaysMax
	}
	return math.Min(battDaysMax, usableKWh/loadKW/24)
}

// avgPowerKW — средняя по времени мощность (Вт в истории → кВт) за [since, now];
// нечисловые отрезки (unavailable) не считаются. ok=false — данных нет.
func avgPowerKW(sts []hass.HistState, since, now time.Time) (float64, bool) {
	var sum, dur float64
	for i, p := range sts {
		v, err := strconv.ParseFloat(p.State, 64)
		if err != nil || math.IsNaN(v) {
			continue
		}
		t, end := p.Time, now
		if i+1 < len(sts) && sts[i+1].Time.Before(now) {
			end = sts[i+1].Time
		}
		if t.Before(since) {
			t = since
		}
		if d := end.Sub(t).Hours(); d > 0 {
			sum += v * d
			dur += d
		}
	}
	if dur < 1 { // меньше часа данных — не среднее
		return 0, false
	}
	return sum / dur / 1000, true
}

// loopGauges публикует «АКБ хватит» раз в 30 с; среднюю нагрузку за сутки
// пересчитывает по истории раз в 10 мин (до первого расчёта — текущая).
func (s *Server) loopGauges() {
	var avg float64
	var nextAvg time.Time
	for {
		now := time.Now()
		if now.After(nextAvg) {
			if sts, err := s.client.HistoryStates(loadEntity, now.Add(-24*time.Hour)); err != nil {
				log.Printf("gauges: load history: %v", err)
			} else if a, ok := avgPowerKW(sts, now.Add(-24*time.Hour), now); ok {
				avg = a
			}
			nextAvg = now.Add(10 * time.Minute)
		}
		load := s.store.Num(loadEntity) / 1000
		basis := avg
		if basis <= 0 {
			basis = load
		}
		u := scada.BattUsable(s.store, s.cfg.BattCap)
		days := battDays(u.KWh, basis)
		_ = s.client.SetState(battDaysEntity, fmt.Sprintf("%.2f", days), map[string]any{
			"friendly_name": "АКБ хватит", "icon": "mdi:battery-clock", "unit_of_measurement": "дн",
			"state_class": "measurement", "usable_kwh": math.Round(u.KWh*10) / 10,
			"avg_load_kw_24h": math.Round(avg*100) / 100, "load_kw": math.Round(load*100) / 100,
			"hours": math.Round(days*24*10) / 10,
		})
		time.Sleep(30 * time.Second)
	}
}

// battDaysCards — полукруглый гейдж apexcharts: подпись «2 д 05 ч» / «18 ч 40 м»
// / «45 м». Ширина графика задана явно: иначе ApexCharts рисует 300 px и на
// карточке в полширины подпись обрезается. radialBar отдаёт в формат проценты шкалы — переводим обратно через
// battDaysGaugeTo. Цвет дуги по значению radialBar не умеет (colors-функция и
// color_threshold игнорируются, проверено 27.10) — три копии с разным цветом,
// видна одна по условию: < 1 дня красная, 1–2 жёлтая, дальше зелёная.
func battDaysCards() []any {
	var out []any
	for _, b := range []struct {
		color string
		cond  map[string]any
	}{
		{"#db4437", map[string]any{"below": 1}},
		{"#ffa600", map[string]any{"above": 0.999, "below": 2}},
		{"#43a047", map[string]any{"above": 1.999}},
	} {
		c := battDaysCard(b.color)
		cond := map[string]any{"condition": "numeric_state", "entity": battDaysEntity}
		for k, v := range b.cond {
			cond[k] = v
		}
		c["visibility"] = []any{cond}
		out = append(out, c)
	}
	return out
}

func battDaysCard(color string) map[string]any {
	to := strconv.Itoa(battDaysGaugeTo)
	return map[string]any{
		"type": "custom:apexcharts-card", "chart_type": "radialBar", "header": map[string]any{"show": false},
		"grid_options": map[string]any{"columns": 6, "rows": "auto"},
		"series": []any{map[string]any{"entity": battDaysEntity, "name": "АКБ хватит", "min": 0, "max": battDaysGaugeTo,
			"color": color, "show": map[string]any{"legend_value": false}}},
		"apex_config": map[string]any{
			"chart": map[string]any{"height": 180, "width": 170}, "legend": map[string]any{"show": false},
			"plotOptions": map[string]any{"radialBar": map[string]any{
				"startAngle": -90, "endAngle": 90, "hollow": map[string]any{"size": "62%"},
				"track": map[string]any{"background": "#2a2a2a"},
				"dataLabels": map[string]any{
					"name": map[string]any{"show": true, "offsetY": 24, "fontSize": "13px", "color": "#e5e5e5"},
					"value": map[string]any{"show": true, "offsetY": -8, "fontSize": "22px", "color": "#e5e5e5",
						"formatter": "EVAL:function(p){var m=Math.round(p*" + to + "*1440/100);var d=Math.floor(m/1440),h=Math.floor(m%1440/60),r=m%60;" +
							"return d>0?d+' д '+String(h).padStart(2,'0')+' ч':(h>0?h+' ч '+String(r).padStart(2,'0')+' м':r+' м');}"},
				},
			}},
		},
	}
}

var pvTodayCard = map[string]any{"type": "gauge", "entity": pvTodayEntity, "name": "Солнце сегодня", "unit": "kWh",
	"min": 0, "max": 150, "needle": false, // без severity: с одним порогом HA карточку не рисует (27.10)
	"grid_options": map[string]any{"columns": 6, "rows": "auto"}}

// findApexWith — карточка apexcharts, у которой серия на entity.
func findApexWith(node any, entity string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == "custom:apexcharts-card" {
			if ss, ok := v["series"].([]any); ok && len(ss) > 0 {
				if m, ok := ss[0].(map[string]any); ok && m["entity"] == entity {
					return v
				}
			}
		}
		for _, c := range v {
			if r := findApexWith(c, entity); r != nil {
				return r
			}
		}
	case []any:
		for _, c := range v {
			if r := findApexWith(c, entity); r != nil {
				return r
			}
		}
	}
	return nil
}

// isOurGauge — карточка из нашего блока: гейджи «АКБ хватит» (любой версии),
// отсчёт без света (0.6.74) и «Солнце сегодня».
func isOurGauge(c any) bool {
	m, ok := c.(map[string]any)
	if !ok {
		return false
	}
	switch m["type"] {
	case "gauge":
		e := m["entity"]
		return e == battDaysEntity || e == gridOffEntity || e == pvTodayEntity
	case "custom:apexcharts-card":
		return findApexWith(m, battDaysEntity) != nil
	}
	return false
}

// patchGauges держит в конце сетки с «Солнце (PV)» блок: три цветные копии
// «АКБ хватит» и «Солнце сегодня». Если блок отличается (нет, старый, лишние
// карточки 0.6.74) — наши карточки убираются и блок ставится заново в конец.
func patchGauges(node any) (bool, error) {
	pv := findOwnEntityCard(node, "gauge", "sensor.deye_pv_power_kw")
	if pv == nil {
		return false, nil
	}
	h := findCardsHolding(node, pv)
	if h == nil {
		return false, nil
	}
	// нативные гейджи 0.6.74 убираем, где бы они ни стояли (их переносили в «Сеть и фазы»)
	removed := removeOwnEntityCard(node, "gauge", battDaysEntity)
	removed = removeOwnEntityCard(node, "gauge", gridOffEntity) || removed
	holder, key := h[0].(map[string]any), h[1].(string)
	want := append(battDaysCards(), map[string]any{})
	for k, v := range pvTodayCard {
		want[len(want)-1].(map[string]any)[k] = v
	}
	var kept, ours []any
	for _, c := range holder[key].([]any) {
		if isOurGauge(c) {
			ours = append(ours, c)
		} else {
			kept = append(kept, c)
		}
	}
	if mustJSON(ours) == mustJSON(want) {
		return removed, nil
	}
	holder[key] = append(kept, want...)
	return true, nil
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
