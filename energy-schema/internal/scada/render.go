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
	if v < lo || v > hi { // выход за допустимые границы — оранжевым (не красным)
		return cOrg
	}
	return cGrn
}

// vHighRed — порог повышенного напряжения на ВХОДЕ ИНВЕРТОРА: выше него красным
// (инвертор ловит это как нестабильное и отваливается). Только для инвертора.
const vHighRed = 240.0

// invVCol colours the inverter's incoming grid voltage: red if absent (<1) or
// elevated (>240), orange if low (<205), normal otherwise. Применяется ТОЛЬКО к
// сети·вход инвертора — на входах/стабилизаторах напряжение красным не красим.
func invVCol(v float64) string {
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
	// АВР «залип»: переключён на Резерв, но инвертор всё ещё несёт нагрузку Дома —
	// значит реле не перекинулось (питание не ушло на резерв).
	avrLoad := st.Num("sensor.deye_sun_30k_load_l1_power") + st.Num("sensor.deye_sun_30k_load_l2_power") + st.Num("sensor.deye_sun_30k_load_l3_power")
	avrStuck := avrPos == "reserve" && avrLoad > 200
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
	s.flow(cGrn, rybPhase(st, 1, contRyb), 2, false, 264, 108, 360, 108)
	s.flow(cGrn, rybPhase(st, 2, contRyb), 2, false, 264, 144, 328, 144, 328, 30, 720, 30, 720, 44)
	s.flow(cGrn, rybPhase(st, 3, contRyb), 2, false, 264, 180, 284, 180, 284, 14, 985, 14, 985, 44)
	// выходы 3 стабилизаторов -> общая шина (y=275) -> Контактор и АВР(резерв)
	out1, out2, out3 := stabOut(st, 1, contRyb), stabOut(st, 2, contRyb), stabOut(st, 3, contRyb)
	// стабилизаторы отдают мощность, только если есть потребитель: контактор на
	// Ввод1 ИЛИ АВР в резерве. Иначе выходы/шина — серые штрихованые (нет нагрузки).
	stabConsumer := contRyb || avrPos == "reserve"
	og := func(realSt string) string {
		if stabConsumer {
			return realSt
		}
		return "off"
	}
	s.flow(cGrn, og(out1), 3, false, 455, 219, 455, 275)
	s.flow(cGrn, og(out2), 3, false, 720, 219, 720, 275)
	s.flow(cGrn, og(out3), 3, false, 985, 219, 985, 275)
	// шина: зелёная если все выходы в норме; оранжевая при потере; серая штрихованая
	// если всё off ИЛИ нет потребителя.
	busSt := "off"
	if out1 == "on" && out2 == "on" && out3 == "on" {
		busSt = "on"
	} else if out1 != "off" || out2 != "off" || out3 != "off" {
		busSt = "lost"
	}
	if !stabConsumer {
		busSt = "off"
	}
	busDash := ""
	switch busSt {
	case "lost":
		busDash = "6 5"
	case "off":
		busDash = "7 7"
	}
	s.poly(stOn[busSt], 3, busDash, 455, 275, 985, 275)
	// шина -> контактор: активна только когда контактор на Ввод1 (стабилизаторы);
	// заводим к центру карточки контактора (x≈132, рядом с Ввод2 на 156)
	s.flow(cGrn, map[bool]string{true: busSt, false: "off"}[contRyb], 3, false, 455, 275, 132, 275, 132, 300)
	// шина -> АВР(резерв): активна только когда АВР в резерве. Падаем вертикально
	// прямо с шины в точке x=905 (раньше шёл 985->905 по самой шине — наложение).
	s.flow(cGrn, map[bool]string{true: busSt, false: "off"}[avrPos == "reserve"], 3, false, 905, 275, 905, 300)
	// Ввод2 -> Контактор: активна только когда контактор на Ввод2; выходим снизу
	// карточки Зелёного (x=1300) и заводим к центру контактора (x=156)
	s.flow(cBlu, map[bool]string{true: grnSt, false: "off"}[contOn], 2, exporting, 1300, 219, 1300, 252, 156, 252, 156, 300)
	// Контактор -> Инвертор: цвет по активному вводу (зелёный=стабилизаторы, синий=Зелёный)
	cSt := "on"
	if !gridIn {
		cSt = "bad"
	}
	inCol := cGrn
	if contOn {
		inCol = cBlu
	}
	s.flow(inCol, cSt, 2, false, 264, 380, 400, 380)
	// Инвертор -> АВР (осн.). Если АВР залип на резерве, а инвертор всё ещё кормит —
	// рисуем оранжевым (поток есть там, где его быть не должно).
	if avrStuck {
		s.flow(cOrg, "on", 4, false, 740, 380, 800, 380)
	} else {
		s.flow(cGrn, map[bool]string{true: "on", false: "off"}[avrPos == "inverter"], 4, false, 740, 380, 800, 380)
	}
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
			s.t(252, y, 13, cTxt, "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
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
	// Стабилизаторы — по центру между вводами, с увеличенным интервалом
	stabX := []float64{360, 625, 890}
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
		row(0, "вход → выход", fmt.Sprintf("%d → %dВ", st.Int(p+"_vin"), st.Int(p+"_vout")), cTxt)
		row(1, "ступень", fmt.Sprintf("%d", st.Int(p+"_step")), cTxt)
		row(2, "нагрузка", fmt.Sprintf("%.0fА · %.2fкВт", loadA, loadA*st.Num(p+"_vout")/1000), cTxt)
		row(3, "U мин/макс", fmt.Sprintf("%d / %dВ", st.Int(p+"_vmin"), st.Int(p+"_vmax")), cTxt)
		if !st.On(p + "_on") {
			if rybPhase(st, ph, contRyb) == "lost" {
				s.t(x+180, 213, 10, cOrg, "end", "потеря (датчик)")
			} else {
				s.t(x+180, 213, 10, cRed, "end", "линия отключена")
			}
		}
	}
	// Ввод2 Зелёный — карточка того же размера, что и Рыбхоз (240×175), у правого края
	s.box(1180, 44, 240, 175)
	s.head(1180, 44, 240, "regen", cfg.In2Name, map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}[grnSt])
	// состояние/направление — честно: когда ввод не запитан, не пишем «потребление»
	if grnSt == "off" {
		s.t(1300, 94, 12, cGry, "middle", "ввод отключён")
	} else {
		dt, dc := "потребление", cBlu
		if st.State("sensor.sim_green_dir") == "export" {
			dt, dc = "отдача ↑", cGrn
		}
		s.t(1300, 94, 12, dc, "middle", dt)
	}
	for ph := 1; ph <= 3; ph++ {
		y := 120.0 + float64(ph-1)*32
		onE := fmt.Sprintf("sensor.sim_green_l%d_on", ph)
		vE := fmt.Sprintf("sensor.sim_green_l%d_v", ph)
		aE := fmt.Sprintf("sensor.sim_green_l%d_a", ph)
		s.dot(1200, y-5, 5, phCol(st, onE, vE, 200, 250))
		s.t(1216, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if st.On(onE) {
			v := st.Num(vE)
			a := st.Num(aE)
			s.t(1408, y, 13, cTxt, "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else {
			s.t(1408, y, 12, cGry, "end", "— нет —")
		}
	}
	// связь RS-485 со счётчиком ввода
	grnRS := st.Available("sensor.sim_green_l1_on") || st.Available("sensor.sim_green_l2_on") || st.Available("sensor.sim_green_l3_on")
	if grnRS {
		s.t(1190, 210, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(1190, 210, 10, cRed, "start", "RS-485 ✕")
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
	selRow := func(y float64, name, note, col, liveCol, key string, active bool) {
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
		if !active && ctLink {
			// клик по неактивному вводу → переключить контактор (с подтверждением)
			s.t(236, y+17, 10, cSub, "end", "тап →")
			s.p(`<rect x="34" y="%g" width="210" height="26" rx="6" fill="transparent" style="cursor:pointer" data-act="contactor" data-val="%s"/>`, y, key)
		} else {
			s.t(236, y+17, 10, nc, "end", note)
		}
	}
	selRow(364, cfg.In1Name, "по умолч.", cGrn, stOn[rybSt], "in1", !contOn)
	selRow(394, cfg.In2Name, "реле вкл", cBlu, stOn[grnSt], "in2", contOn)
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
	// СПРАВА: при ошибке — её текст; в норме — импорт/отдача энергии по сети за
	// сегодня (↓ из сети · ↑ в сеть). «Норма» и так видна зелёной точкой в шапке.
	if invProb {
		s.t(726, 351, 12, cRed, "end", "Ошибка: "+invState)
	} else {
		imp := st.Num("sensor.deye_sun_30k_today_energy_import")
		exp := st.Num("sensor.deye_sun_30k_today_energy_export")
		s.p(`<text x="726" y="351" font-size="12" text-anchor="end" fill="%s">сегодня <tspan fill="%s">↓ %.0f</tspan> <tspan fill="%s">↑ %.0f</tspan> кВт·ч</text>`, cSub, cAmb, imp, cGrn, exp)
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
		s.t(506, y, 14, invVCol(gv), "end", fmt.Sprintf("%.0f В", gv))
		s.t(572, y, 14, cTxt, "end", fmt.Sprintf("%.0f Вт", gw))
		s.t(652, y, 14, cTxt, "end", fmt.Sprintf("%.0f В", ov))
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
	if avrStuck {
		avrLinkCol = cOrg // залип — оранжевая тревога
	}
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
		} else if avrMode == "manual" {
			// в ручном режиме неактивный источник кликабелен: тап → переключить
			s.t(980, y+16, 11, cSub, "end", "тап →")
			s.p(`<rect x="812" y="%g" width="176" height="24" rx="6" fill="transparent" style="cursor:pointer" data-act="avr_src" data-val="%s"/>`, y, key)
		}
	}
	avrRow(376, "Инвертор", "inverter", cGrn)
	avrRow(404, "Резерв · "+cfg.In1Name, "reserve", cOrg)
	// статистика переключений / тревога залипания (когда питание не ушло на резерв)
	if avrStuck {
		s.t(900, 444, 11, cOrg, "middle", "⚠ залип — инвертор кормит")
	} else {
		s.t(900, 444, 11, cTxt, "middle", fmt.Sprintf("переключений: всего %.0f / сегодня %.0f", st.Num("sensor.sim_avr_switches"), st.Num("sensor.sim_avr_switches_today")))
	}
	// низ: связь RS-485 (как у контактора)
	if avrLink {
		s.t(812, 464, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(812, 464, 10, cRed, "start", "RS-485 ✕")
	}

	// Дом — гейдж
	s.box(1140, 290, 280, 190)
	s.head(1140, 290, 280, "home", "Дом", "")
	// потребление за последние 24 ч (среднее × 24) — в правом верхнем углу
	if av24, ok := st.Avg24h("sensor.deye_sun_30k_load_power"); ok {
		s.t(1404, 314, 12, cSub, "end", fmt.Sprintf("24ч: %.0f кВт·ч", av24*24/1000))
	}
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
	// синяя капля + выноска со значением — СРЕДНЕЕ за 24 ч
	av, okAv := st.Avg24h(lpe)
	if okAv {
		aa := gAng(av/1000, hMax)
		s.markerMax(1280, 410, 78, aa, 78*0.12, cBlu)
		s.markerLabel(1280, 410, 78, aa, fmt.Sprintf("%.1f", av/1000), cBlu)
	}
	// итог за 24 ч одной строкой — крупнее, среднее/макс через слэш
	s.t(1280, 465, 14, cTxt, "middle", fmt.Sprintf("24ч · средн/макс: %.1f / %.1f кВт", av/1000, st.Max24h(lpe)/1000))

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
		// цвет индикатора напряжения стринга по зонам входа MPPT:
		// <150 В — серый (нет солнца/тёмный стринг); 150–800 — норма; 800–1000 —
		// очень опасная зона (красный); >1000 — превышен предел входа («горит инвертор», ⚠)
		vCol, vMark := cTxt, ""
		switch {
		case vv < 150:
			vCol = cGry
		case vv > 1000:
			vCol, vMark = cRed, "⚠ "
		case vv > 800:
			vCol = cRed
		}
		s.p(`<text x="%g" y="684" font-size="16" text-anchor="middle" fill="%s"><tspan fill="%s">%s%.0f В</tspan> · %.1f А</text>`, gx[i], cTxt, vCol, vMark, vv, aa)
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
	s.barTicks(380, 779, 520, pvInputMaxKW, []float64{cfg.PVT1, cfg.PVT2, cfg.PVT3, cfg.PVMax}) // 5 · 20 · 25 · 33
	s.t(898, 779, 10, cSub, "end", fmt.Sprintf("%.0f кВт", pvInputMaxKW))

	// Генератор — компактно: верх = ключевые индикаторы, ниже наработка/масло и фазы
	s.box(956, 520, 464, 280)
	gk := "gen"
	gtc, gtxt := cGry, "ВЫКЛЮЧЕН"
	if genRun {
		gk, gtc, gtxt = "genrun", cGrn, "РАБОТАЕТ"
	}
	s.head(956, 520, 464, gk, "Генератор", gtc)
	genMode := st.State("sensor.sim_gen_mode")
	genAuto := genMode == "auto"
	sig := st.State("sensor.sim_gen_start_signal") == "on"
	// шапка: режим (индикатор в рамке) · значок инвертора (сигнал) · АКБ · статус
	mCol, mTxt := cGrn, "АВТО"
	if !genAuto {
		mCol, mTxt = cGry, "РУЧНОЙ"
	}
	s.p(`<rect x="1085" y="538" width="62" height="18" rx="6" fill="%s" fill-opacity="0.12" stroke="%s" stroke-width="1.2"/>`, mCol, mCol)
	s.t(1116, 551, 12, mCol, "middle", mTxt)
	// пусковой сигнал от инвертора — значок инвертора (DC→AC): серый нет / зелёный есть
	sigCol := cGry
	if sig {
		sigCol = cGrn
	}
	s.p(`<rect x="1156" y="536" width="24" height="16" rx="2" fill="none" stroke="%s" stroke-width="1.6"/>`, sigCol)
	s.p(`<line x1="1168" y1="538" x2="1168" y2="550" stroke="%s" stroke-width="1.3"/>`, sigCol)
	s.p(`<line x1="1160" y1="542" x2="1165" y2="542" stroke="%s" stroke-width="1.3"/><line x1="1160" y1="546" x2="1165" y2="546" stroke="%s" stroke-width="1.3"/>`, sigCol, sigCol)
	s.p(`<path d="M 1170 544 c 1.2,-3.5 3.3,-3.5 4.5,0 c 1.2,3.5 3.3,3.5 4.5,0" fill="none" stroke="%s" stroke-width="1.3"/>`, sigCol)
	// АКБ запуска — значок батареи + значение
	bv := st.Num("sensor.sim_gen_batt_v")
	bvc := cGrn
	if bv > 0 && bv < 12.0 {
		bvc = cOrg
	}
	if bv > 0 && bv < 11.5 {
		bvc = cRed
	}
	s.p(`<rect x="1200" y="540" width="20" height="12" rx="2" fill="none" stroke="%s" stroke-width="1.5"/>`, bvc)
	s.p(`<rect x="1220" y="543" width="3" height="6" rx="1" fill="%s"/>`, bvc)
	s.t(1226, 550, 12, bvc, "start", fmt.Sprintf("%.1fВ", bv))
	s.t(1382, 547, 14, gtc, "end", gtxt)

	// --- управление: подогрев + старт/стоп — компактно, одной строкой справа ---
	htOn := st.State("sensor.sim_gen_coolant_heater") == "on"
	htCur := st.Int("sensor.sim_gen_coolant_temp")
	// температура старта (до которой греем ОЖ перед запуском) — настраивается из UI HA
	// через input_number.gen_start_temp; иначе старый хелпер/эмулятор.
	htTgt := st.Num("sensor.sim_gen_coolant_target")
	if st.Available("input_number.gen_coolant_target") {
		htTgt = st.Num("input_number.gen_coolant_target")
	}
	if st.Available("input_number.gen_start_temp") {
		htTgt = st.Num("input_number.gen_start_temp")
	}
	htCol, htFill := cSub, "0"
	if htOn {
		htCol, htFill = cOrg, "0.16"
	}
	// кнопка подогрева: иконка пламени + текущая→целевая темп. ОЖ внутри
	s.p(`<rect x="1200" y="562" width="132" height="26" rx="8" fill="%s" fill-opacity="%s" stroke="%s" stroke-width="1.4"/>`, htCol, htFill, htCol)
	for i := 0; i < 3; i++ {
		x := 1212.0 + float64(i)*5
		s.p(`<path d="M %.1f 581 q 2 -3 0 -6 q -2 -3 0 -6" fill="none" stroke="%s" stroke-width="1.8"/>`, x, htCol)
	}
	s.t(1232, 580, 12, cTxt, "start", fmt.Sprintf("%d→%.0f°C", htCur, htTgt))
	if genAuto {
		s.p(`<rect x="1200" y="562" width="132" height="26" rx="8" fill="transparent" style="cursor:pointer" data-act="gen_heater" data-val="%s"/>`, map[bool]string{true: "off", false: "on"}[htOn])
	}
	// кнопка старт/стоп (в авто кликабельна; в ручном — серая, только монитор)
	if genAuto {
		bCol, bTxt, bAct := cGrn, "СТАРТ", "gen_start"
		if genRun {
			bCol, bTxt, bAct = cRed, "СТОП", "gen_stop"
		}
		s.p(`<rect x="1338" y="562" width="72" height="26" rx="8" fill="%s" fill-opacity="0.18" stroke="%s" stroke-width="1.5"/>`, bCol, bCol)
		s.t(1374, 580, 13, bCol, "middle", bTxt)
		s.p(`<rect x="1338" y="562" width="72" height="26" rx="8" fill="transparent" style="cursor:pointer" data-act="%s" data-val="1"/>`, bAct)
	} else {
		s.p(`<rect x="1338" y="562" width="72" height="26" rx="8" fill="none" stroke="%s" stroke-width="1.3"/>`, cGry)
		s.t(1374, 580, 13, cGry, "middle", "СТАРТ")
		s.t(1410, 604, 10, cGry, "end", "ручной — только монитор")
	}

	// низ карточки делим на два столбца
	s.p(`<line x1="1192" y1="558" x2="1192" y2="760" stroke="%s" stroke-width="1"/>`, cBrd)

	// ЛЕВО: нагрузка по фазам — ток и напряжение по каждой линии
	s.t(972, 578, 11, cSub, "start", "Нагрузка по фазам")
	for ph := 1; ph <= 3; ph++ {
		y := 608.0 + float64(ph-1)*28
		p := fmt.Sprintf("sensor.sim_gen_l%d", ph)
		a, v := st.Num(p+"_load"), st.Num(p+"_v")
		s.t(972, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		s.t(1052, y, 13, cTxt, "middle", fmt.Sprintf("%.0f В", v))
		s.t(1180, y, 13, cTxt, "end", fmt.Sprintf("%.0f А · %.2f кВт", a, a*v/1000))
	}
	// связь управления генератором (RS-485 через наш блок) — внизу слева
	if st.Available("sensor.sim_gen_state") {
		s.t(972, 748, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(972, 748, 10, cRed, "start", "RS-485 ✕")
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
	s.ringTimer(1262, 692, 32, oilFr, ringCol(oilFr), "замена масла", fmt.Sprintf("%.0f ч", oilRem))
	s.t(1262, 738, 9, cSub, "middle", fmt.Sprintf("из %.0f ч", oilInt))
	s.ringTimer(1352, 692, 32, svcFr, ringCol(svcFr), "ТО", fmt.Sprintf("%.0f ч", svcRem))
	s.t(1352, 738, 9, cSub, "middle", fmt.Sprintf("из %.0f ч", svcInt))

	// низ во всю ширину карточки: наработка + последний запуск
	lastAgo := firstNum("input_number.gen_last_run_h", "sensor.sim_gen_last_run_h")
	lastMin := firstNum("input_number.gen_last_run_min", "sensor.sim_gen_last_run_min")
	s.t(1188, 778, 12, cSub, "middle", fmt.Sprintf("Наработка: %.1f ч", runtime))
	s.t(1188, 794, 10, cSub, "middle", fmt.Sprintf("посл. запуск %.0fч назад · работал %.0f мин", lastAgo, lastMin))

	s.p(`</svg>`)
	return s.String()
}
