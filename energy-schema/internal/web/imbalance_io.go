package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"energy-schema/internal/hass"
)

const (
	// /share переживает обновления аддона и не раздаётся HA по /local/ (в отличие от /config/www)
	imbFile    = "/share/energy_schema/imbalance.jsonl"
	imbEntity  = "sensor.energy_schema_imbalance_last"
	imbPollDur = 5 * time.Second
)

// imbEntities — поля выборки (порядок важен: imbSampleOf, склейка истории).
var imbEntities = []string{
	"sensor.energy_schema_bms_cell_max", "sensor.energy_schema_bms_cell_min",
	"sensor.deye_sun_30k_battery_current", "sensor.deye_sun_30k_battery",
	"sensor.energy_schema_bms_limits", // «X / Y А» — лимит заряда первым
	"sensor.energy_schema_bms_cell_max_loc", "sensor.energy_schema_bms_cell_min_loc",
	chargeBalanceStatus, // фаза — по тексту статуса: в истории (minimal_response) атрибутов нет
}

const imbMaxLocIdx, imbMinLocIdx = 5, 6

var (
	imbBackfillFrom = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	// 0.6.47 (16.10, ~12:04 по Киеву) исправил подписи «М·№»: до него модуль и ячейка перепутаны
	imbLocFixAt = time.Date(2026, 10, 16, 9, 5, 0, 0, time.UTC)
)

func numOrNaN(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

// imbPhaseOf — фаза балансировки по тексту статуса (balanceText).
func imbPhaseOf(text string) string {
	switch {
	case strings.HasPrefix(text, "заряд "):
		return balCharge
	case strings.HasPrefix(text, "пауза:"), strings.HasPrefix(text, "пауза ·"):
		return balRest
	}
	return ""
}

func imbSampleOf(t time.Time, st []string) imbSample {
	limit := math.NaN()
	if f := strings.Fields(st[4]); len(f) > 0 {
		limit = numOrNaN(f[0])
	}
	return imbSample{T: t, Max: numOrNaN(st[0]), Min: numOrNaN(st[1]), I: numOrNaN(st[2]), SOC: numOrNaN(st[3]),
		Limit: limit, MaxLoc: st[imbMaxLocIdx], MinLoc: st[imbMinLocIdx], Phase: imbPhaseOf(st[7])}
}

// imbReplay прогоняет трекер по истории: изменения всех сущностей склеиваются
// по времени, события в пределах 2 с (одна публикация BMS) — одна выборка.
func imbReplay(hist [][]hass.HistState) []*imbRecord {
	type ev struct {
		t     time.Time
		i     int
		state string
	}
	var evs []ev
	for i, h := range hist {
		for _, p := range h {
			evs = append(evs, ev{p.Time, i, p.State})
		}
	}
	sort.SliceStable(evs, func(a, b int) bool { return evs[a].t.Before(evs[b].t) })
	tr, cur, out := &imbTracker{source: "history"}, make([]string, len(imbEntities)), []*imbRecord{}
	var gs time.Time
	newGroup := true
	for k, e := range evs {
		if newGroup {
			gs, newGroup = e.t, false
		}
		if (e.i == imbMaxLocIdx || e.i == imbMinLocIdx) && e.t.Before(imbLocFixAt) {
			e.state = swapLoc(e.state)
		}
		cur[e.i] = e.state
		if k+1 < len(evs) && evs[k+1].t.Sub(gs) < 2*time.Second {
			continue
		}
		newGroup = true
		if r := tr.feed(imbSampleOf(e.t.Truncate(time.Second), cur)); r != nil {
			out = append(out, r)
		}
	}
	return out // незавершённый на «сейчас» подъём отбрасывается
}

// imbJournal — JSON lines; в памяти только счётчик и последняя запись.
type imbJournal struct {
	path string
	n    int
	last *imbRecord
}

func loadImbJournal(path string) *imbJournal {
	j := &imbJournal{path: path}
	b, _ := os.ReadFile(path)
	for _, ln := range strings.Split(string(b), "\n") {
		var r imbRecord
		if strings.TrimSpace(ln) != "" && json.Unmarshal([]byte(ln), &r) == nil {
			j.n, j.last = j.n+1, &r
		}
	}
	return j
}

func (j *imbJournal) add(recs ...*imbRecord) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err == nil {
			_, err = f.Write(append(b, '\n'))
		}
		if err != nil {
			_ = f.Close()
			return err
		}
		j.n, j.last = j.n+1, r
	}
	return f.Close()
}

func ruF(v float64, prec int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', prec, 64), ".", ",", 1)
}

// imbState — «3,55 В: 183 мВ · ≥5,2 А·ч · после паузы».
func imbState(r *imbRecord) string {
	var parts []string
	if n := len(r.Levels); n > 0 {
		parts = append(parts, fmt.Sprintf("%s В: %d мВ", ruF(r.Levels[n-1].V, 2), r.Levels[n-1].DeltaMV))
	} else {
		parts = append(parts, fmt.Sprintf("пик %s В: %d мВ", ruF(r.PeakMaxV, 3), r.PeakDeltaMV))
	}
	for _, v := range []float64{3.40, 3.375, 3.45} { // колено — 3,40; иначе что есть
		if g := r.lag(v); g != nil {
			pre := ""
			if !g.Reached {
				pre = "≥"
			}
			parts = append(parts, pre+ruF(g.Ah, 1)+" А·ч")
			break
		}
	}
	parts = append(parts, map[string]string{imbAfterRest: "после паузы", imbFromFlat: "с плато"}[r.Type])
	return strings.Join(parts, " · ")
}

func (s *Server) publishImbalance(j *imbJournal) {
	state, attrs := "нет данных", map[string]any{}
	if j.last != nil {
		state = imbState(j.last)
		b, _ := json.Marshal(j.last)
		_ = json.Unmarshal(b, &attrs)
	}
	attrs["records"], attrs["file"] = j.n, j.path
	attrs["friendly_name"], attrs["icon"] = "Разбаланс (последний подъём)", "mdi:scale-unbalanced"
	if err := s.client.SetState(imbEntity, state, attrs); err != nil {
		log.Println("imbalance: publish:", err)
	}
}

// imbBackfill — один раз (журнала ещё нет): записи из истории HA.
func (s *Server) imbBackfill(j *imbJournal) error {
	hist := make([][]hass.HistState, len(imbEntities))
	for i, e := range imbEntities {
		h, err := s.client.HistoryStates(e, imbBackfillFrom)
		if err != nil {
			return fmt.Errorf("history %s: %w", e, err)
		}
		hist[i] = h
	}
	recs := imbReplay(hist)
	if err := j.add(recs...); err != nil { // и пустой файл: повторно не восстанавливать
		return err
	}
	log.Printf("imbalance: backfill from history since %s — %d records", imbBackfillFrom.Format(time.RFC3339), len(recs))
	return nil
}

// loopImbalance ведёт журнал разбаланса. Незавершённый подъём — только в
// памяти: рестарт аддона посреди заряда его теряет (допустимо).
func (s *Server) loopImbalance() {
	j := loadImbJournal(imbFile)
	if _, err := os.Stat(imbFile); errors.Is(err, os.ErrNotExist) {
		for try := 1; try <= 5; try++ { // HA при старте может быть ещё не готов
			err := s.imbBackfill(j)
			if err == nil {
				break
			}
			log.Println("imbalance: backfill:", err)
			time.Sleep(time.Minute)
		}
	}
	s.publishImbalance(j)
	tr := &imbTracker{source: "live"}
	st := make([]string, len(imbEntities))
	for tick := 1; ; tick++ {
		time.Sleep(imbPollDur)
		for i, e := range imbEntities {
			st[i] = s.store.State(e)
		}
		r := tr.feed(imbSampleOf(time.Now().Truncate(time.Second), st))
		if r != nil {
			if err := j.add(r); err != nil {
				log.Println("imbalance: write:", err)
			}
			log.Printf("imbalance: %s", imbState(r))
		}
		if r != nil || tick%12 == 0 { // раз в минуту: POST-сенсоры HA теряет при своём рестарте
			s.publishImbalance(j)
		}
	}
}
