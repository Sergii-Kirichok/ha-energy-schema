package scada

import (
	"fmt"
	"math"
)

// battery — нижний ряд, карточка АКБ. Оценка «ясного дня» (clearDay) нужна и
// карточке Солнце — сохраняем её в кадре.
func (f *frame) battery() {
	st, s, cfg := f.st, f.s, f.cfg
	bp, load, pvtot := f.bp, f.load, f.pvtot

	// ===================== ROW 3 =====================
	// Батарея
	s.box(24, 520, 300, 280)
	bAlarm := st.On("binary_sensor.deye_sun_30k_battery_fault") || st.On("binary_sensor.deye_sun_30k_battery_alarm")
	bStatCol := cGrn
	if bAlarm {
		bStatCol = cRed
	}
	s.head(24, 520, 300, "batt", "АКБ", bStatCol)
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
	be := "sensor.deye_sun_30k_battery"
	// прежний тип гейджа: дуга заливается ТОЛЬКО до текущего заряда (цвет по уровню),
	// дальше тёмная. Просто уменьшен, чтобы выноски не вылезали за карточку. Пик
	// (красная капля) и минимум (голубая) заряда за 12 ч — выносками на самом гейдже.
	bcx, bcy, br := 174.0, 606.0, 50.0
	s.arc(bcx, bcy, br, 180, 0, "#23272f", 12)
	socCol := cGrn
	if soc < 20 {
		socCol = cRed
	} else if soc < 50 {
		socCol = cAmb
	}
	s.arc(bcx, bcy, br, 180, gAng(soc, 100), socCol, 12)
	s.marker(bcx, bcy, br, gAng(soc, 100), 6)
	if pkMax := st.Max12h(be); pkMax > 1 {
		a := gAng(pkMax, 100)
		s.markerMax(bcx, bcy, br, a, br*0.13, cRed)
		s.markerLabel(bcx, bcy, br, a, fmt.Sprintf("%.0f", pkMax), cRed)
	}
	if mn, ok := st.Min12h(be); ok && mn > 0 {
		a := gAng(mn, 100)
		s.markerMax(bcx, bcy, br, a, br*0.13, cCyn)
		s.markerLabel(bcx, bcy, br, a, fmt.Sprintf("%.0f", mn), cCyn)
	}
	s.t(bcx, bcy-2, 24, cTxt, "middle", fmt.Sprintf("%.0f%%", soc))

	// ток — слева от спидометра, SOH — справа (симметрично)
	s.t(54, 596, 11, cSub, "middle", "ток")
	s.t(54, 616, 15, cTxt, "middle", fmt.Sprintf("%.1f А", st.Num("sensor.deye_sun_30k_battery_current")))
	s.t(294, 596, 11, cSub, "middle", "SOH")
	s.t(294, 616, 15, cTxt, "middle", fmt.Sprintf("%.0f%%", batterySOH(st)))

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
	soh := batterySOH(st)
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
	batCol := cTxt
	// красный, если батареи в одиночку не хватит ДО НАЧАЛА ГЕНЕРАЦИИ (по прогнозу
	// с учётом полей и истории), пока солнце практически не даёт
	if pvKW < 0.3 {
		if hGen := hoursToGen(st); hGen > 0 && batH < hGen {
			batCol = cRed
		}
	}
	s.t(308, 716, 20, batCol, "end", hfmt(batH))
	s.t(40, 742, 13, cSub, "start", "Прогноз на 48 ч")
	s.t(308, 742, 20, cGrn, "end", hfmt(autoH))
	// откуда берётся прогноз генерации + детали симуляции
	s.t(174, 762, 9, cSub, "middle", aNote)
	if avg, n := st.PVRecent(); n > 0 {
		s.t(174, 776, 9, cSub, "middle", fmt.Sprintf("прогноз по факту: %d дн · ясный ~%.0f · средн %.0f кВт·ч/сут", n, clearDay, avg))
	}
	s.t(174, 790, 9, cSub, "middle", fmt.Sprintf("ёмкость %.0f · SOH %.0f%% → %.0f кВт·ч · отключ. %.0f%%", capNom, soh, capKWh, cutoff))
	f.clearDay = clearDay
}

// sun — нижний ряд, карточка Солнышко (погода, три поля PV, суммарная шкала).
func (f *frame) sun() {
	st, s, cfg := f.st, f.s, f.cfg
	pvtot, clearDay := f.pvtot, f.clearDay

	// Солнышко
	s.box(360, 520, 560, 280)
	s.head(360, 520, 560, "sun", "Солнышко", "")
	// текущая погода: иконка состояния + значения, по центру шапки
	if w := "weather.forecast_home_assistant"; st.State(w) != "" && st.State(w) != "unavailable" {
		// облачность и иконку берём из Open-Meteo (точнее Met.no, который сильно
		// врёт); Met.no — фолбэк + источник температуры/ветра
		cloudPct := st.AttrNum(w, "cloud_coverage")
		cond := st.State(w)
		if c, ok := st.SolarCloudNow(); ok {
			cloudPct = c
			cond = cloudCond(c)
		}
		s.wicon(cond, 548, 540)
		s.t(566, 548, 14, cTxt, "start", fmt.Sprintf("%.0f°C · обл %.0f%% · %.1f м/с",
			st.AttrNum(w, "temperature"), cloudPct, st.AttrNum(w, "wind_speed")))
	}
	// сегодня: факт / прогноз на день — кратко. Облачность берём «живую» (как ниже
	// в шапке), чтобы прогноз сегодня и завтра (в карточке батареи) различались.
	todayProd := st.Num("sensor.deye_sun_30k_today_production")
	todayKWhTxt := fmt.Sprintf("сегодня %.0f кВт·ч", todayProd)
	if _, fcLeft, _, _, ok := st.SolarTotals(); ok {
		// прогноз на день = факт + прогноз на остаток (само корректируется, не
		// показывает завышенную утреннюю оценку, когда день пошёл иначе)
		todayKWhTxt = fmt.Sprintf("сегодня %.0f / %.0f кВт·ч", todayProd, todayProd+fcLeft)
	} else if st.Available("weather.forecast_home_assistant") {
		// облачность сегодня — среднее по светлому дню из почасового прогноза
		// (точнее, чем мгновенная «живая» или огрублённый дневной condition).
		cloudT, okc := st.CloudForDay(0)
		if !okc {
			cloudT = st.AttrNum("weather.forecast_home_assistant", "cloud_coverage")
			if cloudT <= 0 {
				if _, c0, ok0 := st.ForecastInfo(0); ok0 {
					cloudT = condCloud(c0)
				}
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
		s.gauge(gx[i], 646, pvR, pw/1000, pvFieldMax, []band{{2, cAmb}, {10, cGrn}, {pvFieldMax, cRed}}, kw(pw), cfg.PVLabels[i])
		// суточная выработка стринга — мелким серым над текущей мощностью
		s.t(gx[i], 626, 10, cSub, "middle", fmt.Sprintf("%.1f кВт·ч", st.DayEnergy(pe)))
		// шкала тиками радиально (как у Дома): концы 0/13 + переходы зон 2/10
		for _, tk := range []float64{0, 2, 10, pvFieldMax} {
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
	// пик суммарной генерации за сегодня: красная капля над шкалой + значение РЯДОМ
	// с каплей (информативнее, чем отдельная подпись «Max:»).
	if pmax := st.DayMax("sensor.deye_sun_30k_pv_power"); pmax > 50 {
		s.barMax(380, 722, 520, pmax/1000, pvInputMaxKW, cRed)
		mv := pmax / 1000
		if mv > pvInputMaxKW {
			mv = pvInputMaxKW
		}
		mx := 380 + 520*mv/pvInputMaxKW
		if mx < 840 {
			s.t(mx+10, 714, 13, cRed, "start", kw(pmax))
		} else {
			s.t(mx-10, 714, 13, cRed, "end", kw(pmax))
		}
	}
	// шкала PV-входа до 39 кВт (шильдик); 33–39 — верхняя зона
	s.bar(380, 722, 520, 44, pvtot/1000, pvInputMaxKW, []band{{cfg.PVT1, cAmb}, {cfg.PVT2, cGrn}, {cfg.PVT3, cOrg}, {cfg.PVMax, cRed}, {pvInputMaxKW, cRed2}}, kw(pvtot))
	// границы шкалы + значения переходов зон
	s.t(382, 779, 10, cSub, "start", "0")
	s.barTicks(380, 779, 520, pvInputMaxKW, []float64{cfg.PVT1, cfg.PVT2, cfg.PVT3, cfg.PVMax}) // 5 · 20 · 25 · 33
	s.t(898, 779, 10, cSub, "end", fmt.Sprintf("%.0f кВт", pvInputMaxKW))
}

// batterySOH prefers the real BMS value (register 10006, published by the
// add-on as sensor.energy_schema_bms_soh) over the Solarman-computed sensor,
// which is off for HV packs (assumes 48 V nominal when counting cycles).
func batterySOH(st State) float64 {
	if st.Available("sensor.energy_schema_bms_soh") {
		return st.Num("sensor.energy_schema_bms_soh")
	}
	return st.Num("sensor.deye_sun_30k_battery_soh")
}
