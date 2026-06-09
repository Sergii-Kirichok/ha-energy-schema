package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const listen = ":8099"
const wwwDir = "/homeassistant/www"

var apiBase = "http://supervisor/core/api"
var token string
var title = "Энергосистема"
var in1Name = "Рыбхоз"
var in2Name = "Зелёный"
var pvLabels = []string{"Поле 1", "Поле 2", "Поле 3", "Поле 4"}
var refresh = 3

var (
	smu     sync.Mutex
	states  = map[string]string{}
	lossBuf []float64
)

func fetchAll() {
	req, _ := http.NewRequest("GET", apiBase+"/states", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("fetch:", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var arr []struct {
		EntityID string `json:"entity_id"`
		State    string `json:"state"`
	}
	if derr := json.Unmarshal(body, &arr); derr != nil {
		log.Printf("decode err=%v status=%d tokenlen=%d body=%.120q", derr, resp.StatusCode, len(token), string(body))
		return
	}
	m := make(map[string]string, len(arr))
	for _, e := range arr {
		m[e.EntityID] = e.State
	}
	smu.Lock()
	states = m
	smu.Unlock()
}
func stateOf(e string) string {
	smu.Lock()
	v := states[e]
	smu.Unlock()
	return v
}
func numOf(e string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(stateOf(e)), 64)
	if err != nil {
		return 0
	}
	return f
}
func iv(e string) int { return int(math.Round(numOf(e))) }
func on(e string) bool { return stateOf(e) == "on" }
func sampleLoss() {
	lossBuf = append(lossBuf, numOf("sensor.deye_sun_30k_power_losses"))
	if len(lossBuf) > 6 {
		lossBuf = lossBuf[1:]
	}
}
func avgLoss() float64 {
	if len(lossBuf) == 0 {
		return numOf("sensor.deye_sun_30k_power_losses")
	}
	s := 0.0
	for _, x := range lossBuf {
		s += x
	}
	return s / float64(len(lossBuf))
}

const (
	cTxt = "#e5e7eb"
	cSub = "#9ca3af"
	cBox = "#171a20"
	cBrd = "#2b2f38"
	cGrn = "#22c55e"
	cGry = "#6b7280"
	cRed = "#ef4444"
	cOrg = "#f59e0b"
	cAmb = "#f5b300"
	cBlu = "#60a5fa"
	cPur = "#a78bfa"
)

func kw(w float64) string { return fmt.Sprintf("%.2f кВт", w/1000) }

type SB struct{ b strings.Builder }

func (s *SB) p(f string, a ...interface{}) { s.b.WriteString(fmt.Sprintf(f, a...)) }
func (s *SB) box(x, y, w, h float64) {
	s.p(`<rect x="%g" y="%g" width="%g" height="%g" rx="12" fill="%s" stroke="%s" stroke-width="1.5"/>`, x, y, w, h, cBox, cBrd)
}
func (s *SB) t(x, y, sz float64, col, anchor, str string) {
	s.p(`<text x="%g" y="%g" font-size="%g" fill="%s" text-anchor="%s">%s</text>`, x, y, sz, col, anchor, html.EscapeString(str))
}
func (s *SB) dot(x, y, r float64, col string) { s.p(`<circle cx="%g" cy="%g" r="%g" fill="%s"/>`, x, y, r, col) }
func (s *SB) head(x, y, w float64, icon, ttl, statusCol string) {
	s.t(x+12, y+27, 20, cTxt, "start", icon)
	s.t(x+42, y+26, 14, cTxt, "start", ttl)
	if statusCol != "" {
		s.dot(x+w-16, y+20, 6, statusCol)
	}
}
func pt(cx, cy, r, deg float64) (float64, float64) {
	rad := deg * math.Pi / 180
	return cx + r*math.Cos(rad), cy - r*math.Sin(rad)
}
func (s *SB) arc(cx, cy, r, start, end float64, col string, wdt float64) {
	x1, y1 := pt(cx, cy, r, start)
	x2, y2 := pt(cx, cy, r, end)
	large := 0
	if math.Abs(start-end) > 180 {
		large = 1
	}
	s.p(`<path fill="none" stroke="%s" stroke-width="%g" stroke-linecap="round" d="M %.1f %.1f A %g %g 0 %d 1 %.1f %.1f"/>`, col, wdt, x1, y1, r, r, large, x2, y2)
}

// flow line: st = "off" (grey dashed), "bad" (red + X), "on" (animated colored)
func (s *SB) flow(col, st string, magKW float64, reverse bool, pts ...float64) {
	mid := len(pts) / 4 * 2
	mx, my := pts[mid], pts[mid+1]
	if st == "off" {
		s.poly(cGry, 2.5, "7 7", pts...)
		return
	}
	if st == "bad" {
		s.poly(cRed, 2.5, "7 7", pts...)
		s.t(mx, my-7, 18, cRed, "middle", "✕")
		return
	}
	dur := 2.2 - magKW*0.12
	if dur < 0.4 {
		dur = 0.4
	}
	if dur > 2.2 {
		dur = 2.2
	}
	to := "-16"
	if reverse {
		to = "16"
	}
	s.p(`<polyline fill="none" stroke="%s" stroke-width="3.5" stroke-dasharray="9 7" points="`, col)
	for i := 0; i < len(pts); i += 2 {
		s.p("%g,%g ", pts[i], pts[i+1])
	}
	s.p(`"><animate attributeName="stroke-dashoffset" from="0" to="%s" dur="%.2fs" repeatCount="indefinite"/></polyline>`, to, dur)
}
func (s *SB) poly(col string, wdt float64, dash string, pts ...float64) {
	d := ""
	if dash != "" {
		d = ` stroke-dasharray="` + dash + `"`
	}
	s.p(`<polyline fill="none" stroke="%s" stroke-width="%g"%s points="`, col, wdt, d)
	for i := 0; i < len(pts); i += 2 {
		s.p("%g,%g ", pts[i], pts[i+1])
	}
	s.p(`"/>`)
}

// phase indicator color: off->red, out-of-range->orange, ok->green
func phCol(onE, vE string, lo, hi float64) string {
	if !on(onE) {
		return cRed
	}
	v := numOf(vE)
	if v < lo || v > hi {
		return cOrg
	}
	return cGrn
}

func rybLineState() string { // overall Ввод1 line
	ons := 0
	for ph := 1; ph <= 3; ph++ {
		if on(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) {
			ons++
		}
	}
	if ons == 0 {
		return "off"
	}
	if ons < 3 {
		return "bad"
	}
	return "on"
}
func greenLineState() string {
	ons, withV := 0, 0
	for ph := 1; ph <= 3; ph++ {
		if on(fmt.Sprintf("sensor.sim_green_l%d_on", ph)) {
			ons++
			if numOf(fmt.Sprintf("sensor.sim_green_l%d_v", ph)) > 50 {
				withV++
			}
		}
	}
	if ons == 0 {
		return "off"
	}
	if withV == 0 || ons < 3 {
		return "bad"
	}
	return "on"
}

func renderSVG() string {
	s := &SB{}
	s.p(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 940" font-family="Arial,Helvetica,sans-serif">`)
	s.p(`<rect x="0" y="0" width="1440" height="940" fill="#0f1115"/>`)
	s.t(1428, 26, 18, cTxt, "end", title)

	cont := stateOf("sensor.sim_contactor")
	gridIn := stateOf("sensor.sim_inv_grid") == "on"
	avrPos := stateOf("sensor.sim_avr_pos")
	genRun := stateOf("sensor.sim_gen_state") == "running"
	rybSt := rybLineState()
	grnSt := greenLineState()

	// ===================== FLOWS (drawn first, under boxes) =====================
	// Ввод1 -> Стаб L1 -> L2 -> L3 (series)
	s.flow(cGrn, rybSt, 4, false, 226, 145, 242, 145)
	s.flow(cGrn, rybSt, 4, false, 432, 145, 448, 145)
	s.flow(cGrn, rybSt, 4, false, 638, 145, 654, 145)
	// Стаб L3 -> Контактор (down)
	st3 := rybSt
	s.flow(cGrn, st3, 4, false, 749, 270, 749, 300, 126, 300, 126, 330)
	// Ввод2 -> Контактор (down). reverse if export
	exporting := stateOf("sensor.sim_export") == "on" && grnSt == "on"
	s.flow(cBlu, grnSt, 3, exporting, 1319, 270, 1319, 300, 126, 300, 126, 330)
	// Контактор -> Инвертор
	cOn := "on"
	if cont == "off" {
		cOn = "off"
	}
	if !gridIn && cOn == "on" {
		cOn = "bad"
	}
	s.flow(cBlu, cOn, 3, false, 236, 455, 410, 455)
	// Инвертор -> АВР (main)
	avrMain := "on"
	if avrPos != "inverter" {
		avrMain = "off"
	}
	s.flow(cGrn, avrMain, 5, false, 670, 455, 820, 455)
	// Стаб (reserve) -> АВР
	avrRes := "off"
	if avrPos == "reserve" {
		avrRes = rybSt
	}
	s.flow(cGrn, avrRes, 4, false, 844, 200, 930, 200, 930, 330)
	// АВР -> Дом
	s.flow(cGrn, "on", 4, false, 1040, 455, 1180, 455)
	// Батарея <-> Инвертор
	bp := numOf("sensor.deye_sun_30k_battery_power")
	s.flow(cPur, "on", math.Abs(bp)/1000, bp < 0, 316, 720, 470, 720, 470, 580)
	// PV -> Инвертор
	pvtot := numOf("sensor.deye_sun_30k_pv1_power") + numOf("sensor.deye_sun_30k_pv2_power") + numOf("sensor.deye_sun_30k_pv3_power") + numOf("sensor.deye_sun_30k_pv4_power")
	pvSt := "on"
	if pvtot < 10 {
		pvSt = "off"
	}
	s.flow(cAmb, pvSt, pvtot/1000, false, 630, 640, 630, 580)
	// Генератор -> Инвертор
	genSt := "off"
	if genRun {
		genSt = "on"
	}
	s.flow(cGrn, genSt, 3, false, 1010, 640, 1010, 600, 600, 600, 600, 580)

	// ===================== ROW 1 =====================
	// Ввод1 Рыбхоз
	s.box(16, 20, 210, 250)
	s.head(16, 20, 210, "🗼", in1Name, map[string]string{"on": cGrn, "bad": cOrg, "off": cRed}[rybSt])
	s.t(28, 64, 11, cSub, "start", "фаза   U      нагр.")
	for ph := 1; ph <= 3; ph++ {
		y := 64.0 + float64(ph)*30
		c := phCol(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph), fmt.Sprintf("sensor.sim_ryb_l%d_vin", ph), 200, 250)
		s.dot(34, y-4, 6, c)
		s.t(50, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if on(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) {
			s.t(86, y, 13, cTxt, "start", fmt.Sprintf("%dV", iv(fmt.Sprintf("sensor.sim_ryb_l%d_vin", ph))))
			s.t(150, y, 13, cTxt, "start", fmt.Sprintf("%.0fA", numOf(fmt.Sprintf("sensor.sim_ryb_l%d_load", ph))))
		} else {
			s.t(86, y, 12, cRed, "start", "обрыв")
		}
	}
	s.t(28, 200, 11, cSub, "start", "столб 0.4 кВ → стабилизаторы")

	// Стабилизаторы L1/L2/L3
	stabX := []float64{242, 448, 654}
	for i := 0; i < 3; i++ {
		ph := i + 1
		x := stabX[i]
		p := fmt.Sprintf("sensor.sim_ryb_l%d", ph)
		link := stateOf(p + "_link")
		linkCol := cGrn
		if link != "ok" {
			linkCol = cRed
		}
		s.box(x, 20, 190, 250)
		s.head(x, 20, 190, "🔧", fmt.Sprintf("Стабилиз. L%d", ph), linkCol)
		mode := stateOf(p + "_mode")
		mc, mt := cBlu, "стабилизация"
		if mode == "transit" {
			mc, mt = cSub, "транзит"
		}
		s.t(x+12, y2(0), 12, mc, "start", mt)
		row := func(n int, label, val string) {
			s.t(x+12, 64+float64(n)*23+18, 11, cSub, "start", label)
			s.t(x+178, 64+float64(n)*23+18, 13, cTxt, "end", val)
		}
		row(0, "U вход", fmt.Sprintf("%dV", iv(p+"_vin")))
		row(1, "U выход", fmt.Sprintf("%dV", iv(p+"_vout")))
		row(2, "Ступень", fmt.Sprintf("%+d", iv(p+"_step")))
		row(3, "Нагрузка", fmt.Sprintf("%.0fA", numOf(p+"_load")))
		row(4, "Пик (час)", fmt.Sprintf("%.0fA", numOf(p+"_peak")))
		row(5, "U мин/макс", fmt.Sprintf("%d / %dV", iv(p+"_vmin"), iv(p+"_vmax")))
		if !on(p + "_on") {
			s.t(x+95, 250, 12, cRed, "middle", "линия отключена")
		}
	}

	// Ввод2 Зелёный
	s.box(1214, 20, 210, 250)
	s.head(1214, 20, 210, "♻️", in2Name, map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}[grnSt])
	dir := stateOf("sensor.sim_green_dir")
	dt, dc := "потребление", cBlu
	if dir == "export" {
		dt, dc = "отдача в сеть ↑", cGrn
	}
	s.t(1226, 64, 12, dc, "start", dt)
	for ph := 1; ph <= 3; ph++ {
		y := 80.0 + float64(ph)*30
		c := phCol(fmt.Sprintf("sensor.sim_green_l%d_on", ph), fmt.Sprintf("sensor.sim_green_l%d_v", ph), 200, 250)
		s.dot(1232, y-4, 6, c)
		s.t(1248, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if on(fmt.Sprintf("sensor.sim_green_l%d_on", ph)) {
			s.t(1284, y, 13, cTxt, "start", fmt.Sprintf("%dV", iv(fmt.Sprintf("sensor.sim_green_l%d_v", ph))))
			s.t(1348, y, 13, cTxt, "start", fmt.Sprintf("%.0fA", numOf(fmt.Sprintf("sensor.sim_green_l%d_a", ph))))
		} else {
			s.t(1284, y, 12, cGry, "start", "— нет —")
		}
	}

	// ===================== ROW 2 =====================
	// Контактор
	s.box(16, 330, 220, 250)
	s.head(16, 330, 220, "🔀", "Контактор", "")
	act, ac := "выкл", cGry
	if cont == "rybhoz" {
		act, ac = "→ "+in1Name, cGrn
	} else if cont == "green" {
		act, ac = "→ "+in2Name, cBlu
	}
	s.t(126, 410, 15, ac, "middle", act)
	if stateOf("sensor.sim_export") == "on" {
		s.t(126, 440, 12, cGrn, "middle", "отдача → Ввод №2 ↑")
	} else {
		s.t(126, 440, 12, cSub, "middle", "отдача выкл")
	}

	// Инвертор
	s.box(410, 330, 260, 250)
	s.head(410, 330, 260, "🔌", "Инвертор", map[bool]string{true: cGrn, false: cGry}[genRun || gridIn])
	s.t(540, 420, 14, cTxt, "middle", "собств. потребл.")
	s.t(540, 444, 16, cSub, "middle", fmt.Sprintf("%.0f Вт · ср.30с", avgLoss()))
	if gridIn {
		s.t(540, 500, 13, cGrn, "middle", "берёт от сети ✓")
	} else {
		s.t(540, 500, 13, cOrg, "middle", "от сети НЕ берёт ✕")
	}

	// АВР
	s.box(820, 330, 220, 250)
	avrLinkCol := cGrn
	if stateOf("sensor.sim_avr_link") != "ok" {
		avrLinkCol = cRed
	}
	s.head(820, 330, 220, "🔀", "АВР", avrLinkCol)
	if avrPos == "inverter" {
		s.t(930, 415, 15, cGrn, "middle", "через инвертор")
	} else {
		s.t(930, 415, 15, cOrg, "middle", "резерв (Ввод №1)")
	}
	s.t(930, 445, 12, cSub, "middle", "осн.: инвертор · рез.: Ввод №1")

	// Дом
	s.box(1180, 330, 244, 250)
	s.head(1180, 330, 244, "🏠", "Дом", "")
	s.t(1302, 430, 13, cSub, "middle", "потребление (оба дома)")
	s.t(1302, 462, 22, cBlu, "middle", kw(numOf("sensor.sim_home_load_kw")*1000))

	// ===================== ROW 3 =====================
	// Батарея (гейдж)
	s.box(16, 640, 300, 260)
	s.head(16, 640, 300, "🔋", "Батарея", "")
	soc := numOf("sensor.deye_sun_30k_battery")
	bcx, bcy, br := 166.0, 820.0, 78.0
	s.arc(bcx, bcy, br, 180, 0, "#2b2f38", 13)
	socCol := cGrn
	if soc < 20 {
		socCol = cRed
	} else if soc < 50 {
		socCol = cAmb
	}
	s.arc(bcx, bcy, br, 180, 180-soc*1.8, socCol, 13)
	s.t(bcx, bcy-6, 26, cTxt, "middle", fmt.Sprintf("%.0f%%", soc))
	bps, bpc := kw(bp), cPur
	if bp < 0 {
		bps, bpc = "заряд "+kw(-bp), cGrn
	} else if bp > 0 {
		bps, bpc = "разряд "+kw(bp), cOrg
	}
	s.t(bcx, bcy+22, 13, bpc, "middle", bps)

	// Солнечные поля
	s.box(420, 640, 420, 260)
	s.head(420, 640, 420, "☀️", "Солнечные поля", "")
	s.t(432, 700, 11, cSub, "start", "поле")
	s.t(820, 700, 11, cSub, "end", "мощность")
	tot := 0.0
	for i := 0; i < 4; i++ {
		y := 700.0 + float64(i+1)*32
		pw := numOf(fmt.Sprintf("sensor.deye_sun_30k_pv%d_power", i+1))
		tot += pw
		s.dot(440, y-4, 6, map[bool]string{true: cAmb, false: cGry}[pw > 5])
		s.t(456, y, 13, cTxt, "start", pvLabels[i])
		s.t(820, y, 13, cTxt, "end", kw(pw))
	}
	s.t(432, 888, 13, cSub, "start", "Всего")
	s.t(820, 888, 15, cAmb, "end", kw(tot))

	// Генератор
	s.box(940, 640, 484, 260)
	gtc, gtxt := cGry, "выключен"
	if genRun {
		gtc, gtxt = cGrn, "работает"
	}
	s.head(940, 640, 484, "⚙️", "Генератор", gtc)
	gl := func(n int, label, val, col string) {
		s.t(956, 684+float64(n)*26, 12, cSub, "start", label)
		s.t(1180, 684+float64(n)*26, 13, col, "end", val)
	}
	gl(0, "Состояние", gtxt, gtc)
	sig := stateOf("sensor.sim_gen_start_signal") == "on"
	gl(1, "Сигнал на запуск", map[bool]string{true: "ЕСТЬ", false: "нет"}[sig], map[bool]string{true: cOrg, false: cSub}[sig])
	htOn := stateOf("sensor.sim_gen_coolant_heater") == "on"
	gl(2, "Подогрев ОЖ", map[bool]string{true: "вкл", false: "выкл"}[htOn], map[bool]string{true: cOrg, false: cSub}[htOn])
	gl(3, "Темп. ОЖ", fmt.Sprintf("%d°C", iv("sensor.sim_gen_coolant_temp")), cTxt)
	tts := numOf("sensor.sim_gen_time_to_start_min")
	ttsTxt, ttsCol := "—", cSub
	if sig && !genRun {
		ttsTxt, ttsCol = fmt.Sprintf("%.0f мин (прогрев)", tts), cOrg
	}
	gl(4, "До запуска", ttsTxt, ttsCol)
	oil := numOf("sensor.sim_gen_oil_remaining_h")
	oc := cSub
	if oil < 10 {
		oc = cRed
	}
	gl(5, "До замены масла", fmt.Sprintf("%.0f ч", oil), oc)
	gl(6, "Наработка", fmt.Sprintf("%.1f ч", numOf("sensor.sim_gen_runtime_h")), cTxt)
	// per-phase on the right
	s.t(1220, 700, 11, cSub, "start", "фаза   U      нагр.")
	for ph := 1; ph <= 3; ph++ {
		y := 700.0 + float64(ph)*28
		p := fmt.Sprintf("sensor.sim_gen_l%d", ph)
		s.t(1228, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(1280, y, 13, cTxt, "start", fmt.Sprintf("%dV", iv(p+"_v")))
		s.t(1356, y, 13, cTxt, "start", fmt.Sprintf("%.0fA", numOf(p+"_load")))
	}

	s.p(`</svg>`)
	return s.b.String()
}

// y2 helper for stabilizer mode line position
func y2(_ int) float64 { return 60 }

func handleSVG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(renderSVG()))
}
func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;background:#0f1115;height:100%%}#c{width:100%%}svg{width:100%%;height:auto;display:block}</style></head><body><div id="c"></div><script>
function load(){fetch('schematic.svg?t='+Date.now()).then(function(r){return r.text()}).then(function(t){document.getElementById('c').innerHTML=t})}
load();setInterval(load,%d000);</script></body></html>`, refresh)
}

func writeFiles() {
	if err := os.WriteFile(wwwDir+"/energy_schema.svg", []byte(renderSVG()), 0644); err != nil {
		log.Println("write svg:", err)
	}
}
func writeWrapper() {
	h := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;background:#0f1115;height:100%%}#c{width:100%%}svg{width:100%%;height:auto;display:block}</style></head><body><div id="c"></div><script>
function load(){fetch('energy_schema.svg?t='+Date.now()).then(function(r){return r.text()}).then(function(t){document.getElementById('c').innerHTML=t})}
load();setInterval(load,%d000);</script></body></html>`, refresh)
	if err := os.WriteFile(wwwDir+"/energy_schema.html", []byte(h), 0644); err != nil {
		log.Println("write html:", err)
	}
}

func loadOptions() {
	b, err := os.ReadFile("/data/options.json")
	if err != nil {
		return
	}
	var o struct {
		RefreshSeconds int    `json:"refresh_seconds"`
		HaURL          string `json:"ha_url"`
		HaToken        string `json:"ha_token"`
		Title          string `json:"title"`
		In1Name        string `json:"in1_name"`
		In2Name        string `json:"in2_name"`
		Pv1            string `json:"pv1_label"`
		Pv2            string `json:"pv2_label"`
		Pv3            string `json:"pv3_label"`
		Pv4            string `json:"pv4_label"`
	}
	if json.Unmarshal(b, &o) != nil {
		return
	}
	if o.RefreshSeconds > 0 {
		refresh = o.RefreshSeconds
	}
	if o.Title != "" {
		title = o.Title
	}
	if o.In1Name != "" {
		in1Name = o.In1Name
	}
	if o.In2Name != "" {
		in2Name = o.In2Name
	}
	for i, v := range []string{o.Pv1, o.Pv2, o.Pv3, o.Pv4} {
		if v != "" {
			pvLabels[i] = v
		}
	}
	if o.HaToken != "" {
		token = o.HaToken
		u := o.HaURL
		if u == "" {
			u = "http://homeassistant:8123"
		}
		apiBase = strings.TrimRight(u, "/") + "/api"
	}
}

func main() {
	token = os.Getenv("SUPERVISOR_TOKEN")
	loadOptions()
	log.Printf("start: tokenlen=%d apiBase=%s title=%q", len(token), apiBase, title)
	os.MkdirAll(wwwDir, 0755)
	writeWrapper()
	go func() {
		for {
			fetchAll()
			sampleLoss()
			writeFiles()
			time.Sleep(5 * time.Second)
		}
	}()
	http.HandleFunc("/schematic.svg", handleSVG)
	http.HandleFunc("/", handleIndex)
	log.Println("energy-schema add-on on", listen, "refresh", refresh, "s")
	log.Fatal(http.ListenAndServe(listen, nil))
}
