package web

import (
	"log"
	"math"
	"time"
)

// Плавный старт сети. Сеть пропала — в «Лимит от сети» (рег. 128) заранее
// пишем малый ток RampStartA: инвертор применяет лимит с задержкой, так что
// подключится он уже с ним, а не с полным лимитом на слабую (190 В) сеть.
// Сеть вернулась — раз в RampEveryS смотрим самую низкую фазу: выше нижнего
// порога сети инвертора (рег. 186) на RampMarginV — прибавляем RampStepA,
// ниже половины запаса — шаг назад, между — держим. Дошли до «Лимита от
// сети» — старт закончен, дальше обычная регуляция. Только при включённом
// «Заряд от сети»: без него сеть батарею не заряжает и писать нечего.
const (
	gridUpEntity  = "binary_sensor.deye_sun_30k_grid" // реле сети замкнуто (рег. 552, бит 2)
	gridVLowReg   = 186                               // нижний порог напряжения сети, ×0,1 В
	rampVLowGuess = 185.0                             // ponytail: если 186 не прочитался; заменится чтением
)

func (st *chargeState) rampActive() bool { return st.RampA > 0 }

// rampTick ограничивает ток от сети grid на время плавного старта.
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
	if !s.store.On(gridUpEntity) {
		if st.RampA != t.RampStartA {
			st.RampA = t.RampStartA
			s.rampReadVLow(st)
			st.save(chargeFile)
			log.Printf("ramp: grid down — grid charge pre-set to %.0f A (low threshold %.1f V)", st.RampA, st.RampVLow)
		}
		st.RampAt = time.Now() // отсчёт шага — от подключения, а не от пропажи
		return s.rampCap(st, grid)
	}
	if !st.rampActive() || time.Since(st.RampAt) < time.Duration(t.RampEveryS*float64(time.Second)) {
		return s.rampCap(st, grid)
	}
	minV := math.Inf(1)
	for ph := 1; ph <= 3; ph++ {
		minV = math.Min(minV, s.store.Num(gridVoltEntity(ph)))
	}
	was := st.RampA
	switch {
	case minV < st.RampVLow+t.RampMarginV/2:
		st.RampA = math.Max(t.RampStartA, st.RampA-t.RampStepA)
	case minV >= st.RampVLow+t.RampMarginV:
		st.RampA += t.RampStepA
	}
	st.RampAt = time.Now()
	if st.RampA >= grid {
		log.Printf("ramp: done — min phase %.0f V, grid limit %.0f A", minV, grid)
		st.RampA = 0
	} else if st.RampA != was {
		log.Printf("ramp: %.0f → %.0f A, min phase %.0f V (low %.1f + %.0f V margin)", was, st.RampA, minV, st.RampVLow, t.RampMarginV)
	}
	st.save(chargeFile)
	return s.rampCap(st, grid)
}

func (s *Server) rampCap(st *chargeState, grid float64) float64 {
	if !st.rampActive() {
		return grid
	}
	return math.Min(grid, st.RampA)
}

// rampReadVLow — нижний порог сети прямо из инвертора; при ошибке — прошлый или догадка.
func (s *Server) rampReadVLow(st *chargeState) {
	if r, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, gridVLowReg, 1); err == nil && r[gridVLowReg] > 0 {
		st.RampVLow = float64(r[gridVLowReg]) / 10
	} else if st.RampVLow == 0 {
		st.RampVLow = rampVLowGuess
	}
}
