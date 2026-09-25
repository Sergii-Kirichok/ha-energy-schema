package scada

// АВР ген. — третий АВР между «АВР дома» и Домом. Вход 1 — генератор напрямую,
// вход 2 — выход «АВР дома» (инвертор или резерв), выход — Дом. Нужен, чтобы
// запитать Дом от генератора, когда нет ни Ввода 1, ни инвертора.
// Пока реального устройства нет, карточка работает на сущностях эмулятора
// sensor.sim_avr3_{pos,mode,link}; нет сущностей → «нет связи», поток по входу 2.
const (
	avr3PosEntity  = "sensor.sim_avr3_pos"  // "main" (вход 2, от АВР дома) | "gen" (вход 1)
	avr3ModeEntity = "sensor.sim_avr3_mode" // "auto" | "manual"
	avr3LinkEntity = "sensor.sim_avr3_link" // "ok" = связь RS-485 есть
)

// Средний ряд: равные промежутки ~56 px между 5 карточками (АВР вводов 24,
// Инвертор 320, АВР дома 717, АВР ген. 973, Дом 1190). genTrunkX — ствол линии
// генератора: входит в центр АВР ген. и от узла на y=494 уходит к инвертору.
const (
	avr3X     = 973.0
	genTrunkX = avr3X + 80
)

// avr3Pos — положение АВР ген.; неизвестное = "main" (штатный путь через АВР дома).
func avr3Pos(st State) string {
	if st.State(avr3PosEntity) == "gen" {
		return "gen"
	}
	return "main"
}

func (f *frame) avr3() {
	st, s := f.st, f.s
	x, w := avr3X, 160.0
	link := st.State(avr3LinkEntity) == "ok" && !f.emuStale
	col := cGrn
	if !link {
		col = cRed
	}
	s.box(x, 300, w, 175)
	s.head(x, 300, w, "sw", "АВР ген.", col)
	mode := st.State(avr3ModeEntity)
	modeCol, modeTxt := cGrn, "АВТО"
	if mode == "manual" {
		modeCol, modeTxt = cBlu, "РУЧНОЙ"
	}
	if !link {
		modeCol, modeTxt = cRed, "НЕТ СВЯЗИ"
	}
	s.p(`<rect x="%g" y="340" width="%g" height="26" rx="13" fill="%s" fill-opacity="0.15" stroke="%s" stroke-width="1.5"/>`, x+10, w-20, modeCol, modeCol)
	s.t(x+w/2, 357, 11, modeCol, "middle", modeTxt)
	pos := avr3Pos(st)
	row := func(y float64, name, key, c string) {
		active := pos == key
		if active {
			s.p(`<rect x="%g" y="%g" width="%g" height="24" rx="6" fill="%s" fill-opacity="0.16" stroke="%s" stroke-width="1.5"/>`, x+10, y, w-20, c, c)
		} else {
			s.p(`<rect x="%g" y="%g" width="%g" height="24" rx="6" fill="none" stroke="%s" stroke-width="1"/>`, x+10, y, w-20, cBrd)
		}
		tc := cSub
		if active {
			tc = c
		}
		s.t(x+18, y+16, 12, tc, "start", name)
		switch {
		case active:
			s.t(x+w-16, y+16, 12, c, "end", "→ Дом")
		case mode == "manual" && link:
			s.t(x+w-16, y+16, 11, cSub, "end", "тап →")
			s.p(`<rect x="%g" y="%g" width="%g" height="24" rx="6" fill="transparent" style="cursor:pointer" data-act="avr3_src" data-val="%s"/>`, x+10, y, w-20, key)
		}
	}
	row(376, "От АВР дома", "main", cGrn)
	row(404, "Генератор", "gen", cOrg)
	if link {
		s.t(x+12, 464, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(x+12, 464, 10, cRed, "start", "RS-485 ✕")
	}
}
