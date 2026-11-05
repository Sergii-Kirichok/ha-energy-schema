package web

import (
	"fmt"
	"log"
	"math"
	"time"
)

// Сторож напряжения сети для «Лимита от сети» (рег. 128): ток заряда от сети
// ведётся так, чтобы самая низкая из трёх фаз не ушла под порог отключения
// инвертора (рег. 186) — иначе инвертор отключит сеть и дом сядет на батарею.
// Потолок — всегда ползунок «Лимит от сети», выше него не поднимаем.
//   - сеть пропала — заранее малый ток RampStartA (инвертор применяет лимит с
//     задержкой и после паузы переподключения встанет на сеть уже с ним);
//   - мин. фаза ниже порога + RampMarginV/2 — сразу на RampStartA;
//   - ниже порога + RampMarginV — шаг RampStepA вниз;
//   - выше порога + RampMarginV + RampHystV — шаг вверх, к ползунку;
//   - между — держим (не качаем ток туда-обратно на каждом колебании).
//
// Ползунок уменьшили — сразу к нему. Шаги не чаще RampEveryS (кроме сильной
// просадки). Дошли до ползунка и сеть в норме — регистр не трогаем, сторож
// только смотрит. Только при включённых «Заряд от сети» и «Сторож сети». Каждый тик с сетью —
// строка статистики для подстройки (charge_ramp_stats.go).
const (
	rampGuardHelper  = "input_boolean.energy_schema_charge_grid_guard" // выкл — ток от сети ровно по ползунку
	rampStatusEntity = "sensor.energy_schema_charge_grid_guard"
	gridUpEntity     = "binary_sensor.deye_sun_30k_grid" // реле сети замкнуто (рег. 552, бит 2)
	gridVHighReg     = 185                               // верхний порог напряжения сети, ×0,1 В (186 — нижний)
	rampVLowGuess    = 185.0                             // ponytail: если 185..186 не прочитались; заменится чтением
	rampLimitsAge    = time.Hour                         // пороги перечитываются (их меняют со страницы инвертора)
)

const (
	bandOK     = "ok"     // можно прибавлять к ползунку
	bandHold   = "hold"   // полоса гистерезиса — держим
	bandLow    = "low"    // мин. фаза близко к низу — шаг вниз
	bandSevere = "severe" // сильная просадка — сразу минимум
)

func (st *chargeState) rampActive() bool { return st.RampA > 0 }

// phaseMinMax — самая низкая и самая высокая фаза сети, В.
func (s *Server) phaseMinMax() (minV, maxV float64) {
	minV = math.Inf(1)
	for ph := 1; ph <= 3; ph++ {
		v := s.store.Num(gridVoltEntity(ph))
		minV, maxV = math.Min(minV, v), math.Max(maxV, v)
	}
	return
}

// vBand — где самая низкая фаза относительно нижнего порога инвертора.
func (s *Server) vBand(st *chargeState) string {
	if st.RampVLow == 0 {
		return ""
	}
	lo, m, h := st.RampVLow, s.cfg.Charge.RampMarginV, s.cfg.Charge.RampHystV
	minV, _ := s.phaseMinMax()
	switch {
	case minV < lo+m/2:
		return bandSevere
	case minV < lo+m:
		return bandLow
	case minV < lo+m+h:
		return bandHold
	}
	return bandOK
}

var bandText = map[string]string{bandOK: "можно больше", bandHold: "держим", bandLow: "близко к низу — вниз", bandSevere: "сильная просадка — минимум"}

// rampTick — ток от сети с учётом сторожа напряжения (grid — с ползунка/режима)
// + статус сторожа на дашборд.
func (s *Server) rampTick(st *chargeState, grid float64) float64 {
	g := s.rampDecide(st, grid)
	minV, _ := s.phaseMinMax()
	var text string
	switch {
	case s.store.State(gridChargeSwitch) != "on":
		text = "не нужен — заряд от сети выкл"
	case s.store.State(rampGuardHelper) == "off":
		text = fmt.Sprintf("выкл — ток по ползунку, %.0f А", g)
	case !s.store.Available(gridUpEntity):
		text = "нет данных о сети"
	case !s.store.On(gridUpEntity):
		text = fmt.Sprintf("сети нет — заранее %.0f А", g)
	default:
		text = fmt.Sprintf("%.0f из %.0f А · %s · мин. фаза %.0f В (порог %.0f)", g, grid, bandText[s.vBand(st)], minV, st.RampVLow)
	}
	_ = s.client.SetState(rampStatusEntity, text, map[string]any{"friendly_name": "Заряд: сторож сети", "icon": "mdi:shield-sun",
		"grid_a": g, "slider_a": grid, "band": s.vBand(st), "min_v": minV, "v_low": st.RampVLow, "v_high": st.RampVHigh})
	return g
}

func (s *Server) rampDecide(st *chargeState, grid float64) float64 {
	t := s.cfg.Charge
	if s.store.State(gridChargeSwitch) != "on" || s.store.State(rampGuardHelper) == "off" {
		if st.rampActive() {
			st.RampA = 0
			st.save(chargeFile)
		}
		return grid
	}
	if !s.store.Available(gridUpEntity) { // нет данных (рестарт HA) — состояние не трогаем
		return s.rampCap(st, grid)
	}
	s.rampReadLimits(st)
	if !s.store.On(gridUpEntity) {
		if st.RampA != t.RampStartA {
			s.rampStat(st, "grid_down", bandSevere, st.RampA, t.RampStartA, grid)
			st.RampA = t.RampStartA
			st.save(chargeFile)
			log.Printf("ramp: grid down — grid charge pre-set to %.0f A (low threshold %.1f V)", st.RampA, st.RampVLow)
		}
		st.RampAt = time.Now() // отсчёт шага — от подключения, а не от пропажи
		return s.rampCap(st, grid)
	}
	if !st.rampActive() { // первый тик с сетью — без рывка, от текущего лимита
		st.RampA, st.RampAt = grid, time.Now()
	}
	band, was := s.vBand(st), st.RampA
	st.RampA = math.Min(st.RampA, grid) // вниз к ползунку — всегда безопасно, сразу
	if band == bandSevere {
		st.RampA = t.RampStartA
	} else if time.Since(st.RampAt) >= time.Duration(t.RampEveryS*float64(time.Second)) {
		switch band {
		case bandLow:
			st.RampA = math.Max(t.RampStartA, st.RampA-t.RampStepA)
		case bandOK:
			st.RampA = math.Min(grid, st.RampA+t.RampStepA)
		}
	}
	ev := "sample"
	if st.RampA != was {
		ev = "step"
		st.RampAt = time.Now()
		st.save(chargeFile)
		log.Printf("ramp: %s — %.0f → %.0f A (slider %.0f, low threshold %.1f V)", band, was, st.RampA, grid, st.RampVLow)
	}
	s.rampStat(st, ev, band, was, st.RampA, grid)
	return st.RampA
}

func (s *Server) rampCap(st *chargeState, grid float64) float64 {
	if !st.rampActive() {
		return grid
	}
	return math.Min(grid, st.RampA)
}

// rampReadLimits — пороги сети прямо из инвертора (раз в час); ошибка — прошлые или догадка.
func (s *Server) rampReadLimits(st *chargeState) {
	if st.RampVLow > 0 && time.Since(st.limitsAt) < rampLimitsAge {
		return
	}
	st.limitsAt = time.Now()
	if r, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, gridVHighReg, 2); err == nil && r[gridVHighReg+1] > 0 {
		st.RampVHigh, st.RampVLow = float64(r[gridVHighReg])/10, float64(r[gridVHighReg+1])/10
	} else if st.RampVLow == 0 {
		st.RampVLow = rampVLowGuess
	}
}
