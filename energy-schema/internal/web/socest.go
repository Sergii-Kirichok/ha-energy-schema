package web

import (
	"math"
	"strconv"
	"time"

	"energy-schema/internal/scada"
)

// socEst — SOC с десятыми: BMS отдаёт целые проценты, а 1 % нашей стойки —
// ~1,9 кВт·ч, поэтому между сменами процента досчитываем по току батареи
// (кулонометрия) и держим оценку в окне ±0,5 % от значения BMS — ошибка
// интегрирования не копится, BMS остаётся источником правды.
type socEst struct {
	f  float64
	at time.Time
	ok bool
}

// step: soc — целый SOC от BMS, currentA — ток АКБ (HA: «−» заряд), ah —
// ёмкость стойки (А·ч с учётом SOH). Возвращает уточнённый SOC.
func (e *socEst) step(soc, currentA, ah float64, now time.Time) float64 {
	dt := now.Sub(e.at).Hours()
	if !e.ok || ah <= 0 || dt <= 0 || dt > 0.1 { // первый раз / нет ёмкости / долгий разрыв — с BMS
		e.f, e.at, e.ok = soc, now, true
		return e.f
	}
	e.at = now
	e.f -= currentA * dt / ah * 100
	e.f = math.Max(soc-0.5, math.Min(soc+0.5, e.f))
	e.f = math.Max(0, math.Min(100, e.f))
	return e.f
}

// updateSOC — после каждого опроса HA: уточнённый SOC в хранилище для схемы.
func (s *Server) updateSOC() {
	if !s.store.Available("sensor.deye_sun_30k_battery") {
		return
	}
	ah := s.store.Num(scada.InvBattAh)
	if ah <= 0 {
		ah = s.store.Num(scada.BattAhEntity)
	}
	if soh := s.store.Num("sensor.energy_schema_bms_soh"); soh > 0 && soh <= 100 {
		ah *= soh / 100
	}
	f := s.soc.step(s.store.Num("sensor.deye_sun_30k_battery"), s.store.Num("sensor.deye_sun_30k_battery_current"), ah, time.Now())
	s.store.SetVirtual(scada.InvSOC, strconv.FormatFloat(f, 'f', 2, 64))
	if s.load.at.IsZero() { // после рестарта — не с мгновенного значения, а со среднего за сутки
		if avg, ok := s.store.Avg24h("sensor.deye_sun_30k_load_power"); ok && avg > 0 {
			s.load.v, s.load.at = avg/1000, time.Now()
		}
	}
	if s.store.Available("sensor.deye_sun_30k_load_power") {
		l := s.load.step(s.store.Num("sensor.deye_sun_30k_load_power")/1000, time.Now())
		s.store.SetVirtual(scada.InvLoadAvg, strconv.FormatFloat(l, 'f', 3, 64))
	}
}

// loadAvgTau — постоянная сглаживания нагрузки для прогнозов автономии: часы
// «Только от АКБ» не должны прыгать от каждого включения чайника.
const loadAvgTau = 30 * time.Minute

// loadEMA — экспоненциальное среднее нагрузки (кВт).
type loadEMA struct {
	v  float64
	at time.Time
}

func (e *loadEMA) step(kw float64, now time.Time) float64 {
	dt := now.Sub(e.at)
	if e.at.IsZero() || dt > time.Hour || dt <= 0 {
		e.v, e.at = kw, now
		return e.v
	}
	e.at = now
	e.v += (kw - e.v) * math.Min(1, float64(dt)/float64(loadAvgTau))
	return e.v
}
