package scada

// Паспортная ёмкость стойки — из самого оборудования, а не ручного ввода:
// А·ч — регистр 102 инвертора («Battery Capacity», задаётся при установке
// стойки; страница инвертора), число модулей — BMS (рег. 10070, атрибут packs
// сенсора лимитов), номинал модуля LFP 16S — 51,2 В. Проверка 13.10: старая
// стойка 100 А·ч × 12 × 51,2 = 61,4 кВт·ч (в конфиге было 60), новая 315 А·ч.
const (
	BattAhEntity    = "number.deye_sun_30k_battery_capacity"
	bmsLimitsEntity = "sensor.energy_schema_bms_limits"
	ModuleNominalV  = 51.2
)

// BattCapKWh — номинальная ёмкость (кВт·ч) и источник: "inverter" (А·ч × модули)
// или "config" (fallback из настроек аддона, пока инвертор/BMS не прочитаны).
func BattCapKWh(st State, fallback float64) (float64, string) {
	ah, packs := invNum(st, InvBattAh, BattAhEntity), st.AttrNum(bmsLimitsEntity, "packs")
	if ah > 0 && packs > 0 {
		return ah * packs * ModuleNominalV / 1000, "inverter"
	}
	return fallback, "config"
}

// Свежие значения из регистров инвертора (кладёт web при каждом чтении
// 102..130); сущности Solarman — запасной путь (обновляются раз в 5 мин).
const (
	InvBattAh      = "inv.batt_ah"
	InvShutdownSOC = "inv.shutdown_soc"
	InvLowSOC      = "inv.low_soc"
	InvSOC         = "inv.soc" // SOC с десятыми (web/socest.go)
)

// invNum — свежее значение регистра, иначе сущность Solarman.
func invNum(st State, fresh, entity string) float64 {
	if v := st.Num(fresh); v > 0 {
		return v
	}
	return st.Num(entity)
}

// socLimits — пороги батареи: отключение (рег. 115) и «низкий заряд» (117).
func socLimits(st State) (shutdown, low float64) {
	shutdown = invNum(st, InvShutdownSOC, "number.deye_sun_30k_battery_shutdown_soc")
	low = invNum(st, InvLowSOC, "number.deye_sun_30k_battery_low_soc")
	if shutdown <= 0 {
		shutdown = 15
	}
	if low < shutdown {
		low = shutdown
	}
	return
}

// chargeTarget — до какого SOC сейчас заряжаем: «Цель на ночь» регулятора;
// 100 — если полный заряд (по кнопке или по сроку) или регулятор выключен.
func chargeTarget(st State) float64 {
	t := st.Num("input_number.energy_schema_charge_target_soc")
	full := st.State("input_boolean.energy_schema_charge_full_now") == "on" ||
		st.Attr("sensor.energy_schema_charge_setpoint", "full_due") == "true" ||
		st.State("input_boolean.energy_schema_charge_auto") == "off"
	if full || t <= 0 || t > 100 {
		return 100
	}
	return t
}
