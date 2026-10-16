package web

import (
	"testing"
	"time"
)

// Пауза ждёт всё сразу: время, ячейку ≤ 3,45 В и разрешение BMS; дальше —
// следующая порция минимальным током (2 А).
func TestBalancePauseWaitsTimeVoltageBMS(t *testing.T) {
	for _, c := range []struct {
		ago         time.Duration
		cell, limit string
		status      string
	}{
		{30 * time.Minute, "3.400", "100", "пауза: ещё 30 мин · разбег на верху 60 мВ · порций 2"},
		{61 * time.Minute, "3.500", "100", "пауза: до ≤3,45 В · разбег на верху 60 мВ · порций 2"},
		{61 * time.Minute, "3.400", "", "пауза: разрешения BMS · разбег на верху 60 мВ · порций 2"},
		{61 * time.Minute, "3.400", "0", "пауза: разрешения BMS · разбег на верху 60 мВ · порций 2"},
	} {
		s, f := balSetup(t, map[string]string{bmsCellMaxEntity: c.cell}, map[int]int{108: 0}, c.limit)
		st := recentState()
		st.BalPhase, st.BalCycles, st.BalTopMV, st.BalSoft = balRest, 2, 60, true
		st.BalStart, st.BalRestAt = time.Now().Add(-time.Hour), time.Now().Add(-c.ago)
		balTick(t, s, &st)
		wantVals(t, "max", f.setValues(chargeMaxEntity)) // 0 уже стоит — записей нет
		wantBal(t, f, &st, balRest, "balance/rest", c.status)
	}
	s, f := balSetup(t, map[string]string{bmsCellMaxEntity: "3.450", bmsCellDeltaEntity: "5"}, map[int]int{108: 0}, "100")
	st := recentState()
	st.BalPhase, st.BalCycles, st.BalSoft = balRest, 2, true
	st.BalStart, st.BalRestAt = time.Now().Add(-time.Hour), time.Now().Add(-61*time.Minute)
	balTick(t, s, &st)
	wantVals(t, "max", f.setValues(chargeMaxEntity), 1)
	wantBal(t, f, &st, balCharge, "balance/soft", "заряд 2 А · разбег 5 мВ · порций 2")
}

// Предел по времени: через 3 дня без «выровнено» балансировка выключается
// сама; сессия из старой версии (без начала) отсчёт начинает с сейчас.
func TestBalanceMaxDays(t *testing.T) {
	s, f := balSetup(t, nil, map[int]int{108: 0}, "100")
	st := recentState()
	st.BalPhase, st.BalCycles, st.BalTopMV = balRest, 4, 60
	balTick(t, s, &st)
	if st.BalPhase != balRest || time.Since(st.BalStart) > time.Minute || len(f.find("/services/input_boolean/turn_off")) != 0 {
		t.Fatalf("migrated session must go on: phase %q start %v", st.BalPhase, st.BalStart)
	}
	st.BalStart = time.Now().Add(-72*time.Hour - time.Minute)
	s, f = balSetup(t, nil, map[int]int{108: 0}, "100")
	balTick(t, s, &st)
	offs := f.find("/services/input_boolean/turn_off")
	if st.BalPhase != balDone || len(offs) != 1 || offs[0].body["entity_id"] != chargeBalanceEntity {
		t.Fatalf("phase %q, turn_off %v", st.BalPhase, offs)
	}
	wantBal(t, f, &st, balDone, "", "остановлено через 3 дн.: разбег на верху 60 мВ")
}

// 16.10: при SOC 100 % на плоской части сеть помогать не переставала — ToU
// выключен всю сессию без оглядки на SOC и прогноз: в паузе не трогается,
// возвращается, когда балансировку выключили.
func TestBalanceGridIgnoresSOC(t *testing.T) {
	const tou = "/services/solarman/write_multiple_holding_registers"
	grid := map[string]string{"sensor.deye_sun_30k_battery": "100", bmsCellMaxEntity: "3.340", bmsCellDeltaEntity: "2",
		"binary_sensor.deye_sun_30k_grid": "on", gridChargeSwitch: "on"}
	s, f := balSetup(t, grid, map[int]int{108: 3, 146: 255}, "31")
	st := recentState()
	st.BalPhase = balCharge
	balTick(t, s, &st)
	if st.TouWas == nil || *st.TouWas != 255 || len(f.find(tou)) != 1 {
		t.Fatalf("ToU must be off for the session: was %v, writes %v", st.TouWas, f.find(tou))
	}
	grid[bmsCellMaxEntity], grid[bmsCellDeltaEntity] = "3.550", "80"
	s, f = balSetup(t, grid, map[int]int{108: 3, 146: 254}, "0")
	balTick(t, s, &st)
	if st.BalPhase != balRest || st.TouWas == nil || len(f.find(tou)) != 0 {
		t.Fatalf("pause must keep ToU off: phase %q was %v writes %v", st.BalPhase, st.TouWas, f.find(tou))
	}
	grid[chargeBalanceEntity] = "off"
	s, f = balSetup(t, grid, map[int]int{108: 0, 146: 254}, "0")
	balTick(t, s, &st)
	if w := f.find(tou); st.BalPhase != "" || st.TouWas != nil || len(w) != 1 {
		t.Errorf("balancing off must restore ToU: phase %q was %v writes %v", st.BalPhase, st.TouWas, w)
	}
}
