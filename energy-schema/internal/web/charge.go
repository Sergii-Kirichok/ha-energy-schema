package web

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"time"

	"energy-schema/internal/hass"
)

// Динамический ток заряда. Регистры Deye (MODBUS RTU V104): 108 «Max A Charge»
// (общий потолок, 1 А) и 128 «Grid charge the battery current» (от сети, 1 А);
// в HA это number.*_battery_max_charging_current и *_battery_grid_charging_current.
// Логика: ниже taperSOC — заряд по максимуму (maxA); от taperSOC к целевому SOC
// ток линейно падает до 1 А; выше цели — держим 1 А. Раз в fullDays дней цель
// поднимается до 100 %; любое достижение 100 % обнуляет отсчёт. Все параметры —
// хелперы HA (input_number/input_boolean), регулятор включается переключателем.
const (
	chargeMaxEntity  = "number.deye_sun_30k_battery_max_charging_current"
	chargeGridEntity = "number.deye_sun_30k_battery_grid_charging_current"
	chargeFile       = "/data/charge.json"
	chargeMinA       = 1.0
	chargeCellCapA   = 5.0             // при разбалансе ячеек — не выше этого…
	chargeCellCapSOC = 70.0            // …но только от этого SOC: ниже батарея выравнивается сама, ток не режем
	chargeWatchEvery = 2 * time.Second // проверка отпечатка входов (из опроса HA, без Modbus)
	chargeIdleEvery  = time.Minute     // сверка с инвертором, если ничего не менялось
	parallelReg      = 110             // «Parallel Bat&Bat2»: =1 → инвертор умножает лимиты 108/128 на 2 (проверено 25.09: 8 А → 15,8 А факт)
)

var chargeHelpers = []hass.Helper{
	{Domain: "input_boolean", ID: "energy_schema_charge_auto", Name: "Заряд: авто-регулятор", Icon: "mdi:battery-sync", Initial: 1},
	{Domain: "input_number", ID: "energy_schema_charge_max_a", Name: "Заряд: общий лимит", Min: 1, Max: 30, Step: 1, Initial: 25, Unit: "A", Icon: "mdi:current-dc"},
	{Domain: "input_number", ID: "energy_schema_charge_grid_a", Name: "Заряд: лимит от сети", Min: 1, Max: 15, Step: 1, Initial: 5, Unit: "A", Icon: "mdi:transmission-tower"},
	{Domain: "input_number", ID: "energy_schema_charge_taper_soc", Name: "Заряд: снижать ток с", Min: 50, Max: 99, Step: 1, Initial: 80, Unit: "%", Icon: "mdi:battery-70"},
	{Domain: "input_number", ID: "energy_schema_charge_target_soc", Name: "Заряд: цель на ночь", Min: 50, Max: 100, Step: 1, Initial: 90, Unit: "%", Icon: "mdi:battery-90"},
	{Domain: "input_number", ID: "energy_schema_charge_full_days", Name: "Заряд: полный раз в", Min: 1, Max: 30, Step: 1, Initial: 14, Unit: "д", Icon: "mdi:calendar-refresh"},
	{Domain: "input_boolean", ID: "energy_schema_charge_full_now", Name: "Заряд: полный сейчас", Icon: "mdi:battery-charging-100"},
}

const chargeFullNowEntity = "input_boolean.energy_schema_charge_full_now"

// chargeInput — всё, что нужно закону регулирования (чистая функция, тестируется).
type chargeInput struct {
	SOC, MaxA, TaperSOC, TargetSOC float64
	FullDue                        bool
	CellLevel                      string // "ok" | "warn" | "bad" (см. cellVerdict)
	Night                          bool   // нет генерации — стоим на полном токе к утру
}

// chargeSetpoint возвращает ток заряда (целые амперы, ≥1) и режим для сенсора.
func chargeSetpoint(in chargeInput) (float64, string) {
	// ночью ток не нужен, а утро должно начаться с полного лимита даже без
	// связи с HA — поэтому на ночь пишем «Общий лимит», а не 0/спад
	if in.Night {
		return math.Max(chargeMinA, math.Round(in.MaxA)), "night-ready"
	}
	// Полный заряд — не «тянуть к 100 % большим током», а добрать минимальным
	// после цели на ночь: медленный хвост и есть балансировка ячеек.
	target := in.TargetSOC
	mode := "night"
	if in.FullDue {
		mode = "full"
	}
	var a float64
	switch {
	case in.SOC < in.TaperSOC || in.TaperSOC >= target:
		a, mode = in.MaxA, "max"
	case in.SOC < target:
		a = in.MaxA * (target - in.SOC) / (target - in.TaperSOC)
		mode += "/taper"
	case in.FullDue && in.SOC < 100:
		a, mode = chargeMinA, "full/trickle"
	default:
		a, mode = 0, mode+"/hold" // 0 = стоп заряда: даже 1 в регистре (×2 канала) набивает до 100 % за день
	}
	if in.CellLevel == "bad" && in.SOC >= chargeCellCapSOC && a > chargeCellCapA {
		a, mode = chargeCellCapA, mode+"/cells"
	}
	if a <= 0 {
		return 0, mode
	}
	return math.Max(chargeMinA, math.Round(a)), mode
}

type chargeState struct {
	LastFull   time.Time `json:"last_full"`
	Factor     float64   `json:"-"`           // множитель регистр→факт (1 или 2), 0 = ещё не прочитан
	ZeroBroken bool      `json:"zero_broken"` // проверено: Deye не останавливает заряд по 108=0
	ZeroSince  time.Time `json:"-"`           // когда записали 0 (для самопроверки)
	Night      bool      `json:"-"`           // ночной режим (гистерезис по генерации)
}

func loadChargeState(path string) chargeState {
	var st chargeState
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func (st chargeState) save(path string) {
	b, _ := json.Marshal(st)
	_ = os.WriteFile(path, b, 0o644)
}

func (s *Server) ensureChargeHelpers() {
	for _, h := range chargeHelpers {
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
	for _, h := range chargeHelpers {
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
			log.Printf("charge: input_number.%s → slider %g..%g", h.ID, h.Min, h.Max)
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
	for {
		// событийно: отпечаток входов берётся из общего опроса HA (локально,
		// инвертор не трогаем); к инвертору идём при изменении или раз в минуту
		if sig := s.chargeSignature(); sig != lastSig || time.Since(lastRun) >= chargeIdleEvery {
			s.chargeTick(&st, &lastMax, &lastGrid)
			lastSig, lastRun = sig, time.Now()
		}
		time.Sleep(chargeWatchEvery)
	}
}

// chargeSignature — всё, от чего зависит уставка: хелперы, SOC, баланс ячеек.
func (s *Server) chargeSignature() string {
	sig := s.store.State("sensor.deye_sun_30k_battery") + "|" + s.store.Attr("sensor.energy_schema_bms_balance", "level") +
		"|" + pvBucket(s.store.Num("sensor.deye_sun_30k_pv_power"))
	for _, h := range chargeHelpers {
		sig += "|" + s.store.State(h.Domain+"."+h.ID)
	}
	return sig
}

func (s *Server) chargeTick(st *chargeState, lastMax, lastGrid *float64) {
	num := func(e string) float64 { return s.store.Num(e) }
	// SOC — BMS через Solarman (регистр 214, тот же, что 10005)
	soc := num("sensor.deye_sun_30k_battery")
	if !s.store.Available("sensor.deye_sun_30k_battery") {
		return
	}
	fullDays := num("input_number.energy_schema_charge_full_days")
	// первый запуск: отсчёт с сегодня, а не «полный заряд немедленно» — иначе
	// регулятор молча тянет к 100 % в первый же день вместо цели на ночь
	if soc >= 100 || st.LastFull.IsZero() {
		st.LastFull = time.Now()
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
	in := chargeInput{SOC: soc, MaxA: num("input_number.energy_schema_charge_max_a"),
		TaperSOC: num("input_number.energy_schema_charge_taper_soc"), TargetSOC: num("input_number.energy_schema_charge_target_soc"),
		FullDue: fullDue, CellLevel: s.store.Attr("sensor.energy_schema_bms_balance", "level")}
	st.Night = nightHyst(num("sensor.deye_sun_30k_pv_power"), st.Night)
	in.Night = st.Night
	auto := s.store.On("input_boolean.energy_schema_charge_auto")
	if in.MaxA <= 0 || in.TargetSOC <= 0 {
		return // хелперы ещё не созданы / не прочитаны
	}
	amps, mode := chargeSetpoint(in)
	if !auto {
		// ручной режим: без спада и удержания — просто «Общий лимит». Иначе
		// в инверторе навсегда остался бы последний авто-ответ (например, 0 = стоп)
		amps, mode = in.MaxA, "manual"
	}
	nf := "—"
	if !st.LastFull.IsZero() {
		nf = nextFull.Format("2006-01-02")
	}
	_ = s.client.SetState("sensor.energy_schema_charge_next_full", nf, map[string]any{"friendly_name": "Заряд: следующий 100 %",
		"icon": "mdi:calendar-check", "last_full": st.LastFull.Format(time.RFC3339)})
	// 108..110 одним чтением: фактический лимит в инверторе и множитель каналов
	// («Parallel Bat&Bat2»=1 → ×2). Без известного множителя не пишем вовсе
	// (иначе можно случайно дать вдвое больший ток).
	regs, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, 108, 3)
	if err == nil {
		st.Factor = 1 + float64(regs[parallelReg]&1)
		*lastMax = float64(regs[108]) // сверяемся с инвертором, а не с памятью
	} else if st.Factor == 0 {
		log.Println("charge: read 108..110:", err)
		return
	}
	defer func() { s.publishLimit(st.Factor, *lastMax, amps, mode, soc, fullDue, auto) }()
	grid := num("input_number.energy_schema_charge_grid_a")
	maxReg, gridReg := regValue(amps, st.Factor), regValue(grid, st.Factor)
	// самопроверка «0 = стоп»: не документировано, что Deye трактует 0 именно
	// так. Если после записи 0 заряд не прекратился — откат на 1 и запоминаем.
	if maxReg == 0 && st.ZeroBroken {
		maxReg, mode = 1, mode+"/zero-unsupported"
	}
	if maxReg == 0 && *lastMax == 0 && !st.ZeroSince.IsZero() {
		chargingA := -num("sensor.deye_sun_30k_battery_current") // «−» = заряд
		// проверка честная только когда батарее есть что брать: не полная и есть излишек PV
		surplusW := num("sensor.deye_sun_30k_pv_power") - num("sensor.deye_sun_30k_load_power")
		testable := soc < 99 && surplusW > 1500
		switch zeroVerdict(time.Since(st.ZeroSince), chargingA, testable) {
		case "broken":
			st.ZeroBroken = true
			st.save(chargeFile)
			log.Printf("charge: reg108=0 did NOT stop charging (%.1f A after %s) — falling back to 1", chargingA, time.Since(st.ZeroSince).Round(time.Second))
			maxReg = 1
		case "ok":
			log.Printf("charge: reg108=0 honoured — charging %.1f A", chargingA)
			st.ZeroSince = time.Time{} // проверено, больше не смотрим
		}
	}
	if maxReg != *lastMax {
		if err := s.client.CallService("number", "set_value", map[string]any{"entity_id": chargeMaxEntity, "value": maxReg}); err != nil {
			log.Println("charge: set max:", err)
		} else {
			log.Printf("charge: max %.0f A → reg108=%.0f (x%.0f), soc %.0f%%, %s", amps, maxReg, st.Factor, soc, mode)
			if maxReg == 0 && !st.ZeroBroken {
				st.ZeroSince = time.Now()
			}
			*lastMax = maxReg
		}
	}
	if gridReg != *lastGrid {
		if err := s.client.CallService("number", "set_value", map[string]any{"entity_id": chargeGridEntity, "value": gridReg}); err != nil {
			log.Println("charge: set grid:", err)
		} else {
			log.Printf("charge: grid %.0f A → reg128=%.0f", grid, gridReg)
			*lastGrid = gridReg
		}
	}
}
