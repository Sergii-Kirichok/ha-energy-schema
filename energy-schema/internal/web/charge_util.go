package web

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"energy-schema/internal/config"
	"energy-schema/internal/hass"
	"energy-schema/internal/scada"
)

// regValue — значение регистра для желаемого фактического тока: делим на
// множитель, округляем, не ниже 1 (0 у Deye = «не заряжать», не хотим).
func regValue(amps, factor float64) float64 {
	if factor < 1 {
		factor = 1
	}
	if amps <= 0 {
		return 0 // «стоп заряда»
	}
	return math.Max(1, math.Round(amps/factor))
}

// zeroVerdict — итог самопроверки «108 = 0 останавливает заряд»: ждём 2 мин
// (инвертор применяет не сразу) и только при testable (SOC<99, излишек PV);
// дальше заряд > 3 А → не работает, ≤ 1 А → ок.

// publishLimit — сенсор «Текущее ограничение тока заряда»: ФАКТИЧЕСКОЕ значение
// регистра 108 (прочитанное/записанное) × множитель каналов; расчёт регулятора
// и режим — в атрибутах.
func (s *Server) publishLimit(factor, reg108, target float64, mode string, soc float64, fullDue, auto bool, floorA, holdAt float64) {
	if factor < 1 {
		factor = 1
	}
	attrs := map[string]any{"friendly_name": "Текущее ограничение тока заряда", "unit_of_measurement": "A",
		"state_class": "measurement", "icon": "mdi:current-dc", "mode": mode, "target_a": target,
		"reg108": reg108, "channels": factor, "soc": soc, "full_due": fullDue, "auto": auto,
		"zero_floor_a": floorA, "hold_from_soc": holdAt}
	_ = s.client.SetState("sensor.energy_schema_charge_setpoint", fmt.Sprintf("%.0f", reg108*factor), attrs)
}

// nightHyst — ночь, если PV ниже NightBelowW; снова день, когда PV выше
// DayAboveW (разрыв — гистерезис против вечерних облаков).
func nightHyst(pvW float64, wasNight bool, t config.ChargeTuning) bool {
	if wasNight {
		return pvW <= t.DayAboveW
	}
	return pvW < t.NightBelowW
}

// pvBucket — грубая корзина генерации для отпечатка входов регулятора:
// смена корзины = событие (переход день/ночь), дрожание внутри — нет.
func pvBucket(pvW float64, t config.ChargeTuning) string {
	switch {
	case pvW < t.NightBelowW:
		return "n"
	case pvW > t.DayAboveW:
		return "d"
	}
	return "m"
}

// zeroVerdict — итог самопроверки «108 = 0»: ждём 2 мин и только при testable
// (SOC<99, излишек PV). Ток > 3 А → 0 = «без ограничения» (broken). Иначе —
// «ok»: инвертор режет до остаточного тока (у Deye HV это ~1 А, не полный стоп);
// этот остаток запоминается и учитывается в driftMargin.
func zeroVerdict(since time.Duration, chargingA float64, testable bool) string {
	switch {
	case since < 2*time.Minute || !testable:
		return "wait"
	case chargingA > 3:
		return "broken"
	}
	return "ok"
}

// driftMargin — на сколько % SOC батарея доберёт остаточным током floorA до
// конца солнца (hoursLeft): на столько раньше начинаем удержание, чтобы к
// вечеру выйти на цель, а не цель + хвост. Не больше 5 %.
func driftMargin(floorA, battV, hoursLeft, capKWh float64) float64 {
	if floorA <= 0 || battV <= 0 || hoursLeft <= 0 || capKWh <= 0 {
		return 0
	}
	return math.Min(5, floorA*battV*hoursLeft/1000/capKWh*100)
}

type chargeState struct {
	LastFull    time.Time `json:"last_full"`
	Factor      float64   `json:"-"`            // множитель регистр→факт (1 или 2), 0 = ещё не прочитан
	ZeroBroken  bool      `json:"zero_broken"`  // проверено: Deye не останавливает заряд по 108=0
	ZeroFloorA  float64   `json:"zero_floor_a"` // остаточный ток заряда при 108=0 (измерен), А
	ZeroChecked bool      `json:"zero_checked"` // остаток измерен самопроверкой
	ZeroSince   time.Time `json:"-"`            // когда записали 0 (для самопроверки)
	Night       bool      `json:"-"`            // ночной режим (гистерезис по генерации)
	Holding     bool      `json:"holding"`      // удержание у цели (гистерезис holdHyst)
	LastMode    string    `json:"-"`            // режим прошлого тика (смена день/ночь — запись сразу)
	Wrote108    time.Time `json:"-"`            // последняя запись регистра 108
	seen        map[string]float64
	seenAt      map[string]time.Time
}

// inputSettle — сколько ползунок должен не меняться, чтобы значение ушло в
// инвертор (три клика «+1» = одна запись, а не три). 0 — без выдержки (тесты).
var inputSettle = 15 * time.Second

// settled — значение k не менялось inputSettle (первое наблюдение — «нет»).
func (st *chargeState) settled(k string, v float64) bool {
	if inputSettle <= 0 {
		return true
	}
	if st.seen == nil {
		st.seen, st.seenAt = map[string]float64{}, map[string]time.Time{}
	}
	if _, ok := st.seenAt[k]; !ok || st.seen[k] != v {
		st.seen[k], st.seenAt[k] = v, time.Now()
		return false
	}
	return time.Since(st.seenAt[k]) >= inputSettle
}

// sunLow — солнце ниже 10° (или sun.sun недоступен — тогда решает генерация).
// Пасмурным днём генерация бывает < 1 кВт, но это не ночь.
func sunLow(st *hass.Store) bool {
	if !st.Available("sun.sun") {
		return true
	}
	if st.State("sun.sun") == "below_horizon" {
		return true
	}
	if e := st.Attr("sun.sun", "elevation"); e != "" {
		return st.AttrNum("sun.sun", "elevation") < 10
	}
	return false
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

// battKWh — номинальная ёмкость стойки из инвертора и BMS (fallback — настройки аддона).
func battKWh(s *Server) float64 { k, _ := scada.BattCapKWh(s.store, s.cfg.BattCap); return k }

// noteBattRegs — ёмкость (102) и пороги SOC (115 отключение, 117 низкий) прямо
// из прочитанных регистров в хранилище: схема видит их сразу, а не через
// опрос настроек Solarman (раз в 5 мин). См. scada.InvBattAh и соседей.
func (s *Server) noteBattRegs(r map[int]int) {
	for reg, key := range map[int]string{102: scada.InvBattAh, 115: scada.InvShutdownSOC, 117: scada.InvLowSOC} {
		if v, ok := r[reg]; ok && v > 0 {
			s.store.SetVirtual(key, strconv.Itoa(v))
		}
	}
}
