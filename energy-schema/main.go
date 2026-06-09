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
var battCap = 30.0
var homeMax, homeT1, homeT2, homeT3 = 30.0, 3.0, 5.0, 25.0
var pvMax, pvT1, pvT2, pvT3 = 33.0, 5.0, 20.0, 25.0

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
		log.Printf("decode err=%v status=%d tokenlen=%d", derr, resp.StatusCode, len(token))
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
func iv(e string) int  { return int(math.Round(numOf(e))) }
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
func (s *SB) poly(col string, wdt float64, dash string, pts ...float64) {
	d := ""
	if dash != "" {
		d = ` stroke-dasharray="` + dash + `"`
	}
	s.p(`<polyline fill="none" stroke="%s" stroke-width="%g" stroke-linejoin="round"%s points="`, col, wdt, d)
	for i := 0; i < len(pts); i += 2 {
		s.p("%g,%g ", pts[i], pts[i+1])
	}
	s.p(`"/>`)
}
func pathD(pts []float64) string {
	d := "M"
	for i := 0; i < len(pts); i += 2 {
		d += fmt.Sprintf(" %g,%g", pts[i], pts[i+1])
	}
	return d
}
func revPts(pts []float64) []float64 {
	r := make([]float64, len(pts))
	n := len(pts)
	for i := 0; i < n; i += 2 {
		r[i] = pts[n-2-i]
		r[i+1] = pts[n-1-i]
	}
	return r
}

// flow: st = off (grey dashed) / bad (red dashed + X) / on (solid + moving dots)
func (s *SB) flow(col, st string, magKW float64, reverse bool, pts ...float64) {
	if st == "off" {
		s.poly(cGry, 2, "7 7", pts...)
		return
	}
	if st == "bad" {
		s.poly(cRed, 2.5, "7 7", pts...)
		mx, my := pts[len(pts)/2/2*2], pts[len(pts)/2/2*2+1]
		s.t(mx, my-6, 17, cRed, "middle", "✕")
		return
	}
	s.poly(col, 3, "", pts...)
	pd := pathD(pts)
	if reverse {
		pd = pathD(revPts(pts))
	}
	dur := 2.6 - magKW*0.12
	if dur < 0.5 {
		dur = 0.5
	}
	if dur > 2.6 {
		dur = 2.6
	}
	for k := 0; k < 3; k++ {
		s.p(`<circle r="4.5" fill="%s"><animateMotion dur="%.2fs" repeatCount="indefinite" begin="%.2fs" path="%s"/></circle>`, col, dur, float64(k)*dur/3, pd)
	}
}

func pt(cx, cy, r, deg float64) (float64, float64) {
	rad := deg * math.Pi / 180
	return cx + r*math.Cos(rad), cy - r*math.Sin(rad)
}
func (s *SB) arc(cx, cy, r, a1, a2 float64, col string, wdt float64) {
	x1, y1 := pt(cx, cy, r, a1)
	x2, y2 := pt(cx, cy, r, a2)
	large := 0
	if math.Abs(a1-a2) > 180 {
		large = 1
	}
	s.p(`<path fill="none" stroke="%s" stroke-width="%g" stroke-linecap="butt" d="M %.1f %.1f A %g %g 0 %d 1 %.1f %.1f"/>`, col, wdt, x1, y1, r, r, large, x2, y2)
}

type band struct {
	thr float64
	col string
}

func (s *SB) gauge(cx, cy, r, val, max float64, bands []band, valTxt, label string) {
	ang := func(v float64) float64 {
		if v > max {
			v = max
		}
		if v < 0 {
			v = 0
		}
		return 180 - v/max*180
	}
	prev := 0.0
	cur := cSub
	for _, b := range bands {
		s.arc(cx, cy, r, ang(prev), ang(b.thr), b.col, 12)
		if val >= prev && val < b.thr {
			cur = b.col
		}
		prev = b.thr
	}
	nx, ny := pt(cx, cy, r-3, ang(val))
	s.poly("#e5e7eb", 2.5, "", cx, cy, nx, ny)
	s.dot(cx, cy, 4, "#e5e7eb")
	s.t(cx, cy+26, 19, cur, "middle", valTxt)
	if label != "" {
		s.t(cx, cy+44, 11, cSub, "middle", label)
	}
}

// horizontal scale bar with color zones + marker + value text
func (s *SB) bar(x, y, w, h, val, max float64, bands []band, valTxt string) {
	prev := 0.0
	for _, b := range bands {
		x1 := x + w*prev/max
		x2 := x + w*math.Min(b.thr, max)/max
		s.p(`<rect x="%.1f" y="%g" width="%.1f" height="%g" fill="%s" opacity="0.85"/>`, x1, y, x2-x1, h, b.col)
		prev = b.thr
	}
	s.p(`<rect x="%g" y="%g" width="%g" height="%g" rx="6" fill="none" stroke="%s" stroke-width="1"/>`, x, y, w, h, cBrd)
	mv := val
	if mv > max {
		mv = max
	}
	mx := x + w*mv/max
	s.poly("#ffffff", 2.5, "", mx, y-3, mx, y+h+3)
	s.t(x+w/2, y+h/2+7, 20, "#ffffff", "middle", valTxt)
}

// icons (centered at ix,iy ~ 13px)
func (s *SB) icon(kind string, ix, iy float64, col string) {
	switch kind {
	case "fish":
		s.p(`<g fill="%s"><ellipse cx="%g" cy="%g" rx="11" ry="6.5"/><polygon points="%g,%g %g,%g %g,%g"/></g><circle cx="%g" cy="%g" r="1.5" fill="#0f1115"/>`, col, ix-2, iy, ix+8, iy, ix+17, iy-6, ix+17, iy+6, ix-7, iy-1.5)
	case "sine":
		s.p(`<path d="M %g %g c 3,-13 8,-13 11,0 c 3,13 8,13 11,0" fill="none" stroke="%s" stroke-width="2.5"/>`, ix-13, iy, col)
	case "inv":
		s.p(`<rect x="%g" y="%g" width="26" height="22" rx="3" fill="none" stroke="%s" stroke-width="2"/><line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="2"/><path d="M %g %g c 2,-5 5,-5 6,0 c 1,5 4,5 6,0" fill="none" stroke="%s" stroke-width="1.8"/>`, ix-13, iy-11, col, ix, iy-9, ix, iy+9, col, ix+2, iy, col)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.8"/>`, ix-9, iy-3, ix-3, iy-3, col)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.8"/>`, ix-9, iy+3, ix-3, iy+3, col)
	case "gen":
		s.p(`<circle cx="%g" cy="%g" r="12" fill="none" stroke="%s" stroke-width="2"/><text x="%g" y="%g" font-size="14" font-weight="bold" fill="%s" text-anchor="middle">G</text>`, ix, iy, col, ix, iy+5, col)
	case "genrun":
		s.p(`<circle cx="%g" cy="%g" r="12" fill="none" stroke="%s" stroke-width="2"/><text x="%g" y="%g" font-size="14" font-weight="bold" fill="%s" text-anchor="middle">G</text>`, ix, iy, col, ix, iy+5, col)
		for i := 0; i < 3; i++ {
			cx := ix - 4 + float64(i)*5
			s.p(`<circle cx="%g" cy="%g" r="3" fill="#9ca3af"><animate attributeName="cy" values="%g;%g" dur="1.6s" repeatCount="indefinite" begin="%.1fs"/><animate attributeName="opacity" values="0.7;0" dur="1.6s" repeatCount="indefinite" begin="%.1fs"/></circle>`, cx, iy-14, iy-14, iy-26, float64(i)*0.5, float64(i)*0.5)
		}
	case "home":
		s.p(`<path d="M %g %g L %g %g L %g %g L %g %g L %g %g Z" fill="none" stroke="%s" stroke-width="2"/>`, ix-12, iy+10, ix-12, iy-2, ix, iy-12, ix+12, iy-2, ix+12, iy+10, col)
	case "batt":
		s.p(`<rect x="%g" y="%g" width="22" height="16" rx="2" fill="none" stroke="%s" stroke-width="2"/><rect x="%g" y="%g" width="3" height="8" fill="%s"/>`, ix-12, iy-8, col, ix+10, iy-4, col)
	case "sun":
		s.p(`<circle cx="%g" cy="%g" r="7" fill="none" stroke="%s" stroke-width="2"/>`, ix, iy, col)
		for a := 0; a < 8; a++ {
			x1, y1 := pt(ix, iy, 10, float64(a)*45)
			x2, y2 := pt(ix, iy, 14, float64(a)*45)
			s.p(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2"/>`, x1, y1, x2, y2, col)
		}
	case "leaf":
		s.p(`<path d="M %g %g q 14,-16 0,-22 q -14,6 0,22 Z" fill="none" stroke="%s" stroke-width="2"/>`, ix, iy+10, col)
	case "sw":
		s.p(`<circle cx="%g" cy="%g" r="3" fill="%s"/><circle cx="%g" cy="%g" r="3" fill="%s"/><line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="2.5"/>`, ix-11, iy+6, col, ix+11, iy+6, col, ix-11, iy+6, ix+9, iy-7, col)
	}
}

func (s *SB) head(x, y, w float64, kind, ttl, statusCol string) {
	s.icon(kind, x+22, y+22, cTxt)
	s.t(x+44, y+27, 14, cTxt, "start", ttl)
	if statusCol != "" {
		s.dot(x+w-16, y+18, 6, statusCol)
	}
}

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
func rybLineState() string {
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
	s.p(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 960" font-family="Arial,Helvetica,sans-serif">`)
	s.p(`<rect x="0" y="0" width="1440" height="960" fill="#0f1115"/>`)
	s.t(1428, 28, 18, cTxt, "end", title)

	cont := stateOf("sensor.sim_contactor")
	gridIn := stateOf("sensor.sim_inv_grid") == "on"
	avrPos := stateOf("sensor.sim_avr_pos")
	genRun := stateOf("sensor.sim_gen_state") == "running"
	rybSt := rybLineState()
	grnSt := greenLineState()
	exporting := stateOf("sensor.sim_export") == "on" && grnSt == "on"
	load := numOf("sensor.sim_home_load_kw")
	pvtot := numOf("sensor.deye_sun_30k_pv1_power") + numOf("sensor.deye_sun_30k_pv2_power") + numOf("sensor.deye_sun_30k_pv3_power")
	bp := numOf("sensor.deye_sun_30k_battery_power")

	// ===== FLOWS (under boxes) =====
	stOn := map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}
	// Рыбхоз -> каждый стабилизатор (отдельные линии)
	rc := stOn[rybSt]
	s.flow(rc, rybSt, 2, false, 204, 96, 280, 96, 280, 44, 395, 44, 395, 44)
	s.flow(rc, rybSt, 2, false, 204, 120, 250, 120, 250, 230, 615, 230, 615, 194)
	s.flow(rc, rybSt, 2, false, 204, 144, 230, 144, 230, 246, 835, 246, 835, 194)
	// Стаб -> Контактор (стабилизированный Ввод1)
	s.flow(cGrn, rybSt, 3, false, 395, 194, 395, 268, 119, 268, 119, 300)
	// Ввод2 -> Контактор
	s.flow(cBlu, grnSt, 2, exporting, 1085, 120, 1085, 274, 119, 274, 119, 300)
	// Контактор -> Инвертор
	cSt := "on"
	if cont == "off" {
		cSt = "off"
	} else if !gridIn {
		cSt = "bad"
	}
	s.flow(cBlu, cSt, 2, false, 214, 380, 400, 380)
	// Инвертор -> АВР (осн.)
	s.flow(cGrn, map[bool]string{true: "on", false: "off"}[avrPos == "inverter"], 4, false, 630, 380, 800, 380)
	// Стаб -> АВР (резерв)
	s.flow(cGrn, map[bool]string{true: rybSt, false: "off"}[avrPos == "reserve"], 3, false, 900, 194, 900, 380)
	// АВР -> Дом
	s.flow(cGrn, "on", 3, false, 1000, 380, 1140, 380)
	// Батарея <-> Инвертор
	s.flow(cPur, "on", math.Abs(bp)/1000, bp < 0, 174, 520, 174, 488, 470, 488, 470, 460)
	// PV -> Инвертор
	s.flow(cAmb, map[bool]string{true: "on", false: "off"}[pvtot > 30], pvtot/1000, false, 540, 520, 540, 460)
	// Генератор -> Инвертор: 2 линии (управление + мощность)
	s.flow(cOrg, "on", 1, false, 1010, 520, 1010, 496, 600, 496, 600, 470)
	s.flow(cGrn, map[bool]string{true: "on", false: "off"}[genRun], 2, false, 1060, 520, 1060, 484, 588, 484, 588, 460)

	// ===================== ROW 1 =====================
	s.box(24, 64, 180, 140)
	s.head(24, 64, 180, "fish", in1Name, stOn[rybSt])
	for ph := 1; ph <= 3; ph++ {
		y := 96.0 + float64(ph-1)*24
		c := phCol(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph), fmt.Sprintf("sensor.sim_ryb_l%d_vin", ph), 200, 250)
		s.dot(40, y-4, 5, c)
		s.t(54, y, 12, cTxt, "start", fmt.Sprintf("L%d", ph))
		if on(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) {
			s.t(80, y, 12, cTxt, "start", fmt.Sprintf("%dВ", iv(fmt.Sprintf("sensor.sim_ryb_l%d_vin", ph))))
			s.t(190, y, 12, cTxt, "end", fmt.Sprintf("%.0fА", numOf(fmt.Sprintf("sensor.sim_ryb_l%d_load", ph))))
		} else {
			s.t(80, y, 11, cRed, "start", "обрыв")
		}
	}
	// Стабилизаторы
	stabX := []float64{300, 520, 740}
	for i := 0; i < 3; i++ {
		ph := i + 1
		x := stabX[i]
		p := fmt.Sprintf("sensor.sim_ryb_l%d", ph)
		linkCol := cGrn
		if stateOf(p+"_link") != "ok" {
			linkCol = cRed
		}
		s.box(x, 44, 190, 150)
		s.head(x, 44, 190, "sine", fmt.Sprintf("Стаб L%d", ph), linkCol)
		mc, mt := cBlu, "стабилизация"
		if stateOf(p+"_mode") == "transit" {
			mc, mt = cSub, "транзит"
		}
		s.t(x+12, 84, 11, mc, "start", mt)
		loadA := numOf(p + "_load")
		row := func(n int, label, val string) {
			s.t(x+12, 100+float64(n)*22, 11, cSub, "start", label)
			s.t(x+178, 100+float64(n)*22, 12, cTxt, "end", val)
		}
		row(0, "вход → выход", fmt.Sprintf("%d → %dВ", iv(p+"_vin"), iv(p+"_vout")))
		row(1, "ступень", fmt.Sprintf("%+d", iv(p+"_step")))
		row(2, "нагрузка", fmt.Sprintf("%.0fА · %.2fкВт", loadA, loadA*numOf(p+"_vout")/1000))
		row(3, "U мин/макс (ч)", fmt.Sprintf("%d / %dВ", iv(p+"_vmin"), iv(p+"_vmax")))
		if !on(p + "_on") {
			s.t(x+95, 188, 11, cRed, "middle", "линия отключена")
		}
	}
	// Ввод2 Зелёный
	s.box(1000, 44, 180, 160)
	s.head(1000, 44, 180, "leaf", in2Name, map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}[grnSt])
	dt, dc := "потребление", cBlu
	if stateOf("sensor.sim_green_dir") == "export" {
		dt, dc = "отдача ↑", cGrn
	}
	s.t(1012, 86, 12, dc, "start", dt)
	for ph := 1; ph <= 3; ph++ {
		y := 104.0 + float64(ph-1)*26
		c := phCol(fmt.Sprintf("sensor.sim_green_l%d_on", ph), fmt.Sprintf("sensor.sim_green_l%d_v", ph), 200, 250)
		s.dot(1016, y-4, 5, c)
		s.t(1030, y, 12, cTxt, "start", fmt.Sprintf("L%d", ph))
		if on(fmt.Sprintf("sensor.sim_green_l%d_on", ph)) {
			s.t(1058, y, 12, cTxt, "start", fmt.Sprintf("%dВ", iv(fmt.Sprintf("sensor.sim_green_l%d_v", ph))))
			s.t(1168, y, 12, cTxt, "end", fmt.Sprintf("%.0fА", numOf(fmt.Sprintf("sensor.sim_green_l%d_a", ph))))
		} else {
			s.t(1058, y, 11, cGry, "start", "— нет —")
		}
	}

	// ===================== ROW 2 =====================
	s.box(24, 300, 190, 160)
	s.head(24, 300, 190, "sw", "Контактор", "")
	act, ac := "выкл", cGry
	if cont == "rybhoz" {
		act, ac = "→ "+in1Name, cGrn
	} else if cont == "green" {
		act, ac = "→ "+in2Name, cBlu
	}
	s.t(119, 380, 15, ac, "middle", act)
	if stateOf("sensor.sim_export") == "on" {
		s.t(119, 410, 11, cGrn, "middle", "отдача → "+in2Name+" ↑")
	} else {
		s.t(119, 410, 11, cSub, "middle", "отдача выкл")
	}

	s.box(400, 300, 230, 160)
	s.head(400, 300, 230, "inv", "Инвертор", map[bool]string{true: cGrn, false: cGry}[genRun || gridIn])
	s.t(515, 388, 12, cSub, "middle", "потери (ср.)")
	s.t(515, 410, 16, cSub, "middle", fmt.Sprintf("%.0f Вт", avgLoss()))
	if gridIn {
		s.t(515, 440, 12, cGrn, "middle", "берёт от сети ✓")
	} else {
		s.t(515, 440, 12, cOrg, "middle", "от сети НЕ берёт ✕")
	}

	s.box(800, 300, 200, 160)
	avrLinkCol := cGrn
	if stateOf("sensor.sim_avr_link") != "ok" {
		avrLinkCol = cRed
	}
	s.head(800, 300, 200, "sw", "АВР", avrLinkCol)
	s.t(812, 352, 10, cSub, "start", "вход: инвертор")
	s.t(812, 368, 10, cSub, "start", "резерв: "+in1Name)
	s.t(988, 360, 10, cSub, "end", "выход: Дом")
	if avrPos == "inverter" {
		s.t(900, 410, 14, cGrn, "middle", "→ инвертор")
	} else {
		s.t(900, 410, 14, cOrg, "middle", "→ резерв")
	}

	// Дом — гейдж
	s.box(1140, 290, 280, 190)
	s.head(1140, 290, 280, "home", "Дом", "")
	s.gauge(1280, 410, 78, load, homeMax, []band{{homeT1, cGrn}, {homeT2, cAmb}, {homeT3, cOrg}, {homeMax, cRed}}, kw(load*1000), "потребление")

	// ===================== ROW 3 =====================
	// Батарея
	s.box(24, 520, 300, 400)
	s.head(24, 520, 300, "batt", "Батарея", "")
	soc := numOf("sensor.deye_sun_30k_battery")
	bcx, bcy := 174.0, 660.0
	s.arc(bcx, bcy, 92, 180, 0, "#2b2f38", 14)
	socCol := cGrn
	if soc < 20 {
		socCol = cRed
	} else if soc < 50 {
		socCol = cAmb
	}
	s.arc(bcx, bcy, 92, 180, 180-soc*1.8, socCol, 14)
	s.t(bcx, bcy-4, 30, cTxt, "middle", fmt.Sprintf("%.0f%%", soc))
	bps, bpc := "ожидание", cSub
	if bp < -20 {
		bps, bpc = "заряд "+kw(-bp), cGrn
	} else if bp > 20 {
		bps, bpc = "разряд "+kw(bp), cOrg
	}
	s.t(bcx, bcy+24, 14, bpc, "middle", bps)
	// автономия
	cutoff := numOf("number.deye_sun_30k_battery_low_soc")
	if cutoff <= 0 {
		cutoff = 20
	}
	usable := battCap * (soc - cutoff) / 100
	netKW := (load*1000 - pvtot) / 1000
	s.t(174, 740, 12, cSub, "middle", fmt.Sprintf("ёмкость %.0f кВт·ч · отключение %.0f%%", battCap, cutoff))
	if netKW <= 0.05 {
		s.t(174, 776, 17, cGrn, "middle", "заряд / избыток")
	} else {
		h := usable / netKW
		if h < 0 {
			h = 0
		}
		s.t(174, 770, 13, cSub, "middle", "автономно (без ген.):")
		s.t(174, 798, 22, cTxt, "middle", fmt.Sprintf("≈ %dч %02dм", int(h), int((h-math.Floor(h))*60)))
	}
	s.t(174, station(), 10, cSub, "middle", "* грубо; учёт погоды/генерации — далее")

	// Солнечные поля
	s.box(360, 520, 560, 400)
	s.head(360, 520, 560, "sun", "Солнечные поля", "")
	gx := []float64{460, 640, 820}
	for i := 0; i < 3; i++ {
		pw := numOf(fmt.Sprintf("sensor.deye_sun_30k_pv%d_power", i+1))
		s.gauge(gx[i], 660, 64, pw/1000, 10, []band{{10, cAmb}}, kw(pw), pvLabels[i])
	}
	s.t(380, 800, 12, cSub, "start", "Всего")
	s.bar(380, 815, 520, 46, pvtot/1000, pvMax, []band{{pvT1, cAmb}, {pvT2, cGrn}, {pvT3, cOrg}, {pvMax, cRed}}, kw(pvtot))

	// Генератор
	s.box(956, 520, 464, 400)
	gk := "gen"
	gtc, gtxt := cGry, "выключен"
	if genRun {
		gk, gtc, gtxt = "genrun", cGrn, "работает"
	}
	s.head(956, 520, 464, gk, "Генератор", gtc)
	gl := func(n int, label, val, col string) {
		s.t(972, 576+float64(n)*28, 12, cSub, "start", label)
		s.t(1200, 576+float64(n)*28, 13, col, "end", val)
	}
	gl(0, "Состояние", gtxt, gtc)
	sig := stateOf("sensor.sim_gen_start_signal") == "on"
	gl(1, "Сигнал на запуск", map[bool]string{true: "ЕСТЬ", false: "нет"}[sig], map[bool]string{true: cOrg, false: cSub}[sig])
	htOn := stateOf("sensor.sim_gen_coolant_heater") == "on"
	gl(2, "Подогрев", map[bool]string{true: "вкл", false: "выкл"}[htOn], map[bool]string{true: cOrg, false: cSub}[htOn])
	gl(3, "Температура", fmt.Sprintf("%d°C", iv("sensor.sim_gen_coolant_temp")), cTxt)
	tts := numOf("sensor.sim_gen_time_to_start_min")
	ttsTxt, ttsCol := "—", cSub
	if sig && !genRun {
		ttsTxt, ttsCol = fmt.Sprintf("%.0f мин", tts), cOrg
	}
	gl(4, "До запуска (прогрев)", ttsTxt, ttsCol)
	oil := numOf("sensor.sim_gen_oil_remaining_h")
	oc := cSub
	if oil < 10 {
		oc = cRed
	}
	gl(5, "До замены масла", fmt.Sprintf("%.0f ч", oil), oc)
	gl(6, "Наработка", fmt.Sprintf("%.1f ч", numOf("sensor.sim_gen_runtime_h")), cTxt)
	s.t(972, 800, 11, cSub, "start", "фаза      U          нагрузка")
	for ph := 1; ph <= 3; ph++ {
		y := 800.0 + float64(ph)*28
		p := fmt.Sprintf("sensor.sim_gen_l%d", ph)
		a := numOf(p + "_load")
		s.t(972, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(1040, y, 13, cTxt, "start", fmt.Sprintf("%dВ", iv(p+"_v")))
		s.t(1200, y, 13, cTxt, "end", fmt.Sprintf("%.0fА · %.2fкВт", a, a*numOf(p+"_v")/1000))
	}

	s.p(`</svg>`)
	return s.b.String()
}

func station() float64 { return 822 }

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
	os.WriteFile(wwwDir+"/energy_schema.html", []byte(h), 0644)
}

func loadOptions() {
	b, err := os.ReadFile("/data/options.json")
	if err != nil {
		return
	}
	var o struct {
		RefreshSeconds int     `json:"refresh_seconds"`
		HaURL          string  `json:"ha_url"`
		HaToken        string  `json:"ha_token"`
		Title          string  `json:"title"`
		In1Name        string  `json:"in1_name"`
		In2Name        string  `json:"in2_name"`
		Pv1            string  `json:"pv1_label"`
		Pv2            string  `json:"pv2_label"`
		Pv3            string  `json:"pv3_label"`
		Pv4            string  `json:"pv4_label"`
		BattCap        float64 `json:"batt_capacity_kwh"`
		HomeMax        float64 `json:"home_max"`
		HomeT1         float64 `json:"home_t1"`
		HomeT2         float64 `json:"home_t2"`
		HomeT3         float64 `json:"home_t3"`
		PvMax          float64 `json:"pv_max"`
		PvT1           float64 `json:"pv_t1"`
		PvT2           float64 `json:"pv_t2"`
		PvT3           float64 `json:"pv_t3"`
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
	if o.BattCap > 0 {
		battCap = o.BattCap
	}
	if o.HomeMax > 0 {
		homeMax = o.HomeMax
	}
	if o.HomeT1 > 0 {
		homeT1 = o.HomeT1
	}
	if o.HomeT2 > 0 {
		homeT2 = o.HomeT2
	}
	if o.HomeT3 > 0 {
		homeT3 = o.HomeT3
	}
	if o.PvMax > 0 {
		pvMax = o.PvMax
	}
	if o.PvT1 > 0 {
		pvT1 = o.PvT1
	}
	if o.PvT2 > 0 {
		pvT2 = o.PvT2
	}
	if o.PvT3 > 0 {
		pvT3 = o.PvT3
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
	log.Printf("start: tokenlen=%d apiBase=%s", len(token), apiBase)
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
	log.Println("energy-schema add-on on", listen)
	log.Fatal(http.ListenAndServe(listen, nil))
}
