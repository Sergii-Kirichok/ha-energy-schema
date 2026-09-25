package config

// ChargeTuning — «глубокие» настройки регулятора заряда. В опциях аддона они
// необязательные и не заданы по умолчанию, поэтому в UI HA скрыты под
// «Показать неиспользуемые необязательные параметры». Повседневные ручки
// (лимиты, пороги SOC) — хелперы HA на дашборде; здесь — их потолки и пороги
// логики, которые меняют редко.
type ChargeTuning struct {
	MaxALimit    float64 // потолок ползунка «Общий лимит», А
	GridALimit   float64 // потолок ползунка «Лимит от сети», А
	FullDaysMax  float64 // потолок ползунка «Полный заряд раз в», дней
	NightBelowW  float64 // PV ниже — ночной режим (полный лимит к утру), Вт
	DayAboveW    float64 // PV выше — снова дневная логика, Вт
	CellCapA     float64 // при разбалансе ячеек ток не выше, А
	CellCapSOC   float64 // …начиная с этого SOC, %
	ZeroOnTarget bool    // на цели на ночь писать 0 (стоп заряда), иначе 1
}

// DefaultCharge — значения, с которыми регулятор работает без настроек.
func DefaultCharge() ChargeTuning {
	return ChargeTuning{MaxALimit: 30, GridALimit: 15, FullDaysMax: 30, NightBelowW: 1000, DayAboveW: 1500,
		CellCapA: 5, CellCapSOC: 70, ZeroOnTarget: true}
}

// chargeOptions — зеркало необязательных опций config.yaml (nil/0 = по умолчанию).
type chargeOptions struct {
	MaxALimit    float64 `json:"charge_max_a_limit"`
	GridALimit   float64 `json:"charge_grid_a_limit"`
	FullDaysMax  float64 `json:"charge_full_days_max"`
	NightBelowW  float64 `json:"charge_night_below_w"`
	DayAboveW    float64 `json:"charge_day_above_w"`
	CellCapA     float64 `json:"charge_cell_cap_a"`
	CellCapSOC   float64 `json:"charge_cell_cap_soc"`
	ZeroOnTarget *bool   `json:"charge_zero_on_target"`
}

func (t *ChargeTuning) apply(o chargeOptions) {
	for _, p := range []struct {
		dst *float64
		v   float64
	}{
		{&t.MaxALimit, o.MaxALimit}, {&t.GridALimit, o.GridALimit}, {&t.FullDaysMax, o.FullDaysMax},
		{&t.NightBelowW, o.NightBelowW}, {&t.DayAboveW, o.DayAboveW}, {&t.CellCapA, o.CellCapA}, {&t.CellCapSOC, o.CellCapSOC},
	} {
		if p.v > 0 {
			*p.dst = p.v
		}
	}
	if o.ZeroOnTarget != nil {
		t.ZeroOnTarget = *o.ZeroOnTarget
	}
	if t.DayAboveW <= t.NightBelowW { // гистерезис обязателен, иначе регистр будет дёргаться
		t.DayAboveW = t.NightBelowW + 500
	}
}
