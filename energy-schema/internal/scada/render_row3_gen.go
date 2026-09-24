package scada

import (
	"fmt"
	"time"
)

// generator — нижний ряд, карточка Генератор.
func (f *frame) generator() {
	st, s := f.st, f.s
	genRun, emuStale := f.genRun, f.emuStale

	// Генератор — компактно: верх = ключевые индикаторы, ниже наработка/масло и фазы
	s.box(956, 520, 464, 280)
	gk := "gen"
	gtc, gtxt := cGry, "ВЫКЛЮЧЕН"
	if genRun {
		gk, gtc, gtxt = "genrun", cGrn, "РАБОТАЕТ"
	}
	s.head(956, 520, 464, gk, "Генератор", "")
	genMode := st.State("sensor.sim_gen_mode")
	genAuto := genMode == "auto"
	sig := st.State("sensor.sim_gen_start_signal") == "on"
	// шапка — все объекты на одном уровне (центр y=543, базовая линия текста 547),
	// равные интервалы 16px: [режим] · [значок инвертора-сигнал] · [АКБ] … статус · точка
	// цвет режима как у АВР: АВТО — синий (автомат, мы им не управляем),
	// РУЧНОЙ — серый (управление у генератора / монитор)
	mCol, mTxt := cBlu, "АВТО"
	if !genAuto {
		mCol, mTxt = cGry, "РУЧНОЙ"
	}
	s.p(`<rect x="1090" y="534" width="62" height="18" rx="6" fill="%s" fill-opacity="0.12" stroke="%s" stroke-width="1.2"/>`, mCol, mCol)
	s.t(1121, 547, 12, mCol, "middle", mTxt)
	// пусковой сигнал от инвертора — значок инвертора (DC→AC): серый нет / зелёный есть
	sigCol := cGry
	if sig {
		sigCol = cGrn
	}
	s.p(`<rect x="1168" y="535" width="24" height="16" rx="2" fill="none" stroke="%s" stroke-width="1.6"/>`, sigCol)
	s.p(`<line x1="1180" y1="537" x2="1180" y2="549" stroke="%s" stroke-width="1.3"/>`, sigCol)
	s.p(`<line x1="1172" y1="540" x2="1177" y2="540" stroke="%s" stroke-width="1.3"/><line x1="1172" y1="546" x2="1177" y2="546" stroke="%s" stroke-width="1.3"/>`, sigCol, sigCol)
	s.p(`<path d="M 1182 543 c 1.2,-3.5 3.3,-3.5 4.5,0 c 1.2,3.5 3.3,3.5 4.5,0" fill="none" stroke="%s" stroke-width="1.3"/>`, sigCol)
	// АКБ запуска — значок батареи + значение
	bv := st.Num("sensor.sim_gen_batt_v")
	bvc := cGrn
	if bv > 0 && bv < 12.0 {
		bvc = cOrg
	}
	if bv > 0 && bv < 11.5 {
		bvc = cRed
	}
	s.p(`<rect x="1208" y="537" width="20" height="12" rx="2" fill="none" stroke="%s" stroke-width="1.5"/>`, bvc)
	s.p(`<rect x="1228" y="540" width="3" height="6" rx="1" fill="%s"/>`, bvc)
	s.t(1234, 547, 12, bvc, "start", fmt.Sprintf("%.1fВ", bv))
	s.t(1378, 547, 14, gtc, "end", gtxt)
	s.dot(1404, 543, 6, gtc)

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
	s.p(`<line x1="1192" y1="558" x2="1192" y2="786" stroke="%s" stroke-width="1"/>`, cBrd)

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
	// связь управления генератором (RS-485 через наш блок) — в левом нижнем углу
	if st.Available("sensor.sim_gen_state") && !emuStale {
		s.t(972, 790, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(972, 790, 10, cRed, "start", "RS-485 ✕")
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
	// кольцо: в центре — остаток, под кольцом — общий интервал (подняты выше на 18)
	s.ringTimer(1262, 674, 32, oilFr, ringCol(oilFr), "замена масла", fmt.Sprintf("%.0f ч", oilRem))
	s.t(1262, 720, 9, cSub, "middle", fmt.Sprintf("из %.0f ч", oilInt))
	s.ringTimer(1352, 674, 32, svcFr, ringCol(svcFr), "ТО", fmt.Sprintf("%.0f ч", svcRem))
	s.t(1352, 720, 9, cSub, "middle", fmt.Sprintf("из %.0f ч", svcInt))

	// ПРАВО (под кольцами масло/ТО): наработка + последний запуск
	lastAgo := firstNum("input_number.gen_last_run_h", "sensor.sim_gen_last_run_h")
	lastMin := firstNum("input_number.gen_last_run_min", "sensor.sim_gen_last_run_min")
	s.t(1306, 736, 12, cSub, "middle", fmt.Sprintf("Наработка: %.1f ч", runtime))
	// последний запуск: абсолютная метка времени + длительность (Xч Yмин)
	lastTime := clockNow().Add(-time.Duration(lastAgo * float64(time.Hour)))
	durTxt := fmt.Sprintf("%d мин", int(lastMin))
	if int(lastMin) >= 60 {
		durTxt = fmt.Sprintf("%d ч %d мин", int(lastMin)/60, int(lastMin)%60)
	}
	s.t(1306, 762, 10, cSub, "middle", fmt.Sprintf("%s · длит. %s", lastTime.Format("2006-01-02 15:04"), durTxt))
	// таймер обратного отсчёта до периодического (планового) запуска
	if nextTs := firstNum("input_number.gen_next_run_ts", "sensor.sim_gen_next_run_ts"); nextTs > 0 {
		rem := int(nextTs - float64(clockNow().Unix()))
		if rem < 0 {
			rem = 0
		}
		ct := fmt.Sprintf("%d ч %d мин", (rem%86400)/3600, (rem%3600)/60)
		if d := rem / 86400; d > 0 {
			ct = fmt.Sprintf("%d д %d ч %d мин", d, (rem%86400)/3600, (rem%3600)/60)
		}
		s.t(1306, 778, 10, cAmb, "middle", "до планового пуска: "+ct)
	}
}
