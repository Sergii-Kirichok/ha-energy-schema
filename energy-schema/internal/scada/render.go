package scada

import (
	"fmt"
	"math"

	"energy-schema/internal/config"
)

// State is the read-only view of HA entity states the renderer needs.
// *hass.Store satisfies it.
type State interface {
	State(entity string) string
	Num(entity string) float64
	Int(entity string) int
	On(entity string) bool
}

// phCol returns a phase color: red if off, orange if voltage out of [lo,hi],
// green otherwise.
func phCol(st State, onE, vE string, lo, hi float64) string {
	if !st.On(onE) {
		return cRed
	}
	v := st.Num(vE)
	if v < lo || v > hi {
		return cOrg
	}
	return cGrn
}

// rybLineState aggregates the three Рыбхоз phases: off / bad (partial) / on.
func rybLineState(st State) string {
	ons := 0
	for ph := 1; ph <= 3; ph++ {
		if st.On(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) {
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

// greenLineState aggregates the three Зелёный phases by presence + voltage.
func greenLineState(st State) string {
	ons, withV := 0, 0
	for ph := 1; ph <= 3; ph++ {
		if st.On(fmt.Sprintf("sensor.sim_green_l%d_on", ph)) {
			ons++
			if st.Num(fmt.Sprintf("sensor.sim_green_l%d_v", ph)) > 50 {
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

// Render builds the full SVG single-line diagram from the current state snapshot.
func Render(st State, cfg config.Config) string {
	s := &Builder{}
	s.p(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 960" font-family="Arial,Helvetica,sans-serif">`)
	s.p(`<rect x="0" y="0" width="1440" height="960" fill="#0f1115"/>`)
	s.t(1428, 28, 18, cTxt, "end", cfg.Title)

	cont := st.State("sensor.sim_contactor")
	gridIn := st.State("sensor.sim_inv_grid") == "on"
	avrPos := st.State("sensor.sim_avr_pos")
	genRun := st.State("sensor.sim_gen_state") == "running"
	rybSt := rybLineState(st)
	grnSt := greenLineState(st)
	exporting := st.State("sensor.sim_export") == "on" && grnSt == "on"
	load := st.Num("sensor.deye_sun_30k_load_power") / 1000
	pvtot := st.Num("sensor.deye_sun_30k_pv1_power") + st.Num("sensor.deye_sun_30k_pv2_power") + st.Num("sensor.deye_sun_30k_pv3_power")
	bp := st.Num("sensor.deye_sun_30k_battery_voltage") * st.Num("sensor.deye_sun_30k_battery_current")

	// ===== FLOWS (under boxes) =====
	stOn := map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}
	rc := stOn[rybSt]
	// Рыбхоз L1->Стаб1 (прямо в левый бок), L2/L3 через верхний зазор
	s.flow(rc, rybSt, 2, false, 264, 108, 340, 108)
	s.flow(rc, rybSt, 2, false, 264, 144, 310, 144, 310, 30, 655, 30, 655, 44)
	s.flow(rc, rybSt, 2, false, 264, 180, 284, 180, 284, 20, 875, 20, 875, 44)
	// выходы 3 стабилизаторов -> общая шина (y=290) -> Контактор и АВР(резерв)
	s.flow(cGrn, rybSt, 3, false, 435, 219, 435, 290)
	s.flow(cGrn, rybSt, 3, false, 655, 219, 655, 290)
	s.flow(cGrn, rybSt, 3, false, 875, 219, 875, 290)
	s.poly(stOn[rybSt], 3, "", 435, 290, 875, 290)
	s.flow(cGrn, rybSt, 3, false, 435, 290, 435, 314, 119, 314, 119, 300)
	s.flow(cGrn, map[bool]string{true: rybSt, false: "off"}[avrPos == "reserve"], 3, false, 875, 290, 905, 290, 905, 300)
	// Ввод2 -> Контактор
	s.flow(cBlu, grnSt, 2, exporting, 1000, 150, 1000, 270, 95, 270, 95, 300)
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
	s.box(24, 44, 240, 175)
	s.head(24, 44, 240, "fish", cfg.In1Name, stOn[rybSt])
	for ph := 1; ph <= 3; ph++ {
		y := 108.0 + float64(ph-1)*36
		onE := fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)
		vE := fmt.Sprintf("sensor.sim_ryb_l%d_vin", ph)
		aE := fmt.Sprintf("sensor.sim_ryb_l%d_load", ph)
		s.dot(44, y-5, 5, phCol(st, onE, vE, 200, 250))
		s.t(60, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if st.On(onE) {
			v := st.Num(vE)
			a := st.Num(aE)
			s.t(252, y, 13, cTxt, "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else {
			s.t(252, y, 12, cRed, "end", "обрыв")
		}
	}
	// Стабилизаторы
	stabX := []float64{340, 560, 780}
	for i := 0; i < 3; i++ {
		ph := i + 1
		x := stabX[i]
		p := fmt.Sprintf("sensor.sim_ryb_l%d", ph)
		linkCol := cGrn
		if st.State(p+"_link") != "ok" {
			linkCol = cRed
		}
		s.box(x, 44, 190, 175)
		s.head(x, 44, 190, "sine", fmt.Sprintf("Стаб L%d", ph), linkCol)
		mc, mt := cBlu, "стабилизация"
		if st.State(p+"_mode") == "transit" {
			mc, mt = cSub, "транзит"
		}
		s.t(x+95, 100, 12, mc, "middle", mt)
		loadA := st.Num(p + "_load")
		row := func(n int, label, val string) {
			s.t(x+14, 124+float64(n)*22, 11, cSub, "start", label)
			s.t(x+176, 124+float64(n)*22, 12, cTxt, "end", val)
		}
		row(0, "вход → выход", fmt.Sprintf("%d → %dВ", st.Int(p+"_vin"), st.Int(p+"_vout")))
		row(1, "ступень", fmt.Sprintf("%d", st.Int(p+"_step")))
		row(2, "нагрузка", fmt.Sprintf("%.0fА · %.2fкВт", loadA, loadA*st.Num(p+"_vout")/1000))
		row(3, "U мин/макс", fmt.Sprintf("%d / %dВ", st.Int(p+"_vmin"), st.Int(p+"_vmax")))
		if !st.On(p + "_on") {
			s.t(x+95, 210, 11, cRed, "middle", "линия отключена")
		}
	}
	// Ввод2 Зелёный
	s.box(1000, 44, 180, 160)
	s.head(1000, 44, 180, "regen", cfg.In2Name, map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}[grnSt])
	dt, dc := "потребление", cBlu
	if st.State("sensor.sim_green_dir") == "export" {
		dt, dc = "отдача ↑", cGrn
	}
	s.t(1012, 86, 12, dc, "start", dt)
	for ph := 1; ph <= 3; ph++ {
		y := 104.0 + float64(ph-1)*26
		c := phCol(st, fmt.Sprintf("sensor.sim_green_l%d_on", ph), fmt.Sprintf("sensor.sim_green_l%d_v", ph), 200, 250)
		s.dot(1016, y-4, 5, c)
		s.t(1030, y, 12, cTxt, "start", fmt.Sprintf("L%d", ph))
		if st.On(fmt.Sprintf("sensor.sim_green_l%d_on", ph)) {
			s.t(1058, y, 12, cTxt, "start", fmt.Sprintf("%dВ", st.Int(fmt.Sprintf("sensor.sim_green_l%d_v", ph))))
			s.t(1168, y, 12, cTxt, "end", fmt.Sprintf("%.0fА", st.Num(fmt.Sprintf("sensor.sim_green_l%d_a", ph))))
		} else {
			s.t(1058, y, 11, cGry, "start", "— нет —")
		}
	}

	// ===================== ROW 2 =====================
	s.box(24, 300, 190, 160)
	s.head(24, 300, 190, "sw", "Контактор", "")
	act, ac := "выкл", cGry
	if cont == "rybhoz" {
		act, ac = "→ "+cfg.In1Name, cGrn
	} else if cont == "green" {
		act, ac = "→ "+cfg.In2Name, cBlu
	}
	s.t(119, 380, 15, ac, "middle", act)
	if st.State("sensor.sim_export") == "on" {
		s.t(119, 410, 11, cGrn, "middle", "отдача → "+cfg.In2Name+" ↑")
	} else {
		s.t(119, 410, 11, cSub, "middle", "отдача выкл")
	}

	df := st.State("sensor.deye_sun_30k_device_fault")
	da := st.State("sensor.deye_sun_30k_device_alarm")
	invState := st.State("sensor.deye_sun_30k_device_state")
	invProb := (invState != "" && invState != "Normal") || (df != "" && df != "OK") || (da != "" && da != "OK")
	s.box(400, 300, 230, 160)
	hc := map[bool]string{true: cGrn, false: cGry}[genRun || gridIn]
	if invProb {
		hc = cRed
	}
	s.head(400, 300, 230, "inv", "Инвертор", hc)
	if invProb {
		s.p(`<polygon points="%g,%g %g,%g %g,%g" fill="none" stroke="%s" stroke-width="2"/><text x="%g" y="%g" font-size="13" font-weight="bold" fill="%s" text-anchor="middle">!</text>`, 600.0, 314.0, 590.0, 332.0, 610.0, 332.0, cRed, 600.0, 330.0, cRed)
		s.t(515, 352, 13, cRed, "middle", "Ошибка: "+invState)
	} else {
		s.t(515, 352, 12, cGrn, "middle", "Статус: норма")
	}
	if gridIn {
		s.t(515, 402, 13, cGrn, "middle", "берёт от сети ✓")
	} else {
		s.t(515, 402, 13, cOrg, "middle", "от сети НЕ берёт ✕")
	}

	s.box(800, 300, 200, 160)
	avrLinkCol := cGrn
	if st.State("sensor.sim_avr_link") != "ok" {
		avrLinkCol = cRed
	}
	s.head(800, 300, 200, "sw", "АВР", avrLinkCol)
	s.t(812, 352, 10, cSub, "start", "вход: инвертор")
	s.t(812, 368, 10, cSub, "start", "резерв: "+cfg.In1Name)
	s.t(988, 360, 10, cSub, "end", "выход: Дом")
	if avrPos == "inverter" {
		s.t(900, 410, 14, cGrn, "middle", "→ инвертор")
	} else {
		s.t(900, 410, 14, cOrg, "middle", "→ резерв")
	}

	// Дом — гейдж
	s.box(1140, 290, 280, 190)
	s.head(1140, 290, 280, "home", "Дом", "")
	s.gauge(1280, 410, 78, load, cfg.HomeMax, []band{{cfg.HomeT1, cGrn}, {cfg.HomeT2, cAmb}, {cfg.HomeT3, cOrg}, {cfg.HomeMax, cRed}}, kw(load*1000), "потребление")

	// ===================== ROW 3 =====================
	// Батарея
	s.box(24, 520, 300, 400)
	bAlarm := st.On("binary_sensor.deye_sun_30k_battery_fault") || st.On("binary_sensor.deye_sun_30k_battery_alarm")
	bStatCol := cGrn
	if bAlarm {
		bStatCol = cRed
	}
	s.head(24, 520, 300, "batt", "Батарея", bStatCol)
	soc := st.Num("sensor.deye_sun_30k_battery")
	bcx, bcy := 174.0, 626.0
	s.arc(bcx, bcy, 80, 180, 0, "#23272f", 13)
	socCol := cGrn
	if soc < 20 {
		socCol = cRed
	} else if soc < 50 {
		socCol = cAmb
	}
	s.arc(bcx, bcy, 80, 180, gAng(soc, 100), socCol, 13)
	s.marker(bcx, bcy, 80, gAng(soc, 100), 7)
	s.t(bcx, bcy-2, 26, cTxt, "middle", fmt.Sprintf("%.0f%%", soc))
	bps, bpc := "ожидание", cSub
	if bp < -20 {
		bps, bpc = "заряд "+kw(-bp), cGrn
	} else if bp > 20 {
		bps, bpc = "разряд "+kw(bp), cOrg
	}
	s.t(bcx, bcy+22, 13, bpc, "middle", bps)
	brow := func(n int, label, val, col string) {
		s.t(40, 686+float64(n)*24, 12, cSub, "start", label)
		s.t(308, 686+float64(n)*24, 13, col, "end", val)
	}
	bstT := map[string]string{"charging": "заряд", "discharging": "разряд", "static": "ожидание", "standby": "ожидание", "full": "полна", "sleep": "сон"}[st.State("sensor.deye_sun_30k_battery_state")]
	if bstT == "" {
		bstT = st.State("sensor.deye_sun_30k_battery_state")
	}
	scol := cTxt
	if bAlarm {
		bstT, scol = "АВАРИЯ", cRed
	}
	brow(0, "Статус", bstT, scol)
	brow(1, "Температура", fmt.Sprintf("%d °C", st.Int("sensor.deye_sun_30k_battery_temperature")), cTxt)
	brow(2, "Ток", fmt.Sprintf("%.1f А", st.Num("sensor.deye_sun_30k_battery_current")), cTxt)
	brow(3, "SOH", fmt.Sprintf("%.1f %%", st.Num("sensor.deye_sun_30k_battery_soh")), cTxt)
	// автономия
	cutoff := st.Num("number.deye_sun_30k_battery_shutdown_soc")
	if cutoff <= 0 {
		cutoff = st.Num("number.deye_sun_30k_battery_low_soc")
	}
	if cutoff <= 0 {
		cutoff = 15
	}
	usable := cfg.BattCap * (soc - cutoff) / 100
	netKW := (load*1000 - pvtot) / 1000
	s.t(174, 806, 12, cSub, "middle", "автономно (без ген.):")
	if netKW <= 0.05 {
		s.t(174, 834, 19, cGrn, "middle", "заряд / избыток")
	} else {
		h := usable / netKW
		if h < 0 {
			h = 0
		}
		s.t(174, 836, 22, cTxt, "middle", fmt.Sprintf("≈ %dч %02dм", int(h), int((h-math.Floor(h))*60)))
	}
	s.t(174, 864, 11, cSub, "middle", fmt.Sprintf("ёмкость %.0f кВт·ч · отключение %.0f%%", cfg.BattCap, cutoff))
	s.t(174, 884, 10, cSub, "middle", "* грубо; погода/генерация — далее")

	// Солнечные поля
	s.box(360, 520, 560, 400)
	s.head(360, 520, 560, "sun", "Солнечные поля", "")
	s.t(906, 547, 14, cAmb, "end", fmt.Sprintf("сегодня: %.0f кВт·ч", st.Num("sensor.deye_sun_30k_today_production")))
	gx := []float64{500, 650, 800}
	for i := 0; i < 3; i++ {
		pw := st.Num(fmt.Sprintf("sensor.deye_sun_30k_pv%d_power", i+1))
		s.gauge(gx[i], 652, 58, pw/1000, 8, []band{{3, cAmb}, {6, cGrn}, {8, cRed}}, kw(pw), cfg.PVLabels[i])
		vv := st.Num(fmt.Sprintf("sensor.deye_sun_30k_pv%d_voltage", i+1))
		aa := st.Num(fmt.Sprintf("sensor.deye_sun_30k_pv%d_current", i+1))
		s.t(gx[i], 692, 12, cSub, "middle", fmt.Sprintf("%.0fВ · %.1fА", vv, aa))
	}
	s.t(380, 802, 12, cSub, "start", "Всего")
	s.bar(380, 816, 520, 46, pvtot/1000, cfg.PVMax, []band{{cfg.PVT1, cAmb}, {cfg.PVT2, cGrn}, {cfg.PVT3, cOrg}, {cfg.PVMax, cRed}}, kw(pvtot))

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
	sig := st.State("sensor.sim_gen_start_signal") == "on"
	gl(1, "Сигнал на запуск", map[bool]string{true: "ЕСТЬ", false: "нет"}[sig], map[bool]string{true: cOrg, false: cSub}[sig])
	htOn := st.State("sensor.sim_gen_coolant_heater") == "on"
	gl(2, "Подогрев", map[bool]string{true: "вкл", false: "выкл"}[htOn], map[bool]string{true: cOrg, false: cSub}[htOn])
	gl(3, "Температура", fmt.Sprintf("%d°C", st.Int("sensor.sim_gen_coolant_temp")), cTxt)
	tts := st.Num("sensor.sim_gen_time_to_start_min")
	ttsTxt, ttsCol := "—", cSub
	if sig && !genRun {
		ttsTxt, ttsCol = fmt.Sprintf("%.0f мин", tts), cOrg
	}
	gl(4, "До запуска (прогрев)", ttsTxt, ttsCol)
	oil := st.Num("sensor.sim_gen_oil_remaining_h")
	oc := cSub
	if oil < 10 {
		oc = cRed
	}
	gl(5, "До замены масла", fmt.Sprintf("%.0f ч", oil), oc)
	gl(6, "Наработка", fmt.Sprintf("%.1f ч", st.Num("sensor.sim_gen_runtime_h")), cTxt)
	s.t(972, 800, 11, cSub, "start", "фаза      U          нагрузка")
	for ph := 1; ph <= 3; ph++ {
		y := 800.0 + float64(ph)*28
		p := fmt.Sprintf("sensor.sim_gen_l%d", ph)
		a := st.Num(p + "_load")
		s.t(972, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(1040, y, 13, cTxt, "start", fmt.Sprintf("%dВ", st.Int(p+"_v")))
		s.t(1200, y, 13, cTxt, "end", fmt.Sprintf("%.0fА · %.2fкВт", a, a*st.Num(p+"_v")/1000))
	}

	s.p(`</svg>`)
	return s.String()
}
