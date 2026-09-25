package scada

import "fmt"

// batteryBack — оборот карточки АКБ: быстрые настройки регулятора заряда.
// Рисуется в тех же координатах (24,520 300×280) внутри <g class="face f-back">;
// лицевая/оборотная сторона переключается на клиенте (data-flip), значения
// меняются кнопками data-set="key:±step|toggle" → POST /control?act=param.
func (f *frame) batteryBack() {
	st, s := f.st, f.s
	s.p(`<g class="face f-back">`)
	s.box(24, 520, 300, 280)
	s.head(24, 520, 300, "batt", "АКБ · заряд", "")
	// «↩» — назад на лицевую сторону
	s.p(`<g data-flip="batt" style="cursor:pointer"><rect x="284" y="528" width="30" height="22" rx="6" fill="transparent" stroke="%s"/>`, cBrd)
	s.t(299, 544, 13, cSub, "middle", "↩")
	s.p(`</g>`)

	y := 578.0
	for _, q := range QuickParams {
		s.t(40, y, 12, cSub, "start", q.Label)
		if q.Bool {
			on := st.On(q.Entity)
			col, txt := cGry, "ВЫКЛ"
			if on {
				col, txt = cGrn, "ВКЛ"
			}
			if !st.Available(q.Entity) {
				col, txt = cGry, "—"
			}
			s.p(`<g data-set="%s:toggle" style="cursor:pointer"><rect x="230" y="%g" width="78" height="22" rx="6" fill="%s" fill-opacity="0.18" stroke="%s"/>`, q.Key, y-16, col, col)
			s.t(269, y, 12, col, "middle", txt)
			s.p(`</g>`)
		} else {
			val := "—"
			if st.Available(q.Entity) {
				val = fmt.Sprintf("%.0f %s", st.Num(q.Entity), q.Unit)
			}
			s.t(196, y, 14, cTxt, "end", val)
			for i, d := range []float64{-q.Step, q.Step} {
				x := 214 + float64(i)*48
				sign := "−"
				if d > 0 {
					sign = "+"
				}
				s.p(`<g data-set="%s:%+g" style="cursor:pointer"><rect x="%g" y="%g" width="42" height="22" rx="6" fill="transparent" stroke="%s"/>`, q.Key, d, x, y-16, cBrd)
				s.t(x+21, y, 13, cTxt, "middle", fmt.Sprintf("%s%.0f", sign, q.Step))
				s.p(`</g>`)
			}
		}
		y += 30
	}
	// итог: что регулятор пишет прямо сейчас
	sp := "—"
	if st.Available("sensor.energy_schema_charge_setpoint") {
		sp = fmt.Sprintf("%.0f А (%s)", st.Num("sensor.energy_schema_charge_setpoint"), st.Attr("sensor.energy_schema_charge_setpoint", "mode"))
	}
	s.t(174, 776, 10, cSub, "middle", "уставка "+sp+" · reg108 = "+st.State("number.deye_sun_30k_battery_max_charging_current")+" · reg128 = "+st.State("number.deye_sun_30k_battery_grid_charging_current"))
	s.t(174, 790, 10, cSub, "middle", "следующий 100 %: "+st.State("sensor.energy_schema_charge_next_full")+" · SOC "+fmt.Sprintf("%.0f%%", st.Num("sensor.deye_sun_30k_battery")))
	s.p(`</g>`)
}
