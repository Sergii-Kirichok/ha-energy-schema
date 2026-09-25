package scada

// QuickParam — быстрый параметр на обороте карточки АКБ: хелпер HA, шаг кнопок
// «−/+», границы. Единый список для рендера (scada) и обработчика /control (web).
type QuickParam struct {
	Key, Label, Unit, Entity string
	Min, Max, Step           float64
	Bool                     bool // переключатель (input_boolean), а не число
}

// QuickParams — порядок = порядок строк на обороте.
var QuickParams = []QuickParam{
	{Key: "auto", Label: "Авто-регулятор", Entity: "input_boolean.energy_schema_charge_auto", Bool: true},
	{Key: "max_a", Label: "Общий лимит", Unit: "А", Entity: "input_number.energy_schema_charge_max_a", Min: 1, Max: 30, Step: 5},
	{Key: "grid_a", Label: "Лимит от сети", Unit: "А", Entity: "input_number.energy_schema_charge_grid_a", Min: 1, Max: 15, Step: 1},
	{Key: "taper_soc", Label: "Снижать ток с", Unit: "%", Entity: "input_number.energy_schema_charge_taper_soc", Min: 50, Max: 99, Step: 5},
	{Key: "target_soc", Label: "Цель на ночь", Unit: "%", Entity: "input_number.energy_schema_charge_target_soc", Min: 50, Max: 100, Step: 5},
	{Key: "full_days", Label: "Полный раз в", Unit: "д", Entity: "input_number.energy_schema_charge_full_days", Min: 1, Max: 30, Step: 1},
	{Key: "full_now", Label: "Полный заряд сейчас", Entity: "input_boolean.energy_schema_charge_full_now", Bool: true},
}

// QuickParamByKey — поиск по ключу (nil, если нет такого).
func QuickParamByKey(key string) *QuickParam {
	for i := range QuickParams {
		if QuickParams[i].Key == key {
			return &QuickParams[i]
		}
	}
	return nil
}
