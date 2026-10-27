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
//     Запас — как «Доступно» на схеме (scada.BattUsable). Подпись «2 д 05 ч» —
//     своя карточка custom:energy-schema-gauge (gaugecard.go): нативный гейдж
//     HA с подменённым текстом значения;
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

// battDaysCard — своя карточка custom:energy-schema-gauge (gaugecard.js):
// обычный гейдж HA с подписью «1 д 19 ч»; цвет дуги — штатные пороги
// (< 1 дня красный, 1–2 жёлтый, дальше зелёный).
func battDaysCard() map[string]any {
	return map[string]any{"type": "custom:energy-schema-gauge", "entity": battDaysEntity, "name": "АКБ хватит",
		"format": "days", "min": 0, "max": battDaysGaugeTo, "needle": false,
		"severity":     map[string]any{"red": 0, "yellow": 1, "green": 2},
		"grid_options": map[string]any{"columns": 6, "rows": "auto"}}
}

// cardPos — список карточек и индекс card в нём.
func cardPos(node any, card map[string]any) (map[string]any, string, int) {
	h := findCardsHolding(node, card)
	if h == nil {
		return nil, "", -1
	}
	holder, key := h[0].(map[string]any), h[1].(string)
	for i, c := range holder[key].([]any) {
		if m, ok := c.(map[string]any); ok && sameMap(m, card) {
			return holder, key, i
		}
	}
	return nil, "", -1
}

func insertAt(holder map[string]any, key string, i int, card any) {
	l := holder[key].([]any)
	if i < 0 || i > len(l) {
		i = len(l)
	}
	holder[key] = append(l[:i:i], append([]any{card}, l[i:]...)...)
}

// patchGauges держит «АКБ хватит» (своя карточка) и «Солнце сегодня». Место,
// куда их перенёс пользователь, сохраняется: прежние версии «АКБ хватит»
// (нативный гейдж 0.6.74, три apexcharts 0.6.75–0.6.78) заменяются на месте;
// новой карточки нет нигде — ставится в конец сетки с «Солнце (PV)». Наши
// поля карточки обновляются, grid_options и прочее пользователя не трогаем.
func patchGauges(node any) (bool, error) {
	changed := removeOwnEntityCard(node, "gauge", gridOffEntity)
	want := battDaysCard()
	var holder map[string]any
	key, at := "", -1
	old := func() map[string]any {
		if c := findOwnEntityCard(node, "gauge", battDaysEntity); c != nil {
			return c
		}
		return findApexWith(node, battDaysEntity)
	}
	for c := old(); c != nil; c = old() {
		if holder == nil {
			holder, key, at = cardPos(node, c)
		}
		removeCard(node, c)
		changed = true
	}
	if cur := findOwnEntityCard(node, "custom:energy-schema-gauge", battDaysEntity); cur != nil {
		holder, key, at = cardPos(node, cur)
		for k, v := range want {
			if k != "grid_options" && mustJSON(cur[k]) != mustJSON(v) {
				cur[k] = v
				changed = true
			}
		}
		at++ // «Солнце сегодня», если его нет, — сразу за ним
	} else {
		if holder == nil {
			pv := findOwnEntityCard(node, "gauge", "sensor.deye_pv_power_kw")
			if pv == nil {
				return changed, nil
			}
			h := findCardsHolding(node, pv)
			holder, key = h[0].(map[string]any), h[1].(string)
		}
		insertAt(holder, key, at, want)
		if at >= 0 {
			at++
		}
		changed = true
	}
	if findOwnEntityCard(node, "gauge", pvTodayEntity) == nil && holder != nil {
		cp := map[string]any{}
		for k, v := range pvTodayCard {
			cp[k] = v
		}
		insertAt(holder, key, at, cp)
		changed = true
	}
	return changed, nil
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
