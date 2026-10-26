package web

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"energy-schema/internal/hass"
	"energy-schema/internal/scada"
)

// Два гейджа на дашборде рядом с «Солнце (PV)» и «Заряд АКБ»:
//   - «АКБ хватит» — сколько дней проработаем только от батареи при текущей
//     нагрузке (запас до отключения — как «Доступно» на схеме, scada.BattUsable);
//   - отсчёт без света — часы с момента, когда последний раз были все три
//     фазы; 0, пока свет есть. Имя гейджа берётся из friendly_name сенсора:
//     «Свет есть» / «Нет света · с 14:05» / «Нет L3 · с 14:05».
const (
	battDaysEntity = "sensor.energy_schema_battery_days"
	gridOffEntity  = "sensor.energy_schema_grid_off_hours"
	battDaysMax    = 99.0 // нагрузки почти нет — «очень долго»
)

// battDays — дни работы только от АКБ при нагрузке loadKW.
func battDays(usableKWh, loadKW float64) float64 {
	if loadKW < 0.05 {
		return battDaysMax
	}
	return math.Min(battDaysMax, usableKWh/loadKW/24)
}

// phasesOn — сколько фаз сейчас с напряжением (≥ voltOff) и есть ли данные по всем.
func (s *Server) phasesOn() (on []bool, known bool) {
	known = true
	for ph := 1; ph <= 3; ph++ {
		c := voltClass(s.store.State(gridVoltEntity(ph)))
		known = known && c != clsNoData
		on = append(on, c == clsLow || c == clsOK)
	}
	return on, known
}

// lastFullGrid — последний момент, когда напряжение было на всех трёх фазах,
// по истории фаз за окно [since, now]; since, если за окно такого не было.
func lastFullGrid(hist [3][]hass.HistState, since, now time.Time) time.Time {
	type ev struct {
		t  time.Time
		ph int
		on bool
	}
	var evs []ev
	for ph, sts := range hist {
		for _, p := range sts {
			if c := voltClass(p.State); c != clsNoData {
				evs = append(evs, ev{p.Time, ph, c != clsOff})
			}
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].t.Before(evs[j].t) })
	var on [3]bool
	last := since
	for _, e := range evs {
		if on[0] && on[1] && on[2] && e.t.After(last) {
			last = e.t // все три держались до этого события
		}
		on[e.ph] = e.on
	}
	if on[0] && on[1] && on[2] {
		return now
	}
	return last
}

// gridOffName — имя гейджа по фазам: «Свет есть», «Нет света · с …», «Нет L3 · с …».
func gridOffName(on []bool, since, now time.Time) string {
	var off []string
	for i, o := range on {
		if !o {
			off = append(off, fmt.Sprintf("L%d", i+1))
		}
	}
	if len(off) == 0 {
		return "Свет есть"
	}
	what := "Нет света"
	if len(off) < 3 {
		what = "Нет " + strings.Join(off, ", ")
	}
	at := since.Local().Format("15:04")
	if y, d := since.Local().YearDay(), now.Local().YearDay(); y != d {
		at = since.Local().Format("02.01 15:04")
	}
	return fmt.Sprintf("%s · с %s", what, at)
}

// loopGauges публикует оба сенсора раз в 30 с. Отсчёт без света при старте
// восстанавливается из истории фаз за 3 суток, дальше ведётся по живым значениям.
func (s *Server) loopGauges() {
	now := time.Now()
	since := now.Add(-72 * time.Hour)
	var hist [3][]hass.HistState
	for ph := 1; ph <= 3; ph++ {
		sts, err := s.client.HistoryStates(gridVoltEntity(ph), since)
		if err != nil {
			log.Printf("gauges: L%d history: %v", ph, err)
		}
		hist[ph-1] = sts
	}
	lastOn := lastFullGrid(hist, since, now)
	for {
		now = time.Now()
		u := scada.BattUsable(s.store, s.cfg.BattCap)
		load := s.store.Num("sensor.deye_sun_30k_load_power") / 1000
		days := battDays(u.KWh, load)
		_ = s.client.SetState(battDaysEntity, fmt.Sprintf("%.1f", days), map[string]any{
			"friendly_name": "АКБ хватит", "icon": "mdi:battery-clock", "unit_of_measurement": "дн",
			"state_class": "measurement", "usable_kwh": math.Round(u.KWh*10) / 10, "load_kw": math.Round(load*100) / 100,
			"hours": math.Round(days*24*10) / 10,
		})
		if on, known := s.phasesOn(); known {
			if on[0] && on[1] && on[2] {
				lastOn = now
			}
			hours := now.Sub(lastOn).Hours()
			_ = s.client.SetState(gridOffEntity, fmt.Sprintf("%.1f", hours), map[string]any{
				"friendly_name": gridOffName(on, lastOn, now), "icon": "mdi:transmission-tower-off",
				"unit_of_measurement": "ч", "state_class": "measurement", "since": lastOn.Local().Format(time.RFC3339),
			})
		}
		time.Sleep(30 * time.Second)
	}
}

// gaugeCards — карточки двух гейджей (в сетку гейджей раздела «Сейчас»).
var gaugeCards = []map[string]any{
	{"type": "gauge", "entity": battDaysEntity, "name": "АКБ хватит", "unit": "дн", "min": 0, "max": 10, "needle": false,
		"severity": map[string]any{"red": 0, "yellow": 1, "green": 3}, "grid_options": map[string]any{"columns": 6, "rows": "auto"}},
	{"type": "gauge", "entity": gridOffEntity, "unit": "ч", "min": 0, "max": 12, "needle": false, // имя — из friendly_name
		"severity": map[string]any{"green": 0, "yellow": 2, "red": 6}, "grid_options": map[string]any{"columns": 6, "rows": "auto"}},
}

// patchGauges добавляет гейджи в конец списка, где стоит гейдж «Солнце (PV)»
// (sensor.deye_pv_power_kw); уже добавленные не трогает.
func patchGauges(node any) (bool, error) {
	pv := findOwnEntityCard(node, "gauge", "sensor.deye_pv_power_kw")
	if pv == nil {
		return false, nil
	}
	h := findCardsHolding(node, pv)
	if h == nil {
		return false, nil
	}
	holder, key := h[0].(map[string]any), h[1].(string)
	changed := false
	for _, c := range gaugeCards {
		if findOwnEntityCard(node, "gauge", c["entity"].(string)) != nil {
			continue
		}
		cp := map[string]any{}
		for k, v := range c {
			cp[k] = v
		}
		holder[key] = append(holder[key].([]any), cp)
		changed = true
	}
	return changed, nil
}
