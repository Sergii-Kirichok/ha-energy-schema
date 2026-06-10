package scada

import (
	"fmt"
	"math"
	"strings"

	"energy-schema/internal/config"
)

// invPeakKW — пиковая мощность инвертора по выходу (10 с) сверх длительных
// 33 кВт; зона 33–45 кВт — «перегруз» на гейдже потребления (Дом).
const invPeakKW = 45.0

// pvInputMaxKW — максимальная мощность PV-входа по шильдику инвертора (39 кВт).
// Это потолок шкалы суммарной генерации в карточке Солнце.
const pvInputMaxKW = 39.0

// State is the read-only view of HA entity states the renderer needs.
// *hass.Store satisfies it.
type State interface {
	State(entity string) string
	Num(entity string) float64
	Int(entity string) int
	On(entity string) bool
	Available(entity string) bool
	// last-good snapshot + offline duration (for devices that dropped out)
	LastState(entity string) string
	LastNum(entity string) float64
	LastInt(entity string) int
	LostInfo(entity string) string
	// scalar attributes + time-until helpers (weather, sunset)
	Attr(entity, key string) string
	AttrNum(entity, key string) float64
	HoursUntil(entity, key string) float64
	// daily weather forecast (cloud % + condition, 0=today 1=tomorrow)
	ForecastInfo(daysAhead int) (float64, string, bool)
	// today's peak numeric value for an entity (for gauge max markers)
	DayMax(entity string) float64
	// energy (kWh) integrated for a *_power entity since local midnight
	DayEnergy(entity string) float64
	// rolling 24h peak/trough (for battery/home markers — independent of midnight)
	Max24h(entity string) float64
	Avg24h(entity string) (float64, bool)
	// empirical generation baseline from long-term statistics
	PVClearDayKWh() float64 // best recent day (clear-day proxy), 0 if unknown
	PVRecent() (float64, int)
	// inverter→grid reconnection countdown: remaining s, total s, active, attempt #
	ReconnectInfo() (float64, float64, bool, int)
}

// phCol returns a phase color: red if off, orange if voltage out of [lo,hi],
// green otherwise.
func phCol(st State, onE, vE string, lo, hi float64) string {
	if !st.On(onE) {
		return cRed
	}
	v := st.Num(vE)
	if v > vHighRed { // повышенное — красным (инвертор ловит как нестабильное и отваливается)
		return cRed
	}
	if v < lo || v > hi {
		return cOrg
	}
	return cGrn
}

// vHighRed — порог повышенного напряжения: выше него подсвечиваем красным.
const vHighRed = 240.0

// vCol returns the colour for a voltage reading: red if absent (<1) or elevated
// (>240 — что и сбивает инвертор при переключении ступени стабилизатора),
// orange if low (<205), normal otherwise.
func vCol(v float64) string {
	switch {
	case v < 1:
		return cRed
	case v > vHighRed:
		return cRed
	case v < 205:
		return cOrg
	default:
		return cTxt
	}
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
	s.p(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 808" font-family="Arial,Helvetica,sans-serif">`)
	s.p(`<rect x="0" y="0" width="1440" height="830" fill="#0f1115"/>`)
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
	s.flow(cBlu, cSt, 2, false, 264, 380, 400, 380)
	// Инвертор -> АВР (осн.)
	s.flow(cGrn, map[bool]string{true: "on", false: "off"}[avrPos == "inverter"], 4, false, 740, 380, 800, 380)
	// АВР -> Дом
	s.flow(cGrn, "on", 3, false, 1000, 380, 1140, 380)
	// Батарея <-> Инвертор
	// Батарея <-> Инвертор: движение только при заряде/разряде; в покое (idle) — статичная линия.
	// Горизонталь на одном уровне с линией генератора (y=494).
	if math.Abs(bp) > 20 {
		s.flow(cPur, "on", math.Abs(bp)/1000, bp < 0, 174, 520, 174, 494, 470, 494, 470, 475)
	} else {
		s.poly(cPur, 3, "", 174, 520, 174, 494, 470, 494, 470, 475)
	}
	// PV -> Инвертор
	s.flow(cAmb, map[bool]string{true: "on", false: "off"}[pvtot > 30], pvtot/1000, false, 540, 520, 540, 475)
	// Генератор -> Инвертор: одна силовая линия. Управляющий сигнал отдельной линией
	// не рисуем — он показан значком «G» в правом нижнем углу карточки инвертора.
	s.flow(cGrn, map[bool]string{true: "on", false: "off"}[genRun], 2, false, 1060, 520, 1060, 494, 588, 494, 588, 475)

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
			s.t(252, y, 13, vCol(v), "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else if ps == "lost" {
			s.t(252, y, 12, cOrg, "end", "потеря связи")
		} else {
			s.t(252, y, 12, cRed, "end", "обрыв")
		}
	}
	// связь RS-485 со счётчиком ввода (живая, если хоть одна фаза отвечает)
	rybRS := st.Available("sensor.sim_ryb_l1_on") || st.Available("sensor.sim_ryb_l2_on") || st.Available("sensor.sim_ryb_l3_on")
	if rybRS {
		s.t(34, 210, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(34, 210, 10, cRed, "start", "RS-485 ✕")
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
		// связь RS-485 (как у контактора) — внизу слева
		if linkOk {
			s.t(x+10, 213, 10, cSub, "start", "RS-485 ✓")
		} else {
			s.t(x+10, 213, 10, cRed, "start", "RS-485 ✕")
		}
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
				s.t(x+180, 213, 10, cOrg, "end", "молчит "+info)
			}
			continue
		}
		mc, mt := cBlu, "стабилизация"
		if st.State(p+"_mode") == "transit" {
			mc, mt = cSub, "транзит"
		}
		s.t(x+95, 100, 12, mc, "middle", mt)
		loadA := st.Num(p + "_load")
		row := func(n int, label, val, col string) {
			s.t(x+14, 124+float64(n)*22, 11, cSub, "start", label)
			s.t(x+176, 124+float64(n)*22, 12, col, "end", val)
		}
		row(0, "вход → выход", fmt.Sprintf("%d → %dВ", st.Int(p+"_vin"), st.Int(p+"_vout")), vCol(st.Num(p+"_vout")))
		row(1, "ступень", fmt.Sprintf("%d", st.Int(p+"_step")), cTxt)
		row(2, "нагрузка", fmt.Sprintf("%.0fА · %.2fкВт", loadA, loadA*st.Num(p+"_vout")/1000), cTxt)
		row(3, "U мин/макс", fmt.Sprintf("%d / %dВ", st.Int(p+"_vmin"), st.Int(p+"_vmax")), vCol(st.Num(p+"_vmax")))
		if !st.On(p + "_on") {
			if rybPhase(st, ph, contRyb) == "lost" {
				s.t(x+180, 213, 10, cOrg, "end", "потеря (датчик)")
			} else {
				s.t(x+180, 213, 10, cRed, "end", "линия отключена")
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
			s.t(1248, y, 13, vCol(v), "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else {
			s.t(1248, y, 12, cGry, "end", "— нет —")
		}
	}
	// связь RS-485 со счётчиком ввода
	grnRS := st.Available("sensor.sim_green_l1_on") || st.Available("sensor.sim_green_l2_on") || st.Available("sensor.sim_green_l3_on")
	if grnRS {
		s.t(1030, 210, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(1030, 210, 10, cRed, "start", "RS-485 ✕")
	}

	// ===================== ROW 2 =====================
	// Контактор — одно реле (RS-485), с обратной связью: ВЫКЛ=Ввод1, ВКЛ=Ввод2
	s.box(24, 300, 240, 175)
	ctLink := st.State("sensor.sim_contactor_link") != "lost" // обратная связь RS-485 (по умолч. есть)
	ctDot := cGrn
	if !ctLink {
		ctDot = cRed
	}
	s.head(24, 300, 240, "sw", "Контактор", ctDot)
	// крупный статус: состояние реле → какой ввод в работе
	if !ctLink {
		s.t(144, 348, 14, cRed, "middle", "НЕТ СВЯЗИ (485)")
	} else if contOn {
		s.t(144, 348, 16, cBlu, "middle", "ВКЛ → Ввод 2")
	} else {
		s.t(144, 348, 16, cGrn, "middle", "ВЫКЛ → Ввод 1")
	}
	// какой ввод сейчас активен (подсветка) + индикатор «живости» линии + при каком реле
	selRow := func(y float64, name, note, col, liveCol string, active bool) {
		if active {
			s.p(`<rect x="34" y="%g" width="210" height="26" rx="6" fill="%s" fill-opacity="0.16" stroke="%s" stroke-width="1.5"/>`, y, col, col)
		} else {
			s.p(`<rect x="34" y="%g" width="210" height="26" rx="6" fill="none" stroke="%s" stroke-width="1"/>`, y, cBrd)
		}
		s.dot(52, y+13, 5, liveCol)
		tc, nc := cSub, cSub
		if active {
			tc, nc = col, col
		}
		s.t(68, y+17, 13, tc, "start", name)
		s.t(236, y+17, 10, nc, "end", note)
	}
	selRow(364, cfg.In1Name, "по умолч.", cGrn, stOn[rybSt], !contOn)
	selRow(394, cfg.In2Name, "реле вкл", cBlu, stOn[grnSt], contOn)
	// пояснение защиты: без управляющего питания контактор остаётся на Вводе 1
	s.t(144, 442, 10, cSub, "middle", "перекидной · без питания → Ввод 1 (защита)")
	// низ: связь RS-485 + отдача
	if ctLink {
		s.t(34, 462, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(34, 462, 10, cRed, "start", "RS-485 ✕")
	}
	if st.State("sensor.sim_export") == "on" {
		s.t(254, 462, 10, cGrn, "end", "отдача ↑")
	} else {
		s.t(254, 462, 10, cSub, "end", "отдача —")
	}

	df := st.State("sensor.deye_sun_30k_device_fault")
	da := st.State("sensor.deye_sun_30k_device_alarm")
	invState := st.State("sensor.deye_sun_30k_device_state")
	invProb := (invState != "" && invState != "Normal") || (df != "" && df != "OK") || (da != "" && da != "OK")
	s.box(400, 300, 340, 175)
	hc := map[bool]string{true: cGrn, false: cGry}[genRun || gridIn]
	if invProb {
		hc = cRed
	}
	s.head(400, 300, 340, "inv", "Инвертор", hc)
	// температура инвертора — в шапке
	temp := st.Num("sensor.deye_sun_30k_temperature")
	tc := cGrn
	if temp >= 65 {
		tc = cRed
	} else if temp >= 50 {
		tc = cOrg
	}
	s.t(580, 327, 13, tc, "middle", fmt.Sprintf("%.1f °C", temp))
	// наличие/использование сети — СЛЕВА: КРАСНЫМ когда сети нет (проблема входа)
	gridP := st.Num("sensor.deye_sun_30k_grid_power")
	if !gridAvail {
		s.t(414, 351, 12, cRed, "start", "сеть: НЕТ ✕")
	} else if gridBonded {
		s.t(414, 351, 12, cGrn, "start", fmt.Sprintf("сеть: %.2f кВт ✓", gridP/1000))
	} else {
		s.t(414, 351, 12, cOrg, "start", "сеть: откл. защитой ✕")
	}
	// статус инвертора — СПРАВА
	if invProb {
		s.t(726, 351, 12, cRed, "end", "Ошибка: "+invState)
	} else {
		s.t(726, 351, 12, cGrn, "end", "Статус: норма")
	}
	// по фазам: ВХОД (сеть) и ВЫХОД (инвертор → дом) — это РАЗНЫЕ счётчики.
	// При пропаже сети вход = 0В (красный), а выход инвертора держит ~230В.
	s.t(414, 370, 12, cTxt, "start", "фаза")
	s.t(519, 370, 12, cSub, "middle", "сеть · вход")
	s.t(669, 370, 12, cSub, "middle", "инвертор → дом")
	for ph := 1; ph <= 3; ph++ {
		y := 392.0 + float64(ph-1)*20
		gv := st.Num(fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_voltage", ph))
		gw := st.Num(fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_power", ph))
		ov := st.Num(fmt.Sprintf("sensor.deye_sun_30k_output_l%d_voltage", ph))
		lw := st.Num(fmt.Sprintf("sensor.deye_sun_30k_load_l%d_power", ph))
		s.t(414, y, 14, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(506, y, 14, vCol(gv), "end", fmt.Sprintf("%.0f В", gv))
		s.t(572, y, 14, cTxt, "end", fmt.Sprintf("%.0f Вт", gw))
		s.t(652, y, 14, vCol(ov), "end", fmt.Sprintf("%.0f В", ov))
		s.t(726, y, 14, cTxt, "end", fmt.Sprintf("%.0f Вт", lw))
	}
	// нижняя строка: либо обратный отсчёт реконнекта (кольцо), либо состояние сети
	rcRem, rcTotal, rcActive, rcAtt := st.ReconnectInfo()
	if rcActive {
		// инвертор увидел сеть, но ещё не подключился — кольцо с убывающими секундами
		cx, cy, rr := 432.0, 454.0, 15.0
		frac := 0.0
		if rcTotal > 0 {
			frac = rcRem / rcTotal
		}
		col := cOrg
		if rcRem <= 10 {
			col = cGrn // вот-вот подключится
		}
		s.p(`<circle cx="%g" cy="%g" r="%g" fill="none" stroke="#23272f" stroke-width="4"/>`, cx, cy, rr)
		if frac >= 0.999 {
			s.p(`<circle cx="%g" cy="%g" r="%g" fill="none" stroke="%s" stroke-width="4"/>`, cx, cy, rr, col)
		} else if frac > 0.001 {
			x0, y0 := pt(cx, cy, rr, 90)
			x1, y1 := pt(cx, cy, rr, 90-frac*360)
			large := 0
			if frac > 0.5 {
				large = 1
			}
			s.p(`<path fill="none" stroke="%s" stroke-width="4" stroke-linecap="round" d="M %.1f %.1f A %g %g 0 %d 1 %.1f %.1f"/>`, col, x0, y0, rr, rr, large, x1, y1)
		}
		s.t(cx, cy+4, 13, col, "middle", fmt.Sprintf("%.0f", rcRem))
		s.t(cx+rr+8, cy-2, 11, cTxt, "start", "подключение к сети")
		if rcAtt > 1 {
			s.t(cx+rr+8, cy+12, 9, cOrg, "start", fmt.Sprintf("попытка %d", rcAtt))
		} else {
			s.t(cx+rr+8, cy+12, 9, cSub, "start", fmt.Sprintf("реконнект %.0f с", rcTotal))
		}
	} else if !st.On("binary_sensor.deye_sun_30k_grid") {
		s.t(414, 462, 10, cOrg, "start", "сеть отключена · островной режим")
	} else {
		s.t(414, 462, 10, cSub, "start", fmt.Sprintf("сеть %.1f Гц · реконнект %.0f с", st.Num("sensor.deye_sun_30k_grid_frequency"), rcTotal))
	}
	// значок генератора (правый нижний угол): подан ли управляющий сигнал на запуск
	genSigCol := cGry
	if st.State("sensor.sim_gen_start_signal") == "on" {
		genSigCol = cOrg
	}
	s.t(702, 462, 10, genSigCol, "end", "пуск")
	s.p(`<circle cx="720" cy="458" r="9" fill="none" stroke="%s" stroke-width="2"/>`, genSigCol)
	s.t(720, 462, 11, genSigCol, "middle", "G")

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
	// статистика переключений: всего / сегодня — одной строкой через слэш
	s.t(900, 444, 11, cTxt, "middle", fmt.Sprintf("переключений: всего %.0f / сегодня %.0f", st.Num("sensor.sim_avr_switches"), st.Num("sensor.sim_avr_switches_today")))
	// низ: связь RS-485 (как у контактора)
	if avrLink {
		s.t(812, 464, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(812, 464, 10, cRed, "start", "RS-485 ✕")
	}

	// Дом — гейдж
	s.box(1140, 290, 280, 190)
	s.head(1140, 290, 280, "home", "Дом", "")
	// шкала до 45 кВт: 33 — длительный максимум инвертора, 33–45 — перегруз (10 с)
	hMax := invPeakKW
	s.gauge(1280, 410, 78, load, hMax, []band{{cfg.HomeT1, cGrn}, {cfg.HomeT2, cAmb}, {cfg.HomeT3, cOrg}, {cfg.PVMax, cRed}, {hMax, cRed2}}, kw(load*1000), "потребление")
	// тики: концы 0/45 + переходы зон (3 пропускаем — сливается с 5 на сжатой шкале)
	for _, tk := range []float64{0, cfg.HomeT2, cfg.HomeT3, cfg.PVMax, hMax} {
		s.gaugeTick(1280, 410, 78, tk, hMax, fmt.Sprintf("%.0f", tk))
	}
	lpe := "sensor.deye_sun_30k_load_power"
	// красная капля + выноска — пик потребления за 24 ч; синяя — минимум за 24 ч
	if pl := st.Max24h(lpe); pl > 50 {
		a := gAng(pl/1000, hMax)
		s.markerMax(1280, 410, 78, a, 78*0.12, cRed)
		s.markerLabel(1280, 410, 78, a, fmt.Sprintf("%.1f", pl/1000), cRed)
	}
	// синяя капля — СРЕДНЕЕ за 24 ч (минимум почти всегда ~0 и неинформативен)
	av, okAv := st.Avg24h(lpe)
	if okAv {
		s.markerMax(1280, 410, 78, gAng(av/1000, hMax), 78*0.12, cBlu)
	}
	// итог за 24 ч одной строкой — крупнее, среднее/макс через слэш
	s.t(1280, 465, 14, cTxt, "middle", fmt.Sprintf("за 24ч · средн/макс: %.1f / %.1f кВт", av/1000, st.Max24h(lpe)/1000))

	// ===================== ROW 3 =====================
	// Батарея
	s.box(24, 520, 300, 280)
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
	bcx, bcy := 174.0, 610.0
	s.arc(bcx, bcy, 68, 180, 0, "#23272f", 13)
	socCol := cGrn
	if soc < 20 {
		socCol = cRed
	} else if soc < 50 {
		socCol = cAmb
	}
	s.arc(bcx, bcy, 68, 180, gAng(soc, 100), socCol, 13)
	s.marker(bcx, bcy, 68, gAng(soc, 100), 7)
	// красная капля + выноска со значением пика заряда (SOC) за последние 24 ч
	if ps := st.Max24h("sensor.deye_sun_30k_battery"); ps > 1 {
		a := gAng(ps, 100)
		s.markerMax(bcx, bcy, 68, a, 68*0.12, cRed)
		s.markerLabel(bcx, bcy, 68, a, fmt.Sprintf("%.0f%%", ps), cRed)
	}
	s.t(bcx, bcy-2, 26, cTxt, "middle", fmt.Sprintf("%.0f%%", soc))

	// ток — слева от спидометра, SOH — справа (симметрично)
	s.t(54, 596, 11, cSub, "middle", "ток")
	s.t(54, 616, 15, cTxt, "middle", fmt.Sprintf("%.1f А", st.Num("sensor.deye_sun_30k_battery_current")))
	s.t(294, 596, 11, cSub, "middle", "SOH")
	s.t(294, 616, 15, cTxt, "middle", fmt.Sprintf("%.0f%%", st.Num("sensor.deye_sun_30k_battery_soh")))

	// заряд/разряд — визуально (пилюля со стрелкой); покой = без стрелки
	if bAlarm {
		s.p(`<rect x="44" y="634" width="260" height="26" rx="13" fill="%s" fill-opacity="0.18" stroke="%s" stroke-width="1.5"/>`, cRed, cRed)
		s.t(174, 652, 14, cRed, "middle", "⚠ АВАРИЯ БАТАРЕИ")
	} else if bp < -20 {
		s.p(`<rect x="44" y="634" width="260" height="26" rx="13" fill="%s" fill-opacity="0.15" stroke="%s" stroke-width="1.5"/>`, cGrn, cGrn)
		s.p(`<polygon points="70,640 78,654 86,640" fill="%s"/>`, cGrn)
		s.t(190, 652, 14, cGrn, "middle", "ЗАРЯД "+kw(-bp))
	} else if bp > 20 {
		s.p(`<rect x="44" y="634" width="260" height="26" rx="13" fill="%s" fill-opacity="0.15" stroke="%s" stroke-width="1.5"/>`, cOrg, cOrg)
		s.p(`<polygon points="70,654 78,640 86,654" fill="%s"/>`, cOrg)
		s.t(190, 652, 14, cOrg, "middle", "РАЗРЯД "+kw(bp))
	} else {
		s.p(`<rect x="44" y="634" width="260" height="26" rx="13" fill="none" stroke="%s" stroke-width="1"/>`, cBrd)
		s.t(174, 652, 13, cSub, "middle", "ожидание (idle)")
	}

	// доступно сейчас: запас энергии до отключения + на сколько его хватит
	cutoff := st.Num("number.deye_sun_30k_battery_shutdown_soc")
	if cutoff <= 0 {
		cutoff = st.Num("number.deye_sun_30k_battery_low_soc")
	}
	if cutoff <= 0 {
		cutoff = 15
	}
	// ёмкость: паспортный номинал пакета × деградацию (SOH). Прямой сенсор Deye
	// занижает (≈50), Ah×«живое» напряжение завышает (≈64) — поэтому берём
	// номинал из конфига (реальные 60 кВт·ч) и корректируем на здоровье батареи.
	capNom := cfg.BattCap
	soh := st.Num("sensor.deye_sun_30k_battery_soh")
	if soh <= 0 || soh > 100 {
		soh = 100
	}
	capKWh := capNom * soh / 100 // эффективная ёмкость с учётом износа
	usableKWh := capKWh * (soc - cutoff) / 100
	if usableKWh < 0 {
		usableKWh = 0
	}
	loadKW := load
	pvKW := pvtot / 1000
	// «ясный день» сезона — из реальной статистики (лучший суточный день за ~10
	// дней), а не из шильдика; если статистики ещё нет — запасной коэффициент.
	clearDay := st.PVClearDayKWh()
	if clearDay < 1 {
		clearDay = cfg.PVDayClearKWh
	}
	// две оценки: чисто АКБ (без солнца) и прогнозная (симуляция 48ч с погодой)
	batH := 99.0
	if loadKW > 0.05 {
		batH = usableKWh / loadKW
	}
	autoH, aNote := simulateAutonomy(st, usableKWh, capKWh*(100-cutoff)/100, loadKW, pvKW, clearDay)
	hfmt := func(h float64) string {
		if loadKW < 0.05 {
			return "≈ —"
		}
		if h >= 48 {
			return "≈ 48 ч+"
		}
		return fmt.Sprintf("≈ %d ч %02d м", int(h), int((h-math.Floor(h))*60))
	}
	rcol := cGrn
	if usableKWh < capKWh*0.12 {
		rcol = cOrg
	}
	// нижние строки: слева — метка, справа — значение крупным шрифтом
	s.t(40, 690, 13, cSub, "start", "Доступно")
	s.t(308, 690, 20, rcol, "end", fmt.Sprintf("%.1f кВт·ч", usableKWh))
	s.t(40, 716, 13, cSub, "start", "Только от АКБ")
	s.t(308, 716, 20, cTxt, "end", hfmt(batH))
	s.t(40, 742, 13, cSub, "start", "Прогноз на 48 ч")
	s.t(308, 742, 20, cGrn, "end", hfmt(autoH))
	// откуда берётся прогноз генерации + детали симуляции
	s.t(174, 762, 9, cSub, "middle", aNote)
	if avg, n := st.PVRecent(); n > 0 {
		s.t(174, 776, 9, cSub, "middle", fmt.Sprintf("прогноз по факту: %d дн · ясный ~%.0f · средн %.0f кВт·ч/сут", n, clearDay, avg))
	}
	s.t(174, 790, 9, cSub, "middle", fmt.Sprintf("ёмкость %.0f · SOH %.0f%% → %.0f кВт·ч · отключ. %.0f%%", capNom, soh, capKWh, cutoff))

	// Солнышко
	s.box(360, 520, 560, 280)
	s.head(360, 520, 560, "sun", "Солнышко", "")
	// текущая погода: иконка состояния + значения, по центру шапки
	if w := "weather.forecast_home_assistant"; st.State(w) != "" && st.State(w) != "unavailable" {
		s.wicon(st.State(w), 548, 540)
		s.t(566, 548, 14, cTxt, "start", fmt.Sprintf("%.0f°C · обл %.0f%% · %.1f м/с",
			st.AttrNum(w, "temperature"), st.AttrNum(w, "cloud_coverage"), st.AttrNum(w, "wind_speed")))
	}
	// сегодня: факт / прогноз на день — кратко. Облачность берём «живую» (как ниже
	// в шапке), чтобы прогноз сегодня и завтра (в карточке батареи) различались.
	todayProd := st.Num("sensor.deye_sun_30k_today_production")
	todayKWhTxt := fmt.Sprintf("сегодня %.0f кВт·ч", todayProd)
	if st.Available("weather.forecast_home_assistant") {
		cloudT := st.AttrNum("weather.forecast_home_assistant", "cloud_coverage")
		if cloudT <= 0 {
			if _, c0, ok0 := st.ForecastInfo(0); ok0 {
				cloudT = condCloud(c0)
			}
		}
		todayKWhTxt = fmt.Sprintf("сегодня %.0f / %.0f кВт·ч", todayProd, clearDay*(1-0.7*cloudT/100))
	}
	s.t(906, 547, 14, cAmb, "end", todayKWhTxt)
	gx := []float64{470, 650, 830} // меньше гейджи → больше места под боковые подписи
	pvFieldMax := 13.0             // макс на 1 MPPT по шильдику (39 кВт / 3 ≈ 13 кВт)
	pvR := 52.0
	for i := 0; i < 3; i++ {
		pe := fmt.Sprintf("sensor.deye_sun_30k_pv%d_power", i+1)
		pw := st.Num(pe)
		s.gauge(gx[i], 646, pvR, pw/1000, pvFieldMax, []band{{5, cAmb}, {10, cGrn}, {pvFieldMax, cRed}}, kw(pw), cfg.PVLabels[i])
		// суточная выработка стринга — мелким серым над текущей мощностью
		s.t(gx[i], 626, 10, cSub, "middle", fmt.Sprintf("%.1f кВт·ч", st.DayEnergy(pe)))
		// шкала тиками радиально (как у Дома): концы 0/13 по бокам + переходы зон 5/10
		for _, tk := range []float64{0, 5, 10, pvFieldMax} {
			s.gaugeTick(gx[i], 646, pvR, tk, pvFieldMax, fmt.Sprintf("%.0f", tk))
		}
		// капля + выноска со значением — пик генерации поля за сегодня
		if pmax := st.DayMax(pe); pmax > 50 {
			a := gAng(pmax/1000, pvFieldMax)
			s.markerMax(gx[i], 646, pvR, a, pvR*0.12, cRed)
			s.markerLabel(gx[i], 646, pvR, a, fmt.Sprintf("%.1f", pmax/1000), cRed)
		}
		vv := st.Num(fmt.Sprintf("sensor.deye_sun_30k_pv%d_voltage", i+1))
		aa := st.Num(fmt.Sprintf("sensor.deye_sun_30k_pv%d_current", i+1))
		s.t(gx[i], 684, 16, cTxt, "middle", fmt.Sprintf("%.0f В · %.1f А", vv, aa))
	}
	s.t(380, 710, 13, cSub, "start", "Всего")
	// пик суммарной генерации за сегодня: число (крупнее) + красная капля над шкалой
	if pmax := st.DayMax("sensor.deye_sun_30k_pv_power"); pmax > 50 {
		s.t(900, 710, 14, cRed, "end", "Max: "+kw(pmax))
		s.barMax(380, 722, 520, pmax/1000, pvInputMaxKW, cRed)
	}
	// шкала PV-входа до 39 кВт (шильдик); 33–39 — верхняя зона
	s.bar(380, 722, 520, 44, pvtot/1000, pvInputMaxKW, []band{{cfg.PVT1, cAmb}, {cfg.PVT2, cGrn}, {cfg.PVT3, cOrg}, {cfg.PVMax, cRed}, {pvInputMaxKW, cRed2}}, kw(pvtot))
	// границы шкалы + значения переходов зон
	s.t(382, 779, 10, cSub, "start", "0")
	s.barTicks(380, 779, 520, pvInputMaxKW, []float64{cfg.PVT2, cfg.PVT3, cfg.PVMax}) // 20 · 25 · 33
	s.t(898, 779, 10, cSub, "end", fmt.Sprintf("%.0f кВт", pvInputMaxKW))

	// Генератор — компактно: верх = ключевые индикаторы, ниже наработка/масло и фазы
	s.box(956, 520, 464, 280)
	gk := "gen"
	gtc, gtxt := cGry, "ВЫКЛЮЧЕН"
	if genRun {
		gk, gtc, gtxt = "genrun", cGrn, "РАБОТАЕТ"
	}
	s.head(956, 520, 464, gk, "Генератор", gtc)
	s.t(1388, 547, 15, gtc, "end", gtxt)

	// сигнал на запуск — «ключик» (зел=запущен / оранж=прогрев / красн=не завёлся / серый=нет)
	sig := st.State("sensor.sim_gen_start_signal") == "on"
	tts := st.Num("sensor.sim_gen_time_to_start_min")
	var sigCol, sigTxt string
	switch {
	case !sig:
		sigCol, sigTxt = cGry, "сигнала нет"
	case genRun:
		sigCol, sigTxt = cGrn, "сигнал · запущен"
	case tts > 0:
		sigCol, sigTxt = cOrg, fmt.Sprintf("сигнал · прогрев %.0fм", tts)
	default:
		sigCol, sigTxt = cRed, "сигнал · НЕ ЗАВЁЛСЯ"
	}
	s.p(`<rect x="972" y="566" width="220" height="28" rx="8" fill="%s" fill-opacity="0.12" stroke="%s" stroke-width="1.3"/>`, sigCol, sigCol)
	s.p(`<circle cx="990" cy="580" r="4" fill="none" stroke="%s" stroke-width="2"/><line x1="994" y1="580" x2="1006" y2="580" stroke="%s" stroke-width="2"/><line x1="1006" y1="580" x2="1006" y2="585" stroke="%s" stroke-width="2"/><line x1="1002" y1="580" x2="1002" y2="584" stroke="%s" stroke-width="2"/>`, sigCol, sigCol, sigCol, sigCol)
	s.t(1014, 584, 12, sigCol, "start", sigTxt)

	// подогрев ОЖ + температура
	htOn := st.State("sensor.sim_gen_coolant_heater") == "on"
	htCol, htTxt := cSub, "подогрев выкл"
	if htOn {
		htCol, htTxt = cOrg, "подогрев вкл"
	}
	s.p(`<rect x="1204" y="566" width="200" height="28" rx="8" fill="%s" fill-opacity="0.10" stroke="%s" stroke-width="1.3"/>`, htCol, cBrd)
	for i := 0; i < 3; i++ {
		x := 1220.0 + float64(i)*5
		s.p(`<path d="M %.1f 587 q 2 -3 0 -6 q -2 -3 0 -6" fill="none" stroke="%s" stroke-width="1.8"/>`, x, htCol)
	}
	s.t(1242, 584, 12, htCol, "start", htTxt)
	s.t(1396, 584, 13, cTxt, "end", fmt.Sprintf("%d°C", st.Int("sensor.sim_gen_coolant_temp")))

	// низ карточки делим на два сегмента
	s.p(`<line x1="1192" y1="612" x2="1192" y2="786" stroke="%s" stroke-width="1"/>`, cBrd)

	// ЛЕВО: нагрузка по фазам — ток и напряжение по каждой линии
	s.t(972, 626, 11, cSub, "start", "Нагрузка по фазам")
	for ph := 1; ph <= 3; ph++ {
		y := 654.0 + float64(ph-1)*26
		p := fmt.Sprintf("sensor.sim_gen_l%d", ph)
		a, v := st.Num(p+"_load"), st.Num(p+"_v")
		s.t(972, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(1052, y, 13, cTxt, "middle", fmt.Sprintf("%.0f В", v))
		s.t(1180, y, 13, cTxt, "end", fmt.Sprintf("%.0f А · %.2f кВт", a, a*v/1000))
	}

	// ПРАВО: обслуживание — кольца обратного отсчёта (масло, ТО) в моточасах
	ringCol := func(frac float64) string {
		switch {
		case frac < 0.1:
			return cRed
		case frac < 0.25:
			return cOrg
		default:
			return cGrn
		}
	}
	frac := func(rem, interval float64) float64 {
		if interval <= 0 {
			return 1
		}
		return rem / interval
	}
	// значения задаются в HA через input_number (их можно менять и сбрасывать в
	// интерфейсе HA); если хелпер не создан — берём из эмулятора.
	firstNum := func(ents ...string) float64 {
		for _, e := range ents {
			if st.Available(e) {
				return st.Num(e)
			}
		}
		return 0
	}
	oilRem := firstNum("input_number.gen_oil_remaining_h", "sensor.sim_gen_oil_remaining_h")
	oilInt := firstNum("input_number.gen_oil_interval_h", "sensor.sim_gen_oil_interval_h")
	svcRem := firstNum("input_number.gen_service_remaining_h", "sensor.sim_gen_service_remaining_h")
	svcInt := firstNum("input_number.gen_service_interval_h", "sensor.sim_gen_service_interval_h")
	runtime := firstNum("input_number.gen_runtime_h", "sensor.sim_gen_runtime_h")
	oilFr := frac(oilRem, oilInt)
	svcFr := frac(svcRem, svcInt)
	// кольцо: в центре — остаток, под кольцом — общий интервал
	s.ringTimer(1258, 672, 33, oilFr, ringCol(oilFr), "замена масла", fmt.Sprintf("%.0f ч", oilRem))
	s.t(1258, 722, 9, cSub, "middle", fmt.Sprintf("из %.0f ч", oilInt))
	s.ringTimer(1352, 672, 33, svcFr, ringCol(svcFr), "ТО", fmt.Sprintf("%.0f ч", svcRem))
	s.t(1352, 722, 9, cSub, "middle", fmt.Sprintf("из %.0f ч", svcInt))
	// наработка — одной строкой
	s.t(1304, 768, 13, cSub, "middle", fmt.Sprintf("Наработка: %.1f ч", runtime))

	s.p(`</svg>`)
	return s.String()
}
