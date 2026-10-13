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
	ah, packs := st.Num(BattAhEntity), st.AttrNum(bmsLimitsEntity, "packs")
	if ah > 0 && packs > 0 {
		return ah * packs * ModuleNominalV / 1000, "inverter"
	}
	return fallback, "config"
}
