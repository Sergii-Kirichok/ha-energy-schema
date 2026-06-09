package scada

import (
	"fmt"
	"math"
	"strings"

	"energy-schema/internal/config"
)

// State is the read-only view of HA entity states the renderer needs.
// *hass.Store satisfies it.
type State interface {
	State(entity string) string
	Num(entity string) float64
	Int(entity string) int
	On(entity string) bool
	// last-good snapshot + offline duration (for devices that dropped out)
	LastState(entity string) string
	LastNum(entity string) float64
	LastInt(entity string) int
	LostInfo(entity string) string
	// scalar attributes + time-until helpers (weather, sunset)
	Attr(entity, key string) string
	AttrNum(entity, key string) float64
	HoursUntil(entity, key string) float64
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

// rybPhase diagnoses ONE Рыбхоз phase line (independently of the others):
//
//	"on"   — фаза под напряжением (нормальный поток);
//	"lost" — датчик линии молчит, НО инвертор видит напряжение на этой фазе →
//	         линия жива, потеряна связь с датчиком/устройством (оранжевый «?»);
//	"bad"  — линии нет и инвертор фазу не подтверждает → реальный обрыв (красный ✕).
//
// Сверка по инвертору достоверна, только когда контактор кормит инвертор Рыбхозом.
func rybPhase(st State, ph int, contRyb bool) string {
	if st.On(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) {
		return "on"
	}
	if contRyb && st.Num(fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_voltage", ph)) > 150 {
		return "lost"
	}
	return "bad"
}

// weatherRu maps a HA weather condition to a short Russian label.
func weatherRu(c string) string {
	m := map[string]string{
		"sunny": "ясно", "clear-night": "ясно", "partlycloudy": "перем. обл.",
		"cloudy": "пасмурно", "fog": "туман", "rainy": "дождь", "pouring": "ливень",
		"hail": "град", "lightning": "гроза", "lightning-rainy": "гроза, дождь",
		"snowy": "снег", "snowy-rainy": "снег, дождь", "windy": "ветрено",
		"windy-variant": "ветрено", "exceptional": "экстрим",
	}
	if v, ok := m[c]; ok {
		return v
	}
	return c
}

// stabOut is the state of stabilizer ph's output line: a real input break just
// de-energizes the output ("off"), it isn't a fault on the output side.
func stabOut(st State, ph int, contRyb bool) string {
	switch rybPhase(st, ph, contRyb) {
	case "bad":
		return "off"
	case "lost":
		return "lost"
	default:
		return "on"
	}
}

// Render builds the full SVG single-line diagram from the current state snapshot.
func Render(st State, cfg config.Config) string {
	s := &Builder{}
	s.p(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 960" font-family="Arial,Helvetica,sans-serif">`)
	s.p(`<rect x="0" y="0" width="1440" height="960" fill="#0f1115"/>`)
	s.t(1428, 28, 18, cTxt, "end", cfg.Title)

	cont := st.State("sensor.sim_contactor")
	gridAvail := st.On("binary_sensor.deye_sun_30k_grid")
	gridBonded := strings.Contains(st.State("sensor.deye_sun_30k_device_relay"), "Grid")
	gridIn := gridBonded
	avrPos := st.State("sensor.sim_avr_pos")
	genRun := st.State("sensor.sim_gen_state") == "running"
	rybSt := rybLineState(st)
	grnSt := greenLineState(st)
	exporting := st.State("sensor.sim_export") == "on" && grnSt == "on"
	load := st.Num("sensor.deye_sun_30k_load_power") / 1000
	pvtot := st.Num("sensor.deye_sun_30k_pv1_power") + st.Num("sensor.deye_sun_30k_pv2_power") + st.Num("sensor.deye_sun_30k_pv3_power")
	bp := st.Num("sensor.deye_sun_30k_battery_voltage") * st.Num("sensor.deye_sun_30k_battery_current")

	// ===== FLOWS (under boxes) =====
	stOn := map[string]string{"on": cGrn, "bad": cOrg, "lost": cOrg, "off": cGry}
	// Контактор — одно реле: ВЫКЛ → Ввод1 Рыбхоз (по автоматам, дефолт); ВКЛ → Ввод2 Зелёный.
	contOn := cont == "on"
	contRyb := !contOn // активный ввод = Рыбхоз, пока контактор выключен
	// Рыбхоз: КАЖДАЯ фаза — своё состояние; обрыв одной не валит остальные.
	// L2/L3 разведены по высоте (верх y=30 и y=14), стояк L2 правее (x=328) —
	// чтобы маркеры аварии («?»/✕) не наезжали на L1 и друг на друга.
	s.flow(cGrn, rybPhase(st, 1, contRyb), 2, false, 264, 108, 340, 108)
	s.flow(cGrn, rybPhase(st, 2, contRyb), 2, false, 264, 144, 328, 144, 328, 30, 655, 30, 655, 44)
	s.flow(cGrn, rybPhase(st, 3, contRyb), 2, false, 264, 180, 284, 180, 284, 14, 875, 14, 875, 44)
	// выходы 3 стабилизаторов -> общая шина (y=275) -> Контактор и АВР(резерв)
	out1, out2, out3 := stabOut(st, 1, contRyb), stabOut(st, 2, contRyb), stabOut(st, 3, contRyb)
	s.flow(cGrn, out1, 3, false, 435, 219, 435, 275)
	s.flow(cGrn, out2, 3, false, 655, 219, 655, 275)
	s.flow(cGrn, out3, 3, false, 875, 219, 875, 275)
	// шина: зелёная ТОЛЬКО если все три выхода в норме; оранжевая если хоть одна
	// фаза в потере связи/байпасе (стабилизация под вопросом); серая если всё off.
	busSt := "off"
	if out1 == "on" && out2 == "on" && out3 == "on" {
		busSt = "on"
	} else if out1 != "off" || out2 != "off" || out3 != "off" {
		busSt = "lost"
	}
	busDash := ""
	if busSt == "lost" {
		busDash = "6 5"
	}
	s.poly(stOn[busSt], 3, busDash, 435, 275, 875, 275)
	s.flow(cGrn, busSt, 3, false, 435, 275, 119, 275, 119, 300)
	s.flow(cGrn, map[bool]string{true: busSt, false: "off"}[avrPos == "reserve"], 3, false, 875, 275, 905, 275, 905, 300)
	// Ввод2 -> Контактор
	s.flow(cBlu, grnSt, 2, exporting, 1020, 150, 1002, 150, 1002, 250, 95, 250, 95, 300)
	// Контактор -> Инвертор (активный ввод всегда питает инвертор)
	cSt := "on"
	if !gridIn {
		cSt = "bad"
	}
	s.flow(cBlu, cSt, 2, false, 214, 380, 400, 380)
	// Инвертор -> АВР (осн.)
	s.flow(cGrn, map[bool]string{true: "on", false: "off"}[avrPos == "inverter"], 4, false, 700, 380, 800, 380)
	// АВР -> Дом
	s.flow(cGrn, "on", 3, false, 1000, 380, 1140, 380)
	// Батарея <-> Инвертор
	// Батарея <-> Инвертор: движение только при заряде/разряде; в покое (idle) — статичная линия
	if math.Abs(bp) > 20 {
		s.flow(cPur, "on", math.Abs(bp)/1000, bp < 0, 174, 520, 174, 488, 470, 488, 470, 460)
	} else {
		s.poly(cPur, 3, "", 174, 520, 174, 488, 470, 488, 470, 460)
	}
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
		ps := rybPhase(st, ph, contRyb)
		dotCol := phCol(st, onE, vE, 200, 250)
		if ps == "lost" {
			dotCol = cOrg
		}
		s.dot(44, y-5, 5, dotCol)
		s.t(60, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if st.On(onE) {
			v := st.Num(vE)
			a := st.Num(aE)
			s.t(252, y, 13, cTxt, "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else if ps == "lost" {
			s.t(252, y, 12, cOrg, "end", "потеря связи")
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
		linkOk := st.State(p+"_link") == "ok"
		linkCol := cGrn
		if !linkOk {
			linkCol = cRed
		}
		s.box(x, 44, 190, 175)
		s.head(x, 44, 190, "sine", fmt.Sprintf("Стаб L%d", ph), linkCol)
		if !linkOk {
			// Стабилизатор офлайн. Линия — ОТДЕЛЬНЫЙ источник: если она под
			// напряжением (датчик линии / инвертор) — стабилизатор просто в обходе
			// (байпас), питание идёт мимо. Иначе питание не подтверждено.
			lineAlive := rybPhase(st, ph, contRyb) != "bad"
			if lineAlive {
				s.t(x+95, 92, 14, cOrg, "middle", "ТРАНЗИТ (байпас)")
				s.t(x+95, 110, 10, cSub, "middle", "стабилизатор офлайн")
				// ТЕКУЩЕЕ напряжение линии (её данные доступны, источник отдельный)
				s.t(x+95, 142, 17, cTxt, "middle", fmt.Sprintf("%d В", st.Int(p+"_vin")))
				s.t(x+95, 160, 10, cSub, "middle", "на линии (живое)")
			} else {
				s.t(x+95, 100, 14, cRed, "middle", "НЕТ СВЯЗИ")
				s.t(x+95, 122, 11, cSub, "middle", "питание не подтверждено")
			}
			// последнее, что отдал САМ стабилизатор (выход/ступень) — до сбоя
			if st.LastState(p+"_vout") != "" {
				s.t(x+95, 188, 10, cSub, "middle", fmt.Sprintf("посл. от стаб.: выход %dВ · ст %d", st.LastInt(p+"_vout"), st.LastInt(p+"_step")))
			}
			if info := st.LostInfo(p + "_vout"); info != "" {
				s.t(x+95, 210, 10, cOrg, "middle", "стаб. молчит уже "+info)
			}
			continue
		}
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
			if rybPhase(st, ph, contRyb) == "lost" {
				s.t(x+95, 210, 11, cOrg, "middle", "потеря связи (датчик)")
			} else {
				s.t(x+95, 210, 11, cRed, "middle", "линия отключена")
			}
		}
	}
	// Ввод2 Зелёный — карточка того же размера, что и Рыбхоз (240×175)
	s.box(1020, 44, 240, 175)
	s.head(1020, 44, 240, "regen", cfg.In2Name, map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}[grnSt])
	// состояние/направление — честно: когда ввод не запитан, не пишем «потребление»
	if grnSt == "off" {
		s.t(1140, 94, 12, cGry, "middle", "ввод отключён")
	} else {
		dt, dc := "потребление", cBlu
		if st.State("sensor.sim_green_dir") == "export" {
			dt, dc = "отдача ↑", cGrn
		}
		s.t(1140, 94, 12, dc, "middle", dt)
	}
	for ph := 1; ph <= 3; ph++ {
		y := 120.0 + float64(ph-1)*32
		onE := fmt.Sprintf("sensor.sim_green_l%d_on", ph)
		vE := fmt.Sprintf("sensor.sim_green_l%d_v", ph)
		aE := fmt.Sprintf("sensor.sim_green_l%d_a", ph)
		s.dot(1040, y-5, 5, phCol(st, onE, vE, 200, 250))
		s.t(1056, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if st.On(onE) {
			v := st.Num(vE)
			a := st.Num(aE)
			s.t(1248, y, 13, cTxt, "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else {
			s.t(1248, y, 12, cGry, "end", "— нет —")
		}
	}

	// ===================== ROW 2 =====================
	// Контактор — одно реле (RS-485), с обратной связью: ВЫКЛ=Ввод1, ВКЛ=Ввод2
	s.box(24, 300, 190, 175)
	ctLink := st.State("sensor.sim_contactor_link") != "lost" // обратная связь RS-485 (по умолч. есть)
	ctDot := cGrn
	if !ctLink {
		ctDot = cRed
	}
	s.head(24, 300, 190, "sw", "Контактор", ctDot)
	// крупный статус: состояние реле → какой ввод в работе
	if !ctLink {
		s.t(119, 348, 13, cRed, "middle", "НЕТ СВЯЗИ (485)")
	} else if contOn {
		s.t(119, 348, 15, cBlu, "middle", "ВКЛ → Ввод 2")
	} else {
		s.t(119, 348, 15, cGrn, "middle", "ВЫКЛ → Ввод 1")
	}
	// какой ввод сейчас активен (подсветка) + индикатор «живости» линии + при каком реле
	selRow := func(y float64, name, note, col, liveCol string, active bool) {
		if active {
			s.p(`<rect x="34" y="%g" width="160" height="26" rx="6" fill="%s" fill-opacity="0.16" stroke="%s" stroke-width="1.5"/>`, y, col, col)
		} else {
			s.p(`<rect x="34" y="%g" width="160" height="26" rx="6" fill="none" stroke="%s" stroke-width="1"/>`, y, cBrd)
		}
		s.dot(50, y+13, 5, liveCol)
		tc, nc := cSub, cSub
		if active {
			tc, nc = col, col
		}
		s.t(66, y+17, 13, tc, "start", name)
		s.t(186, y+17, 9, nc, "end", note)
	}
	selRow(364, cfg.In1Name, "по умолч.", cGrn, stOn[rybSt], !contOn)
	selRow(394, cfg.In2Name, "реле вкл", cBlu, stOn[grnSt], contOn)
	// пояснение защиты: без управляющего питания контактор остаётся на Вводе 1
	s.t(119, 442, 9, cSub, "middle", "перекидной · без питания → Ввод 1 (защита)")
	// низ: связь RS-485 + отдача
	if ctLink {
		s.t(34, 462, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(34, 462, 10, cRed, "start", "RS-485 ✕")
	}
	if st.State("sensor.sim_export") == "on" {
		s.t(204, 462, 10, cGrn, "end", "отдача ↑")
	} else {
		s.t(204, 462, 10, cSub, "end", "отдача —")
	}

	df := st.State("sensor.deye_sun_30k_device_fault")
	da := st.State("sensor.deye_sun_30k_device_alarm")
	invState := st.State("sensor.deye_sun_30k_device_state")
	invProb := (invState != "" && invState != "Normal") || (df != "" && df != "OK") || (da != "" && da != "OK")
	s.box(400, 300, 300, 160)
	hc := map[bool]string{true: cGrn, false: cGry}[genRun || gridIn]
	if invProb {
		hc = cRed
	}
	s.head(400, 300, 300, "inv", "Инвертор", hc)
	// температура инвертора — в шапке, посередине
	temp := st.Num("sensor.deye_sun_30k_temperature")
	tc := cGrn
	if temp >= 65 {
		tc = cRed
	} else if temp >= 50 {
		tc = cOrg
	}
	s.t(560, 327, 13, tc, "middle", fmt.Sprintf("%.1f °C", temp))
	// фактическое использование сети: реле + наличие — СЛЕВА (опущено от шапки)
	gridP := st.Num("sensor.deye_sun_30k_grid_power")
	if !gridAvail {
		s.t(414, 351, 12, cGry, "start", "сеть: нет ✕")
	} else if gridBonded {
		s.t(414, 351, 12, cGrn, "start", fmt.Sprintf("сеть: %.2f кВт ✓", gridP/1000))
	} else {
		s.t(414, 351, 12, cOrg, "start", "сеть: откл. защитой ✕")
	}
	// статус инвертора — СПРАВА
	if invProb {
		s.t(686, 351, 12, cRed, "end", "Ошибка: "+invState)
	} else {
		s.t(686, 351, 12, cGrn, "end", "Статус: норма")
	}
	// напряжение и нагрузка по фазам на входе (сеть)
	s.t(414, 368, 10, cSub, "start", "фаза")
	s.t(536, 368, 10, cSub, "start", "U вход")
	s.t(686, 368, 10, cSub, "end", "нагрузка")
	for ph := 1; ph <= 3; ph++ {
		y := 386.0 + float64(ph-1)*18
		gv := st.Num(fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_voltage", ph))
		gw := st.Num(fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_power", ph))
		vc := cGrn
		if gv < 1 {
			vc = cSub
		} else if gv < 205 || gv > 250 {
			vc = cOrg
		}
		s.t(414, y, 12, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(536, y, 12, vc, "start", fmt.Sprintf("%.0f В", gv))
		s.t(686, y, 12, cTxt, "end", fmt.Sprintf("%.0f Вт", gw))
	}
	// частота сети + интервал реконнекта (после срабатывания защиты)
	s.t(414, 446, 10, cSub, "start", fmt.Sprintf("сеть %.1f Гц · реконнект %.0f с", st.Num("sensor.deye_sun_30k_grid_frequency"), st.Num("number.deye_sun_30k_grid_reconnection_time")))

	// АВР — управление/связь по RS-485; видно, через что сейчас питается Дом
	s.box(800, 300, 200, 175)
	avrLink := st.State("sensor.sim_avr_link") == "ok"
	avrLinkCol := cGrn
	if !avrLink {
		avrLinkCol = cRed
	}
	s.head(800, 300, 200, "sw", "АВР", avrLinkCol)
	// температура в шкафу — у значка статуса
	atemp := st.Num("sensor.sim_avr_temp")
	atc := cGrn
	if atemp >= 45 {
		atc = cRed
	} else if atemp >= 35 {
		atc = cOrg
	}
	s.t(966, 327, 12, atc, "end", fmt.Sprintf("%.0f°C", atemp))
	// режим работы — пилюля (важно: можем ли МЫ им управлять)
	avrMode := st.State("sensor.sim_avr_mode")
	modeCol, modeTxt := cGrn, "АВТО — переключается сам"
	if avrMode == "manual" {
		modeCol, modeTxt = cBlu, "РУЧНОЙ — управляем мы"
	}
	if !avrLink {
		modeCol, modeTxt = cRed, "НЕТ СВЯЗИ (RS-485)"
	}
	s.p(`<rect x="812" y="340" width="176" height="26" rx="13" fill="%s" fill-opacity="0.15" stroke="%s" stroke-width="1.5"/>`, modeCol, modeCol)
	s.t(900, 357, 11, modeCol, "middle", modeTxt)
	// селектор источника: через что сейчас питается Дом (инвертор / резерв = прямой Ввод 1)
	avrRow := func(y float64, name, key, col string) {
		if avrPos == key {
			s.p(`<rect x="812" y="%g" width="176" height="24" rx="6" fill="%s" fill-opacity="0.16" stroke="%s" stroke-width="1.5"/>`, y, col, col)
		} else {
			s.p(`<rect x="812" y="%g" width="176" height="24" rx="6" fill="none" stroke="%s" stroke-width="1"/>`, y, cBrd)
		}
		tc := cSub
		if avrPos == key {
			tc = col
		}
		s.t(822, y+16, 12, tc, "start", name)
		if avrPos == key {
			s.t(980, y+16, 12, col, "end", "→ Дом")
		}
	}
	avrRow(376, "Инвертор", "inverter", cGrn)
	avrRow(404, "Резерв · "+cfg.In1Name, "reserve", cOrg)
	// статистика переключений: всего и за сегодня — отдельно
	s.t(900, 446, 11, cTxt, "middle", fmt.Sprintf("переключений всего: %.0f", st.Num("sensor.sim_avr_switches")))
	s.t(900, 464, 10, cSub, "middle", fmt.Sprintf("из них сегодня: %.0f", st.Num("sensor.sim_avr_switches_today")))

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
	// температура — рядом со значком статуса (не отдельной строкой)
	btemp := st.Num("sensor.deye_sun_30k_battery_temperature")
	btc := cGrn
	if btemp >= 45 {
		btc = cRed
	} else if btemp >= 35 {
		btc = cOrg
	}
	s.t(286, 547, 13, btc, "end", fmt.Sprintf("%.0f°C", btemp))

	soc := st.Num("sensor.deye_sun_30k_battery")
	bcx, bcy := 174.0, 624.0
	s.arc(bcx, bcy, 78, 180, 0, "#23272f", 13)
	socCol := cGrn
	if soc < 20 {
		socCol = cRed
	} else if soc < 50 {
		socCol = cAmb
	}
	s.arc(bcx, bcy, 78, 180, gAng(soc, 100), socCol, 13)
	s.marker(bcx, bcy, 78, gAng(soc, 100), 7)
	s.t(bcx, bcy-2, 28, cTxt, "middle", fmt.Sprintf("%.0f%%", soc))

	// заряд/разряд — визуально (пилюля со стрелкой); покой = без стрелки
	if bAlarm {
		s.p(`<rect x="44" y="648" width="260" height="30" rx="15" fill="%s" fill-opacity="0.18" stroke="%s" stroke-width="1.5"/>`, cRed, cRed)
		s.t(174, 668, 15, cRed, "middle", "⚠ АВАРИЯ БАТАРЕИ")
	} else if bp < -20 {
		s.p(`<rect x="44" y="648" width="260" height="30" rx="15" fill="%s" fill-opacity="0.15" stroke="%s" stroke-width="1.5"/>`, cGrn, cGrn)
		s.p(`<polygon points="70,670 78,654 86,670" fill="%s"/>`, cGrn)
		s.t(190, 668, 15, cGrn, "middle", "ЗАРЯД "+kw(-bp))
	} else if bp > 20 {
		s.p(`<rect x="44" y="648" width="260" height="30" rx="15" fill="%s" fill-opacity="0.15" stroke="%s" stroke-width="1.5"/>`, cOrg, cOrg)
		s.p(`<polygon points="70,654 78,670 86,654" fill="%s"/>`, cOrg)
		s.t(190, 668, 15, cOrg, "middle", "РАЗРЯД "+kw(bp))
	} else {
		s.p(`<rect x="44" y="648" width="260" height="30" rx="15" fill="none" stroke="%s" stroke-width="1"/>`, cBrd)
		s.t(174, 668, 14, cSub, "middle", "ожидание (idle)")
	}

	// сколько кВт·ч реально доступно сейчас (от текущего заряда до отключения)
	cutoff := st.Num("number.deye_sun_30k_battery_shutdown_soc")
	if cutoff <= 0 {
		cutoff = st.Num("number.deye_sun_30k_battery_low_soc")
	}
	if cutoff <= 0 {
		cutoff = 15
	}
	capKWh := st.Num("number.deye_sun_30k_battery_capacity") * st.Num("sensor.deye_sun_30k_battery_voltage") / 1000
	if capKWh < 1 {
		capKWh = cfg.BattCap
	}
	usableKWh := capKWh * (soc - cutoff) / 100
	if usableKWh < 0 {
		usableKWh = 0
	}
	s.t(174, 702, 11, cSub, "middle", "доступно сейчас (до отключения):")
	rcol := cGrn
	if usableKWh < capKWh*0.12 {
		rcol = cOrg
	}
	s.t(174, 730, 24, rcol, "middle", fmt.Sprintf("%.1f кВт·ч", usableKWh))

	// ток + SOH в одну строку
	s.t(40, 762, 12, cSub, "start", "Ток")
	s.t(150, 762, 13, cTxt, "end", fmt.Sprintf("%.1f А", st.Num("sensor.deye_sun_30k_battery_current")))
	s.t(196, 762, 12, cSub, "start", "SOH")
	s.t(308, 762, 13, cTxt, "end", fmt.Sprintf("%.1f%%", st.Num("sensor.deye_sun_30k_battery_soh")))

	// автономия: если свет пропадёт СЕЙЧАС, без генератора. PV помогает до заката,
	// после — разряд по полной нагрузке. Берём текущую генерацию (по погоде).
	loadKW := load
	pvKW := pvtot / 1000
	sunUp := pvKW > 0.1 || st.State("sun.sun") == "above_horizon"
	hSun := 0.0
	if sunUp {
		hSun = st.HoursUntil("sun.sun", "next_setting")
	}
	r1 := loadKW - pvKW // разряд днём (нагрузка минус PV)
	if r1 < 0 {
		r1 = 0
	}
	autoH := 999.0
	switch {
	case loadKW < 0.05:
	case r1 <= 0.001: // PV покрывает нагрузку → держимся до заката, дальше по нагрузке
		autoH = hSun + usableKWh/loadKW
	default:
		if e1 := r1 * hSun; e1 >= usableKWh {
			autoH = usableKWh / r1
		} else {
			autoH = hSun + (usableKWh-e1)/loadKW
		}
	}
	s.t(174, 802, 12, cSub, "middle", "если свет пропадёт сейчас:")
	if loadKW < 0.05 {
		s.t(174, 834, 20, cGrn, "middle", "нет нагрузки")
	} else if autoH >= 48 {
		s.t(174, 834, 22, cTxt, "middle", "> 2 суток")
	} else {
		s.t(174, 834, 24, cTxt, "middle", fmt.Sprintf("≈ %dч %02dм", int(autoH), int((autoH-math.Floor(autoH))*60)))
	}
	sub := fmt.Sprintf("нагрузка %.1f кВт", loadKW)
	if hSun > 0.1 && pvKW > 0.2 {
		sub += fmt.Sprintf(" · PV %.1f кВт до заката ~%.0fч", pvKW, hSun)
	}
	s.t(174, 860, 10, cSub, "middle", sub)
	s.t(174, 880, 10, cSub, "middle", fmt.Sprintf("ёмкость %.0f кВт·ч · отключение %.0f%%", capKWh, cutoff))

	// Солнышко
	s.box(360, 520, 560, 400)
	s.head(360, 520, 560, "sun", "Солнышко", "")
	// текущая погода между названием и «сегодня» — чтобы на скрине были видны условия
	if w := "weather.forecast_home_assistant"; st.State(w) != "" && st.State(w) != "unavailable" {
		s.t(492, 547, 12, cSub, "start", fmt.Sprintf("%s · %.0f°C · обл %.0f%% · %.1f м/с",
			weatherRu(st.State(w)), st.AttrNum(w, "temperature"), st.AttrNum(w, "cloud_coverage"), st.AttrNum(w, "wind_speed")))
	}
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
