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
}
