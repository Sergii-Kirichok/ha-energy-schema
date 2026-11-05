package web

import (
	"log"
	"math"
	"time"
)

// Сторож напряжения сети для «Лимита от сети» (рег. 128): ток заряда от сети
// ведётся так, чтобы ни одна из трёх фаз не ушла за пороги отключения
// инвертора (рег. 186 нижний, 185 верхний) — иначе инвертор отключит сеть и
// дом сядет на батарею. Заряд от сети нагружает все три фазы, поэтому:
//   - сеть пропала — заранее малый ток RampStartA (инвертор применяет лимит с
//     задержкой и подключится уже с ним, а не с полным на слабую сеть);
//   - мин. фаза ниже нижнего порога + RampMarginV/2 — сразу на RampStartA;
//   - мин. фаза ниже нижнего + RampMarginV — шаг RampStepA вниз;
//   - макс. фаза выше верхнего − RampMarginV — шаг вверх (нагрузка тянет
//     напряжение вниз), можно выше ползунка до потолка GridALimit;
//   - иначе — к ползунку: вниз сразу, вверх шагами. Низ важнее верха.
//
// Шаги не чаще RampEveryS (кроме сильной просадки), гистерезис — ползапаса.
// Только при включённом «Заряд от сети»: без него сеть батарею не заряжает.
const (
	gridUpEntity  = "binary_sensor.deye_sun_30k_grid" // реле сети замкнуто (рег. 552, бит 2)
	gridVHighReg  = 185                               // верхний порог напряжения сети, ×0,1 В (186 — нижний)
	rampVLowGuess = 185.0                             // ponytail: если 185..186 не прочитались; заменится чтением
	rampLimitsAge = time.Hour                         // пороги перечитываются (их меняют со страницы инвертора)
)

const (
	bandOK     = "ok"
	bandHold   = "hold"   // в полосе гистерезиса — держим
	bandLow    = "low"    // мин. фаза близко к низу — шаг вниз
	bandSevere = "severe" // сильная просадка — сразу минимум
	bandHigh   = "high"   // макс. фаза близко к верху — шаг вверх
)

func (st *chargeState) rampActive() bool { return st.RampA > 0 }

// vBand — где сейчас фазы относительно порогов инвертора.
func (s *Server) vBand(st *chargeState) string {
	if st.RampVLow == 0 {
		return ""
	}
	lo, hi, m := st.RampVLow, st.RampVHigh, s.cfg.Charge.RampMarginV
	minV, maxV := math.Inf(1), 0.0
	for ph := 1; ph <= 3; ph++ {
		v := s.store.Num(gridVoltEntity(ph))
		minV, maxV = math.Min(minV, v), math.Max(maxV, v)
	}
	switch {
	case minV < lo+m/2:
		return bandSevere
	case minV < lo+m:
		return bandLow
	case hi > 0 && maxV > hi-m:
		return bandHigh
	case minV < lo+m*1.5 || hi > 0 && maxV > hi-m*1.5:
		return bandHold
	}
	return bandOK
}

// rampTick — ток от сети с учётом сторожа напряжения (grid — с ползунка/режима).
func (s *Server) rampTick(st *chargeState, grid float64) float64 {
	t := s.cfg.Charge
	if s.store.State(gridChargeSwitch) != "on" {
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
			st.RampA = t.RampStartA
			st.save(chargeFile)
			log.Printf("ramp: grid down — grid charge pre-set to %.0f A (thresholds %.1f…%.1f V)", st.RampA, st.RampVLow, st.RampVHigh)
		}
		st.RampAt = time.Now() // отсчёт шага — от подключения, а не от пропажи
		return st.RampA
	}
	if !st.rampActive() { // первый тик с сетью — без рывка, от текущего лимита
		st.RampA, st.RampAt = grid, time.Now()
	}
	band, was := s.vBand(st), st.RampA
	top := math.Max(grid, t.GridALimit)
	if st.probeActive() {
		top = grid // прогон тока: ступень — это и есть задание, выше не подталкиваем
	}
	if band != bandHigh && st.RampA > grid {
		st.RampA = grid // вниз к ползунку — всегда безопасно, сразу
	}
	if band == bandSevere {
		st.RampA = t.RampStartA
	} else if time.Since(st.RampAt) >= time.Duration(t.RampEveryS*float64(time.Second)) {
		switch band {
		case bandLow:
			st.RampA = math.Max(t.RampStartA, st.RampA-t.RampStepA)
		case bandHigh:
			st.RampA = math.Min(top, st.RampA+t.RampStepA)
		case bandOK:
			st.RampA = math.Min(grid, st.RampA+t.RampStepA)
		}
	}
	if st.RampA != was {
		st.RampAt = time.Now()
		st.save(chargeFile)
		log.Printf("ramp: %s — %.0f → %.0f A (slider %.0f, thresholds %.1f…%.1f V)", band, was, st.RampA, grid, st.RampVLow, st.RampVHigh)
	}
	return st.RampA
}

func (s *Server) rampCap(st *chargeState, grid float64) float64 {
	if !st.rampActive() {
		return grid
	}
	return st.RampA
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
		st.RampVLow = rampVLowGuess // верх неизвестен — подталкивание вверх выключено
	}
}
