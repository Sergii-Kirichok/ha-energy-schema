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

// Home Assistant Supervisor proxy to Core API.
const listen = ":8099"
const wwwDir = "/homeassistant/www"

var apiBase = "http://supervisor/core/api"
var token string
var title = "Энергосистема"

var (
	smu     sync.Mutex
	states  = map[string]string{}
	lossBuf []float64
	refresh = 3
)

// Entities the renderer reads (sim_* from emulator + real Deye sensors).
var wanted = func() map[string]bool {
	m := map[string]bool{
		"sensor.sim_contactor": true, "sensor.sim_gen_state": true,
		"sensor.sim_gen_runtime_h": true, "sensor.sim_gen_oil_remaining_h": true,
		"sensor.sim_gen_coolant_heater": true, "sensor.sim_home_bypass_kw": true,
		"sensor.deye_sun_30k_pv_power": true, "sensor.deye_sun_30k_battery": true,
		"sensor.deye_sun_30k_battery_power": true, "sensor.deye_sun_30k_battery_voltage": true,
		"sensor.deye_sun_30k_load_power": true, "sensor.deye_sun_30k_power_losses": true,
	}
	for ph := 1; ph <= 3; ph++ {
		for _, s := range []string{"on", "vin", "vout", "step", "load", "peak"} {
			m[fmt.Sprintf("sensor.sim_ryb_l%d_%s", ph, s)] = true
		}
		for _, s := range []string{"on", "v", "a"} {
			m[fmt.Sprintf("sensor.sim_green_l%d_%s", ph, s)] = true
		}
		for _, s := range []string{"v", "load"} {
			m[fmt.Sprintf("sensor.sim_gen_l%d_%s", ph, s)] = true
		}
	}
	return m
}()

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
		log.Printf("decode err=%v status=%d tokenlen=%d body=%.140q", derr, resp.StatusCode, len(token), string(body))
		return
	}
	m := make(map[string]string)
	for _, e := range arr {
		if wanted[e.EntityID] {
			m[e.EntityID] = e.State
		}
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
func w0(w float64) string { return fmt.Sprintf("%.0f Вт", w) }

func rybState() (string, bool) {
	ons, oor := 0, false
	for ph := 1; ph <= 3; ph++ {
		if stateOf(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) == "on" {
			ons++
			vo := numOf(fmt.Sprintf("sensor.sim_ryb_l%d_vout", ph))
			if vo < 205 || vo > 245 {
				oor = true
			}
		}
	}
	if ons == 0 {
		return cGry, true
	}
	if ons < 3 || oor {
		return cOrg, false
	}
	return cGrn, false
}
func greenState() (string, bool) {
	ons, withV, miss := 0, 0, false
	for ph := 1; ph <= 3; ph++ {
		if stateOf(fmt.Sprintf("sensor.sim_green_l%d_on", ph)) == "on" {
			ons++
			if numOf(fmt.Sprintf("sensor.sim_green_l%d_v", ph)) > 50 {
				withV++
			} else {
				miss = true
			}
		}
	}
	if ons == 0 {
		return cGry, true
	}
	if withV == 0 {
		return cRed, true
	}
	if ons < 3 || miss {
		return cOrg, false
	}
	return cGrn, false
}

type SB struct{ b strings.Builder }

func (s *SB) p(f string, a ...interface{}) { s.b.WriteString(fmt.Sprintf(f, a...)) }
func (s *SB) box(x, y, w, h float64) {
	s.p(`<rect x="%g" y="%g" width="%g" height="%g" rx="10" fill="%s" stroke="%s" stroke-width="1.5"/>`, x, y, w, h, cBox, cBrd)
}
func (s *SB) t(x, y, sz float64, col, anchor, str string) {
	s.p(`<text x="%g" y="%g" font-size="%g" fill="%s" text-anchor="%s">%s</text>`, x, y, sz, col, anchor, html.EscapeString(str))
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

func renderSVG() string {
	s := &SB{}
	s.p(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1180 680" font-family="Arial,Helvetica,sans-serif">`)
	s.p(`<rect x="0" y="0" width="1180" height="680" fill="#0f1115"/>`)
	s.t(1160, 28, 18, cTxt, "end", title)
	rybCol, rybDash := rybState()
	grnCol, grnDash := greenState()
	cont := stateOf("sensor.sim_contactor")
	genRun := stateOf("sensor.sim_gen_state") == "running"
	rd := ""
	if rybDash {
		rd = "7 5"
	}
	s.poly(rybCol, 3, rd, 390, 130, 418, 130, 418, 178, 448, 178)
	gd := ""
	if grnDash {
		gd = "7 5"
	}
	s.poly(grnCol, 3, gd, 390, 300, 418, 300, 418, 208, 448, 208)
	contCol := cGry
	if cont == "rybhoz" {
		contCol = rybCol
	} else if cont == "green" {
		contCol = grnCol
	}
	s.poly(contCol, 3.5, "", 568, 193, 600, 193, 600, 340, 640, 340)
	s.poly(cAmb, 3, "", 720, 128, 720, 292)
	genCol := cGry
	if genRun {
		genCol = cGrn
	}
	s.poly(genCol, 3, "", 390, 500, 615, 500, 615, 415, 640, 415)
	s.poly(cOrg, 2, "6 4", 390, 545, 605, 545, 605, 398, 640, 398)
	s.poly(cPur, 3, "", 740, 442, 740, 512)
	s.poly(cBlu, 3.5, "", 840, 360, 905, 360)

	s.box(20, 18, 370, 214)
	s.t(34, 40, 15, cTxt, "start", "Ввод №1 — Рыбхоз")
	s.p(`<circle cx="372" cy="34" r="7" fill="%s"/>`, rybCol)
	cols := []float64{42, 110, 170, 222, 268, 330}
	for i, h := range []string{"L", "Вход", "Выход", "Ст", "Ток", "Пик"} {
		s.t(cols[i], 64, 11, cSub, "start", h)
	}
	for ph := 1; ph <= 3; ph++ {
		y := 64.0 + float64(ph)*38
		p := fmt.Sprintf("sensor.sim_ryb_l%d", ph)
		on := stateOf(p+"_on") == "on"
		if on {
			s.t(cols[0], y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
			s.t(cols[1], y, 13, cSub, "start", fmt.Sprintf("%dV", iv(p+"_vin")))
			s.t(cols[2], y, 13, cTxt, "start", fmt.Sprintf("%dV", iv(p+"_vout")))
			s.t(cols[3], y, 13, cBlu, "start", fmt.Sprintf("%+d", iv(p+"_step")))
			s.t(cols[4], y, 13, cTxt, "start", fmt.Sprintf("%.0fA", numOf(p+"_load")))
			s.t(cols[5], y, 13, cSub, "start", fmt.Sprintf("%.0fA", numOf(p+"_peak")))
		} else {
			s.t(cols[0], y, 13, cRed, "start", fmt.Sprintf("L%d", ph))
			s.t(cols[1], y, 12, cRed, "start", "обрыв")
		}
	}

	s.box(20, 242, 370, 118)
	s.t(34, 264, 15, cTxt, "start", "Ввод №2 — Зелёный тариф")
	s.p(`<circle cx="372" cy="258" r="7" fill="%s"/>`, grnCol)
	gc := []float64{42, 150, 260}
	for i, h := range []string{"L", "Напряж.", "Ток"} {
		s.t(gc[i], 288, 11, cSub, "start", h)
	}
	for ph := 1; ph <= 3; ph++ {
		y := 288.0 + float64(ph)*22
		p := fmt.Sprintf("sensor.sim_green_l%d", ph)
		if stateOf(p+"_on") == "on" {
			s.t(gc[0], y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
			s.t(gc[1], y, 13, cTxt, "start", fmt.Sprintf("%dV", iv(p+"_v")))
			s.t(gc[2], y, 13, cTxt, "start", fmt.Sprintf("%.0fA", numOf(p+"_a")))
		} else {
			s.t(gc[0], y, 13, cGry, "start", fmt.Sprintf("L%d", ph))
			s.t(gc[1], y, 12, cGry, "start", "— нет —")
		}
	}

	s.box(448, 153, 120, 80)
	s.t(508, 178, 13, cTxt, "middle", "Контактор")
	act, ac := "выкл", cGry
	if cont == "rybhoz" {
		act, ac = "→ Рыбхоз", rybCol
	} else if cont == "green" {
		act, ac = "→ Зелёный", grnCol
	}
	s.t(508, 205, 13, ac, "middle", act)

	s.p(`<circle cx="720" cy="78" r="48" fill="%s" stroke="%s" stroke-width="2"/>`, cBox, cAmb)
	s.t(720, 66, 12, cSub, "middle", "Солнце")
	s.t(720, 92, 15, cAmb, "middle", kw(numOf("sensor.deye_sun_30k_pv_power")))

	s.box(640, 292, 200, 150)
	s.t(740, 322, 16, cTxt, "middle", "ИНВЕРТОР")
	s.t(740, 360, 12, cSub, "middle", "собств. потребление")
	s.t(740, 384, 15, cSub, "middle", fmt.Sprintf("%s · ср.30с", w0(avgLoss())))

	soc := numOf("sensor.deye_sun_30k_battery")
	bp := numOf("sensor.deye_sun_30k_battery_power")
	bcx, bcy, br := 720.0, 600.0, 72.0
	s.arc(bcx, bcy, br, 180, 0, "#2b2f38", 12)
	socCol := cGrn
	if soc < 20 {
		socCol = cRed
	} else if soc < 50 {
		socCol = cAmb
	}
	s.arc(bcx, bcy, br, 180, 180-soc*1.8, socCol, 12)
	s.t(bcx, bcy-8, 22, cTxt, "middle", fmt.Sprintf("%.0f%%", soc))
	bpc, bps := cPur, kw(bp)
	if bp < 0 {
		bps, bpc = "заряд "+kw(-bp), cGrn
	} else if bp > 0 {
		bps = "разряд " + kw(bp)
	}
	s.t(bcx, bcy+16, 12, bpc, "middle", bps)
	s.t(bcx, bcy+34, 12, cSub, "middle", "АКБ")

	s.box(20, 392, 370, 268)
	gtc, gtxt := cGry, "выключен"
	if genRun {
		gtc, gtxt = cGrn, "работает"
	}
	s.t(34, 416, 15, cTxt, "start", "Генератор")
	s.p(`<circle cx="372" cy="410" r="7" fill="%s"/>`, gtc)
	s.t(34, 440, 12, gtc, "start", "Состояние: "+gtxt)
	s.t(34, 462, 12, cSub, "start", fmt.Sprintf("Наработка: %.1f ч", numOf("sensor.sim_gen_runtime_h")))
	oil, oc := numOf("sensor.sim_gen_oil_remaining_h"), cSub
	if oil < 10 {
		oc = cRed
	}
	s.t(34, 484, 12, oc, "start", fmt.Sprintf("До замены масла: %.0f ч", oil))
	hc, ht := cGry, "выкл"
	if stateOf("sensor.sim_gen_coolant_heater") == "on" {
		hc, ht = cOrg, "вкл"
	}
	s.t(34, 506, 12, hc, "start", "Подогрев ОЖ: "+ht)
	pc := []float64{42, 150, 260}
	for i, h := range []string{"L", "Напряж.", "Нагрузка"} {
		s.t(pc[i], 540, 11, cSub, "start", h)
	}
	for ph := 1; ph <= 3; ph++ {
		y := 540.0 + float64(ph)*30
		p := fmt.Sprintf("sensor.sim_gen_l%d", ph)
		s.t(pc[0], y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(pc[1], y, 13, cTxt, "start", fmt.Sprintf("%dV", iv(p+"_v")))
		s.t(pc[2], y, 13, cTxt, "start", fmt.Sprintf("%.0fA", numOf(p+"_load")))
	}
	s.t(300, 500, 11, cGrn, "middle", "ген.")
	s.t(300, 547, 11, cOrg, "middle", "упр.")

	s.box(905, 300, 240, 150)
	s.t(1025, 330, 16, cTxt, "middle", "ДОМ")
	s.t(1025, 366, 12, cSub, "middle", "через инвертор (ИБП)")
	s.t(1025, 390, 15, cBlu, "middle", kw(numOf("sensor.deye_sun_30k_load_power")))
	s.t(1025, 418, 12, cSub, "middle", "мимо инвертора")
	s.t(1025, 440, 14, cTxt, "middle", fmt.Sprintf("%.2f кВт", numOf("sensor.sim_home_bypass_kw")))

	s.p(`</svg>`)
	return s.b.String()
}

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
	if o.HaToken != "" {
		token = o.HaToken
		u := o.HaURL
		if u == "" {
			u = "http://homeassistant:8123"
		}
		apiBase = strings.TrimRight(u, "/") + "/api"
	}
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
