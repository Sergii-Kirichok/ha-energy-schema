package web

import (
	"log"
	"math"
	"strings"
	"time"

	"energy-schema/internal/config"
	"energy-schema/internal/hass"
)

// Динамический ток заряда. Регистры Deye (MODBUS RTU V104): 108 «Max A Charge»
// (общий потолок, 1 А) и 128 «Grid charge the battery current» (от сети, 1 А);
// в HA это number.*_battery_max_charging_current и *_battery_grid_charging_current.
// Логика: до цели на ночь — полный «Общий лимит» (maxA), на цели — 0 (стоп),
// ночью — снова полный лимит на утро (см. charge_law.go). Раз в fullDays дней
// цель поднимается до 100 %; любое достижение 100 % обнуляет отсчёт. Все параметры —
// хелперы HA (input_number/input_boolean), регулятор включается переключателем.
const (
	chargeMaxEntity  = "number.deye_sun_30k_battery_max_charging_current"
	chargeGridEntity = "number.deye_sun_30k_battery_grid_charging_current"
	chargeAutoHelper = "input_boolean.energy_schema_charge_auto" // главный: включён — поля регулятора только у него
	// «Заряд от сети» (рег. 130) — только вручную (страница инвертора, дашборд);
	// заряжать ли от сети по времени суток решает расписание Time of Use инвертора
	gridChargeSwitch     = "switch.deye_sun_30k_battery_grid_charging"
	chargeMinA           = 1.0
	chargeZeroFloorGuess = 1.0             // остаток при 108=0 до первого измерения, А
	chargeWatchEvery     = 2 * time.Second // проверка отпечатка входов (из опроса HA, без Modbus)
	chargeIdleEvery      = time.Minute     // сверка с инвертором, если ничего не менялось
	parallelReg          = 110             // «Parallel Bat&Bat2»: =1 → инвертор умножает лимиты 108/128 на 2 (проверено 25.09: 8 А → 15,8 А факт)
)

// chargeHelpers — хелперы HA регулятора; потолки ползунков — из глубоких
// настроек аддона (config.ChargeTuning).
func chargeHelpers(t config.ChargeTuning) []hass.Helper {
	return []hass.Helper{
		{Domain: "input_boolean", ID: chargeAutoHelper[len("input_boolean."):], Name: "Заряд: авто-регулятор", Icon: "mdi:battery-sync", Initial: 1},
		{Domain: "input_number", ID: "energy_schema_charge_max_a", Name: "Заряд: общий лимит", Min: 1, Max: t.MaxALimit, Step: 1, Initial: math.Min(25, t.MaxALimit), Unit: "A", Icon: "mdi:current-dc"},
		{Domain: "input_number", ID: "energy_schema_charge_grid_a", Name: "Заряд: лимит от сети", Min: 1, Max: t.GridALimit, Step: 1, Initial: math.Min(5, t.GridALimit), Unit: "A", Icon: "mdi:transmission-tower"},
		{Domain: "input_number", ID: "energy_schema_charge_target_soc", Name: "Заряд: цель на ночь", Min: 50, Max: 100, Step: 1, Initial: 90, Unit: "%", Icon: "mdi:battery-90"},
		{Domain: "input_number", ID: "energy_schema_charge_full_days", Name: "Заряд: полный раз в", Min: 1, Max: t.FullDaysMax, Step: 1, Initial: math.Min(14, t.FullDaysMax), Unit: "д", Icon: "mdi:calendar-refresh"},
		{Domain: "input_boolean", ID: "energy_schema_charge_full_now", Name: "Заряд: полный сейчас", Icon: "mdi:battery-charging-100"},
		{Domain: "input_button", ID: chargeApplyBt[len("input_button."):], Name: "Заряд: применить", Icon: "mdi:content-save"},
	}
}

const chargeFullNowEntity = "input_boolean.energy_schema_charge_full_now"

func (s *Server) ensureChargeHelpers() {
	for _, h := range chargeHelpers(s.cfg.Charge) {
		if created, err := s.client.EnsureHelper(h); err != nil {
			log.Printf("charge: helper %s.%s: %v", h.Domain, h.ID, err)
		} else if created {
			log.Printf("charge: created %s.%s", h.Domain, h.ID)
		}
	}
	// уже созданные раньше в режиме box — перевести на компактный slider;
	// режим читаем из атрибутов состояния (store наполняется первым опросом)
	for i := 0; i < 30 && !s.store.Available(chargeFullNowEntity); i++ {
		time.Sleep(time.Second)
	}
	for _, h := range chargeHelpers(s.cfg.Charge) {
		if h.Domain != "input_number" {
			continue
		}
		e := "input_number." + h.ID
		cur := s.store.Attr(e, "mode")
		if cur == "" {
			continue
		}
		// границы из кода — источник правды: при расхождении (сузили диапазон
		// в новой версии) хелпер обновляется так же, как при смене режима
		if s.store.AttrNum(e, "min") != h.Min || s.store.AttrNum(e, "max") != h.Max || s.store.AttrNum(e, "step") != h.Step {
			cur = "" // заставит EnsureNumberMode отправить полный набор полей
		}
		if ch, err := s.client.EnsureNumberMode(h, cur); err != nil {
			log.Printf("charge: %v", err)
		} else if ch {
			log.Printf("charge: input_number.%s → box %g..%g", h.ID, h.Min, h.Max)
		}
	}
}

// loopCharge — раз в минуту: читает хелперы и SOC, пишет лимиты в инвертор
// (только при изменении), публикует уставку/режим/дату полного заряда.
func (s *Server) loopCharge() {
	s.ensureChargeHelpers()
	st := loadChargeState(chargeFile)
	lastMax, lastGrid := -1.0, -1.0
	lastSig, lastRun := "", time.Time{}
	if s.applied.empty() { // первый запуск с черновиком/применением: стартуем с текущих хелперов
		s.applied.apply(s.store.Num)
	}
	lastBtn := s.store.State(chargeApplyBt)
	for {
		// «Применить» на дашборде: черновик хелперов → применённые настройки
		if b := s.store.State(chargeApplyBt); b != lastBtn {
			if lastBtn != "" && b != "" {
				log.Printf("charge: applied from dashboard: %v", s.applied.apply(s.store.Num))
			}
			lastBtn = b
		}
		// событийно: отпечаток входов берётся из общего опроса HA (локально,
		// инвертор не трогаем); к инвертору идём при изменении или раз в минуту
		if sig := s.chargeSignature(); sig != lastSig || time.Since(lastRun) >= chargeIdleEvery {
			s.chargeTick(&st, &lastMax, &lastGrid)
			s.publishPending()
			lastSig, lastRun = sig, time.Now()
		}
		time.Sleep(chargeWatchEvery)
	}
}

// chargeSignature — всё, от чего зависит уставка: хелперы, SOC, баланс ячеек.
func (s *Server) chargeSignature() string {
	sig := s.store.State("sensor.deye_sun_30k_battery") + "|" + s.store.Attr("sensor.energy_schema_bms_balance", "level") +
		"|" + pvBucket(s.store.Num("sensor.deye_sun_30k_pv_power"), s.cfg.Charge) + "|" + s.store.State(gridChargeSwitch)
	for _, h := range chargeHelpers(s.cfg.Charge) {
		sig += "|" + s.store.State(h.Domain+"."+h.ID)
	}
	return sig
}

func (s *Server) chargeTick(st *chargeState, lastMax, lastGrid *float64) {
	s.mb.Lock() // не пересекаться со страницей инвертора (чтение-запись-сверка)
	defer s.mb.Unlock()
	num := func(e string) float64 { return s.store.Num(e) }
	// SOC — BMS через Solarman (регистр 214, тот же, что 10005)
	soc := num("sensor.deye_sun_30k_battery")
	if !s.store.Available("sensor.deye_sun_30k_battery") {
		return
	}
	cfgNum := func(e string) float64 { return s.applied.num(e, s.store.Num) } // применённые, не черновик
	fullDays := cfgNum("input_number.energy_schema_charge_full_days")
	// первый запуск: отсчёт с сегодня, а не «полный заряд немедленно» — иначе
	// регулятор молча тянет к 100 % в первый же день вместо цели на ночь
	if st.LastFull.IsZero() || soc >= 100 && st.LastFull.Format("2006-01-02") != time.Now().Format("2006-01-02") {
		st.LastFull = time.Now() // раз в сутки, а не каждую минуту на 100 %
		st.save(chargeFile)
	}
	nextFull := st.LastFull.Add(time.Duration(fullDays*24) * time.Hour)
	fullDue := fullDays > 0 && !time.Now().Before(nextFull)
	// внеплановый полный заряд по кнопке; при 100 % кнопка гасится сама
	if s.store.On(chargeFullNowEntity) {
		fullDue = true
		if soc >= 100 {
			_ = s.client.CallService("input_boolean", "turn_off", map[string]any{"entity_id": chargeFullNowEntity})
		}
	}
	in := chargeInput{SOC: soc, MaxA: cfgNum("input_number.energy_schema_charge_max_a"),
		TargetSOC: cfgNum("input_number.energy_schema_charge_target_soc"),
		FullDue:   fullDue, CellLevel: s.store.Attr("sensor.energy_schema_bms_balance", "level")}
	// ночь — по генерации И по солнцу: пасмурный день (PV ~1 кВт) — не ночь
	// (12.10: 10:41–10:45 ложная «ночь» → лишние переключения и записи).
	// Недоступный PV — оставляем прежний режим (0 не значит «ночь»).
	if s.store.Available("sensor.deye_sun_30k_pv_power") {
		st.Night = nightHyst(num("sensor.deye_sun_30k_pv_power"), st.Night, s.cfg.Charge) && sunLow(s.store)
	}
	in.Holding = st.Holding
	in.Tune = s.cfg.Charge
	// остаток тока при 108=0 доберёт ещё немного до вечера — удержание раньше на
	// этот запас (панели ЮВ: полезное солнце заканчивается ~за час до заката)
	floorA := st.ZeroFloorA
	if !st.ZeroChecked && !st.ZeroBroken {
		floorA = chargeZeroFloorGuess // ponytail: наблюдение 25–27.09 (1.04 А); заменится измеренным
	}
	margin := driftMargin(floorA, num("sensor.deye_sun_30k_battery_voltage"),
		s.store.HoursUntil("sun.sun", "next_setting")-1, battKWh(s))
	if !fullDue {
		in.TargetSOC -= margin
	}
	in.Night = st.Night
	auto := s.store.On(chargeAutoHelper)
	if auto && s.ovr.clear() {
		log.Println("charge: auto-regulator on — manual overrides from the inverter page cleared")
	}
	if in.MaxA <= 0 || in.TargetSOC <= 0 {
		return // хелперы ещё не созданы / не прочитаны
	}
	amps, mode := chargeSetpoint(in)
	if !auto {
		// ручной режим: без спада и удержания — просто «Общий лимит». Иначе
		// в инверторе навсегда остался бы последний авто-ответ (например, 0 = стоп)
		amps, mode = in.MaxA, "manual"
	}
	st.Holding = strings.Contains(mode, "/hold")
	defer s.publishWrites()
	nf := "—"
	if !st.LastFull.IsZero() {
		nf = nextFull.Format("2006-01-02")
	}
	_ = s.client.SetState("sensor.energy_schema_charge_next_full", nf, map[string]any{"friendly_name": "Заряд: следующий 100 %",
		"icon": "mdi:calendar-check", "last_full": st.LastFull.Format(time.RFC3339)})
	// 102..130 одним чтением: ёмкость и пороги SOC (для схемы — свежее опроса
	// Solarman раз в 5 мин), фактические лимиты в инверторе и множитель каналов
	// («Parallel Bat&Bat2»=1 → ×2). Без известного множителя не пишем вовсе
	// (иначе можно случайно дать вдвое больший ток).
	regs, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, 102, 29)
	if err == nil {
		st.Factor = 1 + float64(regs[parallelReg]&1)
		*lastMax = float64(regs[108]) // сверяемся с инвертором, а не с памятью
		*lastGrid = float64(regs[128])
		s.noteBattRegs(regs)
	} else if st.Factor == 0 {
		log.Println("charge: read 102..130:", err)
		return
	}
	defer func() {
		s.publishLimit(st.Factor, *lastMax, amps, mode, soc, fullDue, auto, st.ZeroFloorA, in.TargetSOC)
	}()
	grid := cfgNum("input_number.energy_schema_charge_grid_a")
	if auto {
		s.fullCharge(st, fullDue, soc, in.MaxA, grid) // прогноз, при нехватке солнца — сеть через выключение ToU
	} else {
		s.touRestore(st) // ручной режим: ToU — как было
	}
	maxReg, gridReg := regValue(amps, st.Factor), regValue(grid, st.Factor)
	// самопроверка «0 = стоп»: не документировано, что Deye трактует 0 именно
	// так. Если после записи 0 заряд не прекратился — откат на 1 и запоминаем.
	if maxReg == 0 && st.ZeroBroken {
		maxReg, mode = 1, mode+"/zero-unsupported"
	}
	// 0 уже стоит в инверторе (например, после рестарта аддона), а остаток
	// ещё не измерен — запускаем проверку и без новой записи
	if maxReg == 0 && *lastMax == 0 && st.ZeroSince.IsZero() && !st.ZeroChecked && !st.ZeroBroken {
		st.ZeroSince = time.Now()
	}
	if maxReg == 0 && *lastMax == 0 && !st.ZeroSince.IsZero() {
		chargingA := -num("sensor.deye_sun_30k_battery_current") // «−» = заряд
		// проверка честная только когда батарее есть что брать: не полная и есть излишек PV
		surplusW := num("sensor.deye_sun_30k_pv_power") - num("sensor.deye_sun_30k_load_power")
		// и без заряда от сети — иначе ток из сети даст ложное «broken» навсегда
		testable := soc < 99 && surplusW > 1500 && s.store.State(gridChargeSwitch) != "on"
		switch zeroVerdict(time.Since(st.ZeroSince), chargingA, testable) {
		case "broken":
			st.ZeroBroken = true
			st.save(chargeFile)
			log.Printf("charge: reg108=0 did NOT stop charging (%.1f A after %s) — falling back to 1", chargingA, time.Since(st.ZeroSince).Round(time.Second))
			maxReg = 1
		case "ok":
			st.ZeroFloorA, st.ZeroChecked = math.Max(0, chargingA), true
			st.save(chargeFile)
			log.Printf("charge: reg108=0 honoured — residual %.2f A (inverter floor, not a full stop)", chargingA)
			st.ZeroSince = time.Time{} // проверено, больше не смотрим
		}
	}
	// регистр в энергонезависимой памяти: некритичные изменения не чаще раза
	// в chargeMinWriteGap; сразу — стоп (0), смена день/ночь,
	// защита ячеек и полный заряд; в ручном режиме — когда ползунок отпустили
	urgent := maxReg == 0 || strings.Contains(mode, "/cells") || strings.HasPrefix(mode, "full") ||
		strings.HasPrefix(mode, "night-ready") != strings.HasPrefix(st.LastMode, "night-ready")
	st.LastMode = mode
	due := urgent || time.Since(st.Wrote108) >= chargeMinWriteGap
	if !auto {
		due = st.settled("max", in.MaxA) // ручной режим: пишем, когда ползунок отпустили
	}
	if maxReg != *lastMax && due && !s.ovr.get("max_charge") {
		if err := s.client.CallService("number", "set_value", map[string]any{"entity_id": chargeMaxEntity, "value": maxReg}); err != nil {
			log.Println("charge: set max:", err)
		} else {
			log.Printf("charge: max %.0f A → reg108=%.0f (x%.0f), soc %.0f%%, %s", amps, maxReg, st.Factor, soc, mode)
			if maxReg == 0 && !st.ZeroBroken {
				st.ZeroSince = time.Now()
			}
			*lastMax, st.Wrote108 = maxReg, time.Now()
			s.writes.add("reg108", 1)
		}
	}
	if gridReg != *lastGrid && st.settled("grid", grid) && !s.ovr.get("grid_a") {
		if err := s.client.CallService("number", "set_value", map[string]any{"entity_id": chargeGridEntity, "value": gridReg}); err != nil {
			log.Println("charge: set grid:", err)
		} else {
			log.Printf("charge: grid %.0f A → reg128=%.0f", grid, gridReg)
			*lastGrid = gridReg
			s.writes.add("reg128", 1)
		}
	}
}
