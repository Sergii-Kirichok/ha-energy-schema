package web

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"energy-schema/internal/hass"
)

// Пороги напряжения фазы: ниже voltOff — напряжения нет (≈0), от voltOff до
// voltLow — пониженное, voltLow и выше — норма. unavailable/unknown — «нет
// данных» (перезапуск HA), а не отключение.
const (
	voltOff = 20.0
	voltLow = 150.0
)

const (
	clsNoData = iota
	clsOff
	clsLow
	clsOK
)

func voltClass(state string) int {
	v, err := strconv.ParseFloat(strings.TrimSpace(state), 64)
	switch {
	case err != nil || math.IsNaN(v):
		return clsNoData
	case v < voltOff:
		return clsOff
	case v < voltLow:
		return clsLow
	}
	return clsOK
}

// outageCat — итог по одной категории (нет / низкое) за окно.
type outageCat struct {
	Minutes            float64
	Count              int // входы в категорию из другого известного состояния
	LastStart, LastEnd time.Time
}

type outageStats struct {
	Off, Low outageCat
	Known    bool // было хоть одно числовое значение
}

// outageStatsOf считает время и число отключений/просадок фазы в окне
// [since, now] по истории состояний (отсортирована по времени). Последний
// отсчёт тянется до now; категория, идущая с начала окна, считается разом.
func outageStatsOf(sts []hass.HistState, since, now time.Time) outageStats {
	var st outageStats
	cat := func(c int) *outageCat {
		switch c {
		case clsOff:
			return &st.Off
		case clsLow:
			return &st.Low
		}
		return nil
	}
	prev := clsNoData
	for i, p := range sts {
		end := now
		if i+1 < len(sts) && sts[i+1].Time.Before(now) {
			end = sts[i+1].Time
		}
		t := p.Time
		if t.Before(since) {
			t = since
		}
		if !end.After(since) || t.After(now) {
			continue // отрезок целиком вне окна
		}
		c := voltClass(p.State)
		if c == clsNoData {
			continue
		}
		st.Known = true
		if c != prev {
			if pc := cat(prev); pc != nil {
				pc.LastEnd = t
			}
			if nc := cat(c); nc != nil {
				nc.Count++
				nc.LastStart, nc.LastEnd = t, time.Time{}
			}
			prev = c
		}
		if nc := cat(c); nc != nil {
			nc.Minutes += end.Sub(t).Minutes()
		}
	}
	return st
}

func fmtDurMin(min float64) string {
	m := int(math.Round(min))
	switch h, mm := m/60, m%60; {
	case m < 1:
		return "<1 мин"
	case h > 0 && mm > 0:
		return fmt.Sprintf("%d ч %d мин", h, mm)
	case h > 0:
		return fmt.Sprintf("%d ч", h)
	default:
		return fmt.Sprintf("%d мин", m)
	}
}

// outageText — «нет 2 ч 15 мин ×3 · низкое 10 мин ×1», «норма», «нет данных».
func outageText(st outageStats) string {
	if !st.Known {
		return "нет данных"
	}
	var parts []string
	for _, c := range []struct {
		name string
		c    outageCat
	}{{"нет", st.Off}, {"низкое", st.Low}} {
		if c.c.Count > 0 {
			parts = append(parts, fmt.Sprintf("%s %s ×%d", c.name, fmtDurMin(c.c.Minutes), c.c.Count))
		}
	}
	if len(parts) == 0 {
		return "норма"
	}
	return strings.Join(parts, " · ")
}

func isoOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.RFC3339)
}

func gridOutageEntity(ph int) string { return fmt.Sprintf("sensor.energy_schema_grid_outage_l%d", ph) }

// publishOutages публикует итоги за 24 ч по каждой фазе (из истории HA).
func (s *Server) publishOutages() {
	now := time.Now()
	since := now.Add(-24 * time.Hour)
	for ph := 1; ph <= 3; ph++ {
		sts, err := s.client.HistoryStates(gridVoltEntity(ph), since)
		if err != nil {
			log.Printf("grid outage: L%d: %v", ph, err)
			continue
		}
		st := outageStatsOf(sts, since, now)
		attrs := map[string]any{
			"friendly_name": fmt.Sprintf("L%d без напряжения / низкое · 24 ч", ph),
			"icon":          "mdi:transmission-tower-off",
			"off_minutes":   math.Round(st.Off.Minutes), "off_count": st.Off.Count,
			"off_last_start": isoOrEmpty(st.Off.LastStart), "off_last_end": isoOrEmpty(st.Off.LastEnd),
			"low_minutes": math.Round(st.Low.Minutes), "low_count": st.Low.Count,
			"low_last_start": isoOrEmpty(st.Low.LastStart), "low_last_end": isoOrEmpty(st.Low.LastEnd),
		}
		if err := s.client.SetState(gridOutageEntity(ph), outageText(st), attrs); err != nil {
			log.Printf("grid outage: L%d: %v", ph, err)
		}
	}
}

// patchGridOutage adds the three outage rows right after the L1..L3 rows of
// the «Сеть и фазы» card (runs after patchGridRows); no-op once present.
func patchGridOutage(node any) (bool, error) {
	card := findCardWith(node, gridRowEntity(1))
	if card == nil || findCardWith(node, gridOutageEntity(1)) != nil {
		return false, nil
	}
	ents, _ := card["entities"].([]any)
	at := -1
	for i, e := range ents {
		switch entityID(e) {
		case gridRowEntity(1), gridRowEntity(2), gridRowEntity(3):
			at = i
		}
	}
	rows := make([]any, 0, len(ents)+3)
	rows = append(rows, ents[:at+1]...)
	for ph := 1; ph <= 3; ph++ {
		rows = append(rows, map[string]any{"entity": gridOutageEntity(ph)})
	}
	card["entities"] = append(rows, ents[at+1:]...)
	return true, nil
}
