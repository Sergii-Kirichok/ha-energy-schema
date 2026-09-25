package web

import (
	"encoding/json"
	"fmt"
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
	chargeCellCapA   = 5.0  // при разбалансе ячеек — не выше этого…
	chargeCellCapSOC = 70.0 // …но только от этого SOC: ниже батарея выравнивается сама, ток не режем
	parallelReg      = 110  // «Parallel Bat&Bat2»: =1 → инвертор умножает лимиты 108/128 на 2 (проверено 25.09: 8 А → 15,8 А факт)
)

var chargeHelpers = []hass.Helper{
	{Domain: "input_boolean", ID: "energy_schema_charge_auto", Name: "Заряд: авто-регулятор", Icon: "mdi:battery-sync", Initial: 1},
	{Domain: "input_number", ID: "energy_schema_charge_max_a", Name: "Заряд: общий лимит", Min: 1, Max: 185, Step: 1, Initial: 25, Unit: "A", Icon: "mdi:current-dc"},
	{Domain: "input_number", ID: "energy_schema_charge_grid_a", Name: "Заряд: лимит от сети", Min: 0, Max: 185, Step: 1, Initial: 5, Unit: "A", Icon: "mdi:transmission-tower"},
	{Domain: "input_number", ID: "energy_schema_charge_taper_soc", Name: "Заряд: снижать ток с", Min: 50, Max: 99, Step: 1, Initial: 80, Unit: "%", Icon: "mdi:battery-70"},
	{Domain: "input_number", ID: "energy_schema_charge_target_soc", Name: "Заряд: цель на ночь", Min: 50, Max: 100, Step: 1, Initial: 90, Unit: "%", Icon: "mdi:battery-90"},
	{Domain: "input_number", ID: "energy_schema_charge_full_days", Name: "Заряд: полный раз в", Min: 1, Max: 60, Step: 1, Initial: 14, Unit: "д", Icon: "mdi:calendar-refresh"},
	{Domain: "input_boolean", ID: "energy_schema_charge_full_now", Name: "Заряд: полный сейчас", Icon: "mdi:battery-charging-100"},
}

const chargeFullNowEntity = "input_boolean.energy_schema_charge_full_now"

// chargeInput — всё, что нужно закону регулирования (чистая функция, тестируется).
type chargeInput struct {
	SOC, MaxA, TaperSOC, TargetSOC float64
	FullDue                        bool
	CellLevel                      string // "ok" | "warn" | "bad" (см. cellVerdict)
}

// chargeSetpoint возвращает ток заряда (целые амперы, ≥1) и режим для сенсора.
func chargeSetpoint(in chargeInput) (float64, string) {
	target := in.TargetSOC
	mode := "night"
	if in.FullDue {
		target, mode = 100, "full"
	}
	var a float64
	switch {
	case in.SOC < in.TaperSOC || in.TaperSOC >= target:
		a, mode = in.MaxA, "max"
	case in.SOC < target:
		a = in.MaxA * (target - in.SOC) / (target - in.TaperSOC)
		mode += "/taper"
	default:
		a, mode = chargeMinA, mode+"/hold"
	}
	if in.CellLevel == "bad" && in.SOC >= chargeCellCapSOC && a > chargeCellCapA {
		a, mode = chargeCellCapA, mode+"/cells"
	}
	return math.Max(chargeMinA, math.Round(a)), mode
}

type chargeState struct {
	LastFull time.Time `json:"last_full"`
	Factor   float64   `json:"-"` // множитель регистр→факт (1 или 2), 0 = ещё не прочитан
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
}

// loopCharge — раз в минуту: читает хелперы и SOC, пишет лимиты в инвертор
// (только при изменении), публикует уставку/режим/дату полного заряда.
func (s *Server) loopCharge() {
	s.ensureChargeHelpers()
	st := loadChargeState(chargeFile)
	lastMax, lastGrid := -1.0, -1.0
	for {
		s.chargeTick(&st, &lastMax, &lastGrid)
		time.Sleep(time.Minute)
	}
}

func (s *Server) chargeTick(st *chargeState, lastMax, lastGrid *float64) {
	num := func(e string) float64 { return s.store.Num(e) }
	// SOC — BMS через Solarman (регистр 214, тот же, что 10005)
	soc := num("sensor.deye_sun_30k_battery")
	if !s.store.Available("sensor.deye_sun_30k_battery") {
		return
	}
	fullDays := num("input_number.energy_schema_charge_full_days")
	if soc >= 100 {
		st.LastFull = time.Now()
		st.save(chargeFile)
	}
	nextFull := st.LastFull.Add(time.Duration(fullDays*24) * time.Hour)
	fullDue := fullDays > 0 && (st.LastFull.IsZero() || !time.Now().Before(nextFull))
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
	auto := s.store.On("input_boolean.energy_schema_charge_auto")
	if in.MaxA <= 0 || in.TargetSOC <= 0 {
		return // хелперы ещё не созданы / не прочитаны
	}
	amps, mode := chargeSetpoint(in)
	if !auto {
		mode = "off"
	}
	attrs := map[string]any{"friendly_name": "Заряд: уставка", "unit_of_measurement": "A", "state_class": "measurement",
		"icon": "mdi:current-dc", "mode": mode, "soc": soc, "full_due": fullDue, "auto": auto}
	_ = s.client.SetState("sensor.energy_schema_charge_setpoint", fmt.Sprintf("%.0f", amps), attrs)
	nf := "—"
	if !st.LastFull.IsZero() {
		nf = nextFull.Format("2006-01-02")
	}
	_ = s.client.SetState("sensor.energy_schema_charge_next_full", nf, map[string]any{"friendly_name": "Заряд: следующий 100 %",
		"icon": "mdi:calendar-check", "last_full": st.LastFull.Format(time.RFC3339)})
	if !auto {
		return
	}
	// множитель регистр→факт: при «Parallel Bat&Bat2»=1 инвертор удваивает лимит.
	// Читаем каждый тик; при ошибке чтения держим прошлое значение, а без него
	// не пишем вовсе (иначе можно случайно дать вдвое больший ток).
	if regs, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, parallelReg, 1); err == nil {
		st.Factor = 1 + float64(regs[parallelReg]&1)
	} else if st.Factor == 0 {
		log.Println("charge: parallel flag:", err)
		return
	}
	grid := num("input_number.energy_schema_charge_grid_a")
	maxReg, gridReg := regValue(amps, st.Factor), regValue(grid, st.Factor)
	if maxReg != *lastMax {
		if err := s.client.CallService("number", "set_value", map[string]any{"entity_id": chargeMaxEntity, "value": maxReg}); err != nil {
			log.Println("charge: set max:", err)
		} else {
			log.Printf("charge: max %.0f A → reg108=%.0f (x%.0f), soc %.0f%%, %s", amps, maxReg, st.Factor, soc, mode)
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

// regValue — значение регистра для желаемого фактического тока: делим на
// множитель, округляем, не ниже 1 (0 у Deye = «не заряжать», не хотим).
func regValue(amps, factor float64) float64 {
	if factor < 1 {
		factor = 1
	}
	return math.Max(1, math.Round(amps/factor))
}
