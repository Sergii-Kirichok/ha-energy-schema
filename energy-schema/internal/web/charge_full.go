package web

import (
	"fmt"
	"log"
	"math"
	"time"

	"energy-schema/internal/scada"
)

// Полный заряд «сегодня до 100 %» (кнопка или срок): идём «Общим лимитом».
// Прогноз решает, хватит ли солнца до заката; если нет и сеть есть — регулятор
// временно выключает Time of Use (один регистр 146) — с включённым «Заряд от
// сети» Deye тогда заряжает от сети в пределах «Лимита от сети»; на 100 % или
// когда полный заряд снят — возвращает ToU как было. Если и так не успеть к полуночи — пишет.
const chargeFullPlan = "sensor.energy_schema_charge_full_plan"

// chargeFile — состояние регулятора (переменная: тесты пишут во временный каталог).
var chargeFile = "/data/charge.json"

// touSettle — сколько держаться новому решению «ToU выкл/вкл», прежде чем писать 146.
var touSettle = 5 * time.Minute

type fullIn struct {
	NeedKWh, BulkKWh                                 float64 // всего до 100 % и из них до порога хвоста
	SolarLeftKWh, AvgLoadKW, HoursSun, HoursMidnight float64
	MaxKW, TailKW, GridKW                            float64 // «Общий лимит», ток хвоста, «Лимит от сети» — в кВт
	GridOK                                           bool
}

type fullOut struct {
	UseGrid, Short bool
	Hours          float64 // через сколько часов ожидается 100 %
	Why            string
}

// planFull — хватит ли солнца, нужна ли сеть, успеем ли к полуночи. Шагами по
// 3 мин: мощность = солнечный излишек (до заката) [+ сеть], но не больше
// «Общего лимита» до порога хвоста и тока хвоста после него.
func planFull(in fullIn) fullOut {
	if in.NeedKWh <= 0 {
		return fullOut{Why: "заряжено"}
	}
	sunKW := 0.0
	if in.HoursSun > 0 {
		sunKW = math.Max(0, in.SolarLeftKWh-in.AvgLoadKW*in.HoursSun) / in.HoursSun
	}
	sim := func(grid bool) float64 {
		const dt = 0.05
		e := 0.0
		for h := 0.0; h < 48; h += dt {
			p := 0.0
			if h < in.HoursSun {
				p += sunKW
			}
			if grid {
				p += in.GridKW
			}
			lim := in.MaxKW
			if e >= in.BulkKWh {
				lim = in.TailKW
			}
			if p = math.Min(p, lim); p <= 0 && (h >= in.HoursSun || sunKW <= 0) {
				return math.Inf(1)
			}
			if e += p * dt; e >= in.NeedKWh {
				return h + dt
			}
		}
		return math.Inf(1)
	}
	if h := sim(false); h <= in.HoursSun {
		return fullOut{Hours: h, Why: "солнцем"}
	}
	if !in.GridOK || in.GridKW <= 0 {
		return fullOut{Short: true, Hours: math.Inf(1), Why: "солнца не хватит, а заряд от сети недоступен"}
	}
	h := sim(true)
	return fullOut{UseGrid: true, Hours: h, Short: h > in.HoursMidnight, Why: "солнце + сеть"}
}

// fullCharge — план полного заряда, публикация и правка слота ToU по плану.
// Вызывается из chargeTick под s.mb (Modbus-транзакции не пересекаются).
func (s *Server) fullCharge(st *chargeState, fullDue bool, soc, maxA, gridA float64) {
	if !fullDue || soc >= 100 {
		s.touRestore(st)
		_ = s.client.SetState(chargeFullPlan, "—", map[string]any{"friendly_name": "Заряд: полный до 100 %", "icon": "mdi:battery-clock"})
		return
	}
	v := math.Max(s.store.Num("sensor.deye_sun_30k_battery_voltage"), 100)
	socF := s.store.Num(scada.InvSOC)
	if socF <= 0 {
		socF = soc
	}
	soh := s.store.Num("sensor.energy_schema_bms_soh")
	if soh <= 0 || soh > 100 {
		soh = 100
	}
	_, left, _, _, _ := s.store.SolarTotals()
	avgLoad, _ := s.store.Avg24h("sensor.deye_sun_30k_load_power")
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	hSun := 0.0
	if s.store.State("sun.sun") == "above_horizon" {
		hSun = math.Max(0, s.store.HoursUntil("sun.sun", "next_setting"))
	}
	gridOK := s.store.On("binary_sensor.deye_sun_30k_grid") && s.store.On(gridChargeSwitch)
	capKWh := battKWh(s) * soh / 100
	tail := s.cfg.Charge.FullTailSOC
	if tail <= 0 || tail > 100 {
		tail = 100
	}
	p := planFull(fullIn{NeedKWh: capKWh * (100 - socF) / 100, BulkKWh: capKWh * math.Max(0, tail-socF) / 100,
		SolarLeftKWh: left, AvgLoadKW: avgLoad / 1000, HoursSun: hSun, HoursMidnight: midnight.Sub(now).Hours(),
		MaxKW: maxA * v / 1000, TailKW: math.Min(maxA, math.Max(chargeMinA, s.cfg.Charge.FullTailA)) * v / 1000,
		GridKW: gridA * v / 1000, GridOK: gridOK})
	text := p.Why
	switch {
	case p.Short && math.IsInf(p.Hours, 1):
	case p.Short:
		text = fmt.Sprintf("не успеть к полуночи при %.0f А — нужно ≈ %.0f ч", maxA, p.Hours)
	default:
		text = fmt.Sprintf("%s, ≈ к %s", p.Why, now.Add(time.Duration(p.Hours*float64(time.Hour))).Format("15:04"))
	}
	_ = s.client.SetState(chargeFullPlan, text, map[string]any{"friendly_name": "Заряд: полный до 100 %",
		"icon": "mdi:battery-clock", "grid": p.UseGrid, "short": p.Short})
	// ToU переключаем, только если нужное состояние держится touSettle: моргание
	// сети или колебание прогноза не должны дёргать регистр 146 туда-обратно
	if p.UseGrid != (st.TouWas != nil) {
		if st.touWant != p.UseGrid || st.touSince.IsZero() {
			st.touWant, st.touSince = p.UseGrid, now
		}
		if now.Sub(st.touSince) < touSettle {
			return
		}
	}
	st.touSince = time.Time{}
	if p.UseGrid {
		s.touPatchNow(st, now)
	} else {
		s.touRestore(st)
	}
}

// touPatchNow — выключить Time of Use (один регистр 146, бит 0): с выключенным
// расписанием и включённым «Заряд от сети» Deye заряжает от сети в пределах
// «Лимита от сети». Прежнее значение 146 — в st.TouWas, вернём после заряда.
func (s *Server) touPatchNow(st *chargeState, _ time.Time) {
	if st.TouWas != nil {
		return // уже выключено нами
	}
	r, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, 146, 1)
	if err != nil || r[146]&1 == 0 {
		return // нет связи или ToU и так выключен — заряд от сети уже разрешён
	}
	was := r[146]
	if err := s.client.WriteHoldingRegisters(bmsDeviceEntity, 146, []int{was &^ 1}); err != nil {
		log.Println("charge: ToU off:", err)
		return
	}
	s.writes.add("tou", 1)
	st.TouWas = &was
	st.save(chargeFile)
	log.Printf("charge: full charge from grid — Time of Use off (146: %d→%d)", was, was&^1)
}

// touRestore — вернуть регистр 146 (Time of Use), если выключали мы.
func (s *Server) touRestore(st *chargeState) {
	if st.TouWas == nil {
		return
	}
	was := *st.TouWas
	if err := s.client.WriteHoldingRegisters(bmsDeviceEntity, 146, []int{was}); err != nil {
		log.Println("charge: ToU restore:", err)
		return
	}
	s.writes.add("tou", 1)
	st.TouWas = nil
	st.save(chargeFile)
	log.Printf("charge: full charge over — Time of Use restored (146 = %d)", was)
}
