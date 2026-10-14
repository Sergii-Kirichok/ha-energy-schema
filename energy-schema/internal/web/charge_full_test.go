package web

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestPlanFull(t *testing.T) {
	// 194 кВт·ч, 96→100 %: 7,7 кВт·ч, до хвоста (95 %) уже дошли — весь остаток хвостом 6 А ≈ 3,9 кВт
	base := fullIn{NeedKWh: 7.7, BulkKWh: 0, AvgLoadKW: 2, HoursSun: 5, HoursMidnight: 10, MaxKW: 19, TailKW: 3.9, GridKW: 2.6, GridOK: true}
	sunny := base
	sunny.SolarLeftKWh = 40 // излишек (40−10)/5 = 6 кВт > хвоста → 7,7/3,9 ≈ 2 ч
	if p := planFull(sunny); p.UseGrid || p.Short || math.Abs(p.Hours-2) > 0.1 {
		t.Errorf("sunny: %+v", p)
	}
	cloudy := base
	cloudy.SolarLeftKWh = 11 // излишек 0,2 кВт — не успеть до заката → сеть
	if p := planFull(cloudy); !p.UseGrid || p.Short || p.Hours > 10 {
		t.Errorf("cloudy: %+v", p)
	}
	noGrid := cloudy
	noGrid.GridOK = false
	if p := planFull(noGrid); p.UseGrid || !p.Short {
		t.Errorf("no grid: %+v", p)
	}
	slow := cloudy
	slow.NeedKWh, slow.BulkKWh, slow.HoursMidnight = 60, 50, 3 // 60 кВт·ч за 3 ч при ≤ 2,8 кВт — нет
	if p := planFull(slow); !p.UseGrid || !p.Short {
		t.Errorf("slow: %+v", p)
	}
	if p := planFull(fullIn{}); p.Why != "заряжено" {
		t.Errorf("full: %+v", p)
	}
}

// ToU выключается одним регистром 146 и возвращается как было.
func TestTouOffAndRestore(t *testing.T) {
	prev := chargeFile
	chargeFile = filepath.Join(t.TempDir(), "charge.json")
	t.Cleanup(func() { chargeFile = prev })
	s, f := tickSetup(t, nil, map[int]int{146: 255})
	st := recentState()
	s.touPatchNow(&st, st.LastFull)
	w := f.find("/services/solarman/write_multiple_holding_registers")
	if len(w) != 1 || w[0].body["register"] != 146.0 || w[0].body["values"].([]any)[0] != 254.0 || st.TouWas == nil {
		t.Fatalf("ToU off: %v, was=%v", w, st.TouWas)
	}
	s.touPatchNow(&st, st.LastFull) // уже выключено нами — второй записи нет
	s.touRestore(&st)
	w = f.find("/services/solarman/write_multiple_holding_registers")
	if len(w) != 2 || w[1].body["values"].([]any)[0] != 255.0 || st.TouWas != nil {
		t.Errorf("restore: %v", w)
	}
	if loadChargeState(chargeFile).TouWas != nil {
		t.Error("restored state must be persisted")
	}
}

// Решение «ToU выкл» по прогнозу ждёт touSettle — моргание сети не пишет 146.
func TestFullChargeTouSettle(t *testing.T) {
	prev, prevS := chargeFile, touSettle
	chargeFile = filepath.Join(t.TempDir(), "charge.json")
	t.Cleanup(func() { chargeFile, touSettle = prev, prevS })
	s, f := tickSetup(t, map[string]string{"binary_sensor.deye_sun_30k_grid": "on", gridChargeSwitch: "on",
		"sun.sun": "below_horizon", "sensor.deye_sun_30k_battery_voltage": "640"}, map[int]int{146: 255})
	s.cfg.BattCap = 60 // ёмкость из настроек (инвертор и BMS в тесте не прочитаны)
	st := recentState()
	touSettle = time.Hour
	s.fullCharge(&st, true, 96, 10, 8) // ночь, солнца нет → нужна сеть, но ещё не подтверждено
	if n := len(f.find("/services/solarman/write_multiple_holding_registers")); n != 0 || st.TouWas != nil {
		t.Fatalf("must wait touSettle, writes=%d", n)
	}
	touSettle = 0
	s.fullCharge(&st, true, 96, 10, 8)
	if n := len(f.find("/services/solarman/write_multiple_holding_registers")); n != 1 || st.TouWas == nil {
		t.Fatalf("after settle ToU must go off, writes=%d", n)
	}
	s.fullCharge(&st, false, 100, 10, 8) // полный заряд закончен — возврат сразу
	if n := len(f.find("/services/solarman/write_multiple_holding_registers")); n != 2 || st.TouWas != nil {
		t.Errorf("restore at the end must be immediate, writes=%d", n)
	}
}
