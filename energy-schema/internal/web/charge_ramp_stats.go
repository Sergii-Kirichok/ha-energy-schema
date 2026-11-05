package web

import (
	"encoding/json"
	"net/http"
	"os"
	"time"
)

// Статистика сторожа напряжения — чтобы потом подстроить запас, гистерезис и
// шаг по реальной сети: как напряжение фаз отвечает на ток от сети. Строка
// JSON на каждый тик с сетью (≈ раз в минуту, чаще при смене полосы) и на
// каждый шаг/пропажу сети. ≈0,4 МБ в сутки; больше rampStatsMax — файл
// уходит в .1 (хранится одна прошлая часть). Выгрузка — /ramp/stats.
var rampStatsFile = "/data/ramp_stats.jsonl"

const (
	rampStatsMax = 5 << 20
	rampStatsGap = 20 * time.Second // «sample» не чаще (события — всегда)
)

type rampSample struct {
	T      string     `json:"t"`
	Ev     string     `json:"ev"`   // sample | step | grid_down
	Band   string     `json:"band"` // ok | hold | low | severe
	FromA  float64    `json:"from_a"`
	A      float64    `json:"a"`      // ток от сети после решения
	Slider float64    `json:"slider"` // ползунок / задание режима
	V      [3]float64 `json:"v"`
	VLow   float64    `json:"v_low"`
	VHigh  float64    `json:"v_high"`
	GridW  float64    `json:"grid_w"`
	LoadW  float64    `json:"load_w"`
	BattA  float64    `json:"batt_a"` // «−» — заряд
	SOC    float64    `json:"soc"`
}

func (s *Server) rampStat(st *chargeState, ev, band string, from, a, slider float64) {
	if ev == "sample" && time.Since(st.statAt) < rampStatsGap {
		return
	}
	st.statAt = time.Now()
	num := s.store.Num
	r := rampSample{T: time.Now().Format(time.RFC3339), Ev: ev, Band: band, FromA: from, A: a, Slider: slider,
		VLow: st.RampVLow, VHigh: st.RampVHigh, GridW: num("sensor.deye_sun_30k_grid_power"), LoadW: num("sensor.deye_sun_30k_load_power"),
		BattA: num("sensor.deye_sun_30k_battery_current"), SOC: num("sensor.deye_sun_30k_battery")}
	for ph := 1; ph <= 3; ph++ {
		r.V[ph-1] = num(gridVoltEntity(ph))
	}
	b, _ := json.Marshal(r)
	if fi, err := os.Stat(rampStatsFile); err == nil && fi.Size() > rampStatsMax {
		_ = os.Rename(rampStatsFile, rampStatsFile+".1")
	}
	f, err := os.OpenFile(rampStatsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}

// handleRampStats отдаёт статистику целиком (прошлая часть, затем текущая).
func (s *Server) handleRampStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ramp_stats.jsonl"`)
	for _, p := range []string{rampStatsFile + ".1", rampStatsFile} {
		if b, err := os.ReadFile(p); err == nil {
			_, _ = w.Write(b)
		}
	}
}
