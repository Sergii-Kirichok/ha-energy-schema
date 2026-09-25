package scada

import "fmt"

// row2 — средний ряд: Контактор, Инвертор, АВР, Дом.
func (f *frame) row2() {
	st, s, cfg := f.st, f.s, f.cfg
	emuStale, contOn, stOn, rybSt, grnSt, genRun := f.emuStale, f.contOn, f.stOn, f.rybSt, f.grnSt, f.genRun
	gridIn, gridAvail, gridBonded, avrStuck, avrPos, load := f.gridIn, f.gridAvail, f.gridBonded, f.avrStuck, f.avrPos, f.load

	// ===================== ROW 2 =====================
	// Контактор — одно реле (RS-485), с обратной связью: ВЫКЛ=Ввод1, ВКЛ=Ввод2
	s.box(24, 300, 240, 175)
	ctLink := st.State("sensor.sim_contactor_link") != "lost" && !emuStale // обратная связь RS-485 (по умолч. есть)
	ctDot := cGrn
	if !ctLink {
		ctDot = cRed
	}
	s.head(24, 300, 240, "sw", "АВР вводов", ctDot)
	// крупный статус: состояние реле → какой ввод в работе
	if !ctLink {
		s.t(144, 348, 14, cRed, "middle", "НЕТ СВЯЗИ (485)")
	} else if contOn {
		s.t(144, 348, 15, cBlu, "middle", "→ Ввод 2 · "+cfg.In2Name)
	} else {
		s.t(144, 348, 15, cGrn, "middle", "→ Ввод 1 · "+cfg.In1Name)
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
	s.t(555, 327, 13, tc, "middle", fmt.Sprintf("%.1f °C", temp))
	// полученные/отданные кВт·ч по сети за сегодня — в первой строке, после температуры
	if !invProb {
		imp := st.Num("sensor.deye_sun_30k_today_energy_import")
		exp := st.Num("sensor.deye_sun_30k_today_energy_export")
		s.p(`<text x="595" y="327" font-size="11" text-anchor="start" fill="%s"><tspan fill="%s">↓ %.0f</tspan> <tspan fill="%s">↑ %.0f</tspan> кВт·ч</text>`, cSub, cAmb, imp, cGrn, exp)
	}
	// наличие/использование сети — СЛЕВА: КРАСНЫМ когда сети нет (проблема входа)
	gridP := st.Num("sensor.deye_sun_30k_grid_power")
	if !gridAvail {
		s.t(414, 351, 12, cRed, "start", "сеть: НЕТ ✕")
	} else if gridBonded {
		s.t(414, 351, 12, cGrn, "start", fmt.Sprintf("сеть: %.2f кВт ✓", gridP/1000))
	} else {
		s.t(414, 351, 12, cOrg, "start", "сеть: откл. защитой ✕")
	}
	// статус инвертора — СПРАВА (норма видна и зелёной точкой в шапке)
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
		s.t(414, 462, 10, cOrg, "start", "сеть отключена · автономный режим")
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
	avrLink := st.State("sensor.sim_avr_link") == "ok" && !emuStale
	avrLinkCol := cGrn
	if avrStuck {
		avrLinkCol = cOrg // залип — оранжевая тревога
	}
	if !avrLink {
		avrLinkCol = cRed
	}
	s.head(800, 300, 200, "sw", "АВР дома", avrLinkCol)
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
		s.t(900, 444, 11, cTxt, "middle", fmt.Sprintf("всего %.0f / сегодня %.0f", st.Num("sensor.sim_avr_switches"), st.Num("sensor.sim_avr_switches_today")))
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
}
