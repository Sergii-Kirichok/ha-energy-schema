package scada

import "fmt"

// batteryBack — оборот карточки АКБ: быстрые настройки регулятора заряда.
// Рисуется в тех же координатах (24,520 300×280) внутри <g class="face f-back">;
// лицевая/оборотная сторона переключается на клиенте (data-flip), значения
// правятся в браузере (data-qn/data-qb/data-qd) и уходят разом по «Сохранить» → POST /control?act=qsave.
func (f *frame) batteryBack() {
	st, s := f.st, f.s
	s.p(`<g class="face f-back" visibility="hidden">`) // скрыта и без CSS страницы (ТВ/картинка)
	s.box(24, 520, 300, 280)
	s.head(24, 520, 300, "batt", "АКБ · заряд", "")
	// клик по иконке / заголовку — назад на лицевую сторону
	s.p(`<rect x="28" y="524" width="150" height="32" rx="6" fill="transparent" style="cursor:pointer" data-flip="batt"/>`)

	// Правка в браузере без немедленной записи (index_html.go): −/+ и тумблеры
	// меняют значение только на экране (рамка строки оранжевая), «Сохранить»
	// отправляет всё разом; после перечитывания — зелёная (или красная) рамка.
	y := 578.0
	for _, q := range QuickParams {
		s.p(`<rect data-qrow="%s" x="32" y="%g" width="284" height="26" rx="6" fill="none" stroke="transparent" stroke-width="1.5"/>`, q.Key, y-19)
		s.t(40, y, 12, cSub, "start", q.Label)
		if q.Bool {
			v, col, txt := 0, cGry, "ВЫКЛ"
			if st.On(q.Entity) {
				v, col, txt = 1, cGrn, "ВКЛ"
			}
			if !st.Available(q.Entity) {
				col, txt = cGry, "—"
			}
			s.p(`<g data-qb="%s" data-v="%d" style="cursor:pointer"><rect x="230" y="%g" width="78" height="22" rx="6" fill="%s" fill-opacity="0.18" stroke="%s"/>`, q.Key, v, y-16, col, col)
			s.p(`<text x="269" y="%g" font-size="12" fill="%s" text-anchor="middle">%s</text></g>`, y, col, txt)
		} else {
			val, v := "—", ""
			if st.Available(q.Entity) {
				val, v = fmt.Sprintf("%.0f %s", st.Num(q.Entity), q.Unit), fmt.Sprintf("%g", st.Num(q.Entity))
			}
			s.p(`<text data-qn="%s" data-v="%s" data-min="%g" data-max="%g" data-u="%s" x="196" y="%g" font-size="14" fill="%s" text-anchor="end">%s</text>`,
				q.Key, v, q.Min, q.Max, q.Unit, y, cTxt, val)
			for i, d := range []float64{-q.Step, q.Step} {
				x := 214 + float64(i)*48
				sign := "−"
				if d > 0 {
					sign = "+"
				}
				s.p(`<g data-qd="%s" data-d="%g" style="cursor:pointer"><rect x="%g" y="%g" width="42" height="22" rx="6" fill="transparent" stroke="%s"/>`, q.Key, d, x, y-16, cBrd)
				s.t(x+21, y, 13, cTxt, "middle", fmt.Sprintf("%s%.0f", sign, q.Step))
				s.p(`</g>`)
			}
		}
		y += 26
	}
	s.p(`<g data-qsave style="cursor:pointer"><rect x="214" y="728" width="94" height="24" rx="6" fill="%s" fill-opacity="0.12" stroke="%s"/>`, cBrd, cBrd)
	s.p(`<text x="261" y="745" font-size="12" fill="%s" text-anchor="middle">Сохранить</text></g>`, cSub)
	// итог: что регулятор пишет прямо сейчас
	sp := "—"
	if st.Available("sensor.energy_schema_charge_setpoint") {
		sp = fmt.Sprintf("%.0f А (%s)", st.Num("sensor.energy_schema_charge_setpoint"), st.Attr("sensor.energy_schema_charge_setpoint", "mode"))
	}
	s.t(174, 776, 10, cSub, "middle", "ограничение "+sp+" · reg108 = "+st.State("number.deye_sun_30k_battery_max_charging_current")+" · reg128 = "+st.State("number.deye_sun_30k_battery_grid_charging_current"))
	s.t(174, 790, 10, cSub, "middle", "следующий 100 %: "+st.State("sensor.energy_schema_charge_next_full")+" · SOC "+fmt.Sprintf("%.0f%%", st.Num("sensor.deye_sun_30k_battery")))
	s.p(`</g>`)
}
