package config

// ChargeTuning — «глубокие» настройки регулятора заряда. В опциях аддона они
// необязательные и не заданы по умолчанию, поэтому в UI HA скрыты под
// «Показать неиспользуемые необязательные параметры». Повседневные ручки
// (лимиты, пороги SOC) — хелперы HA на дашборде; здесь — их потолки и пороги
// логики, которые меняют редко.
type ChargeTuning struct {
	MaxALimit       float64 // потолок ползунка «Общий лимит», А
	GridALimit      float64 // потолок ползунка «Лимит от сети», А
	FullDaysMax     float64 // потолок ползунка «Полный заряд раз в», дней
	NightBelowW     float64 // PV ниже — ночной режим (полный лимит к утру), Вт
	DayAboveW       float64 // PV выше — снова дневная логика, Вт
	CellCapA        float64 // при разбалансе ячеек ток не выше, А
	CellCapSOC      float64 // …начиная с этого SOC, %
	FullTailSOC     float64 // полный заряд: с этого SOC — ток хвоста, %
	FullTailA       float64 // полный заряд: ток хвоста (факт), А
	ZeroOnTarget    bool    // на цели на ночь писать 0 (стоп заряда), иначе 1
	BalanceStopV    float64 // балансировка: порция заряда до этого напряжения макс. ячейки, В
	BalanceSoftMV   float64 // …с этого разбега ячеек — минимальный ток до конца сессии, мВ
	BalanceSoftV    float64 // …с этого напряжения макс. ячейки — минимальный ток до конца сессии, В
	BalancePauseMin float64 // …пауза между порциями не короче, мин
	BalanceResumeV  float64 // …следующая порция, когда макс. ячейка опустилась до, В
	BalanceFastSOC  float64 // …ниже этого SOC (и ячейки на плоской части) — порция «Общим лимитом», %
	BalanceDoneMV   float64 // …выровнено, если разбег на верху не больше, мВ
	BalanceMaxDays  float64 // …выключить через столько дней, даже если не выровнено
}

// DefaultCharge — значения, с которыми регулятор работает без настроек.
func DefaultCharge() ChargeTuning {
	return ChargeTuning{MaxALimit: 30, GridALimit: 15, FullDaysMax: 30, NightBelowW: 1000, DayAboveW: 1500,
		CellCapA: 5, CellCapSOC: 70, FullTailSOC: 95, FullTailA: 6, ZeroOnTarget: true,
		BalanceStopV: 3.55, BalanceSoftMV: 10, BalanceSoftV: 3.45, BalancePauseMin: 60, BalanceResumeV: 3.45, BalanceFastSOC: 97, BalanceDoneMV: 40, BalanceMaxDays: 3}
}

// chargeOptions — зеркало необязательных опций config.yaml (nil/0 = по умолчанию).
type chargeOptions struct {
	MaxALimit       float64 `json:"charge_max_a_limit"`
	GridALimit      float64 `json:"charge_grid_a_limit"`
	FullDaysMax     float64 `json:"charge_full_days_max"`
	NightBelowW     float64 `json:"charge_night_below_w"`
	DayAboveW       float64 `json:"charge_day_above_w"`
	CellCapA        float64 `json:"charge_cell_cap_a"`
	CellCapSOC      float64 `json:"charge_cell_cap_soc"`
	FullTailSOC     float64 `json:"charge_full_tail_soc"`
	FullTailA       float64 `json:"charge_full_tail_a"`
	ZeroOnTarget    *bool   `json:"charge_zero_on_target"`
	BalanceStopV    float64 `json:"charge_balance_stop_v"`
	BalanceSoftMV   float64 `json:"charge_balance_soft_mv"`
	BalanceSoftV    float64 `json:"charge_balance_soft_v"`
	BalancePauseMin float64 `json:"charge_balance_pause_min"`
	BalanceResumeV  float64 `json:"charge_balance_resume_v"`
	BalanceFastSOC  float64 `json:"charge_balance_resume_soc"`
	BalanceDoneMV   float64 `json:"charge_balance_done_mv"`
	BalanceMaxDays  float64 `json:"charge_balance_max_days"` // charge_balance_resume_soc, _max_cycles — не используются с 0.6.54
}

func (t *ChargeTuning) apply(o chargeOptions) {
	for _, p := range []struct {
		dst *float64
		v   float64
	}{
		{&t.MaxALimit, o.MaxALimit}, {&t.GridALimit, o.GridALimit}, {&t.FullDaysMax, o.FullDaysMax},
		{&t.NightBelowW, o.NightBelowW}, {&t.DayAboveW, o.DayAboveW}, {&t.CellCapA, o.CellCapA}, {&t.CellCapSOC, o.CellCapSOC},
		{&t.FullTailSOC, o.FullTailSOC}, {&t.FullTailA, o.FullTailA},
		{&t.BalanceStopV, o.BalanceStopV}, {&t.BalanceSoftMV, o.BalanceSoftMV}, {&t.BalanceSoftV, o.BalanceSoftV}, {&t.BalancePauseMin, o.BalancePauseMin}, {&t.BalanceResumeV, o.BalanceResumeV}, {&t.BalanceFastSOC, o.BalanceFastSOC}, {&t.BalanceDoneMV, o.BalanceDoneMV}, {&t.BalanceMaxDays, o.BalanceMaxDays},
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
