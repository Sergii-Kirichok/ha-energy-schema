package web

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"
)

// Учёт записей в инвертор за сутки (регистры ложатся в энергонезависимую
// память — следим за частотой). Сохраняется в /data, переживает рестарты.
const (
	writesFile        = "/data/writes.json"
	chargeMinWriteGap = 10 * time.Minute // между некритичными записями регистра 108
)

type writeLog struct {
	mu   sync.Mutex
	path string
	Day  string         `json:"day"`
	N    map[string]int `json:"n"` // "reg108" | "reg128" | "switch" | "page" → записей сегодня
}

func loadWriteLog(path string) *writeLog {
	w := &writeLog{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, w)
	}
	if w.N == nil {
		w.N = map[string]int{}
	}
	return w
}

// roll обнуляет счётчики при смене суток (под замком).
func (w *writeLog) roll(now time.Time) {
	if d := now.Format("2006-01-02"); d != w.Day {
		w.Day, w.N = d, map[string]int{}
	}
}

func (w *writeLog) add(kind string, n int) {
	if w == nil || n <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.roll(time.Now())
	w.N[kind] += n
	if b, err := json.Marshal(w); err == nil {
		_ = writeAtomic(w.path, b)
	}
}

// today — всего записей сегодня и разбивка по видам.
func (w *writeLog) today() (int, map[string]int) {
	if w == nil {
		return 0, map[string]int{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.roll(time.Now())
	total, c := 0, map[string]int{}
	for k, v := range w.N {
		total += v
		c[k] = v
	}
	return total, c
}

// publishWrites — сенсор «Записей в инвертор за сутки».
func (s *Server) publishWrites() {
	total, by := s.writes.today()
	attrs := map[string]any{"friendly_name": "Записей в инвертор за сутки", "icon": "mdi:counter",
		"state_class": "measurement"}
	for k, v := range by {
		attrs[k] = v
	}
	_ = s.client.SetState("sensor.energy_schema_inverter_writes", strconv.Itoa(total), attrs)
}
