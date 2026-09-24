package scada

import "fmt"

// row1 — верхний ряд: Ввод1 (Рыбхоз), три стабилизатора, Ввод2 (Зелёный).
func (f *frame) row1() {
	st, s, cfg := f.st, f.s, f.cfg
	stOn, rybSt, contRyb, emuStale, grnSt := f.stOn, f.rybSt, f.contRyb, f.emuStale, f.grnSt

	// ===================== ROW 1 =====================
	s.box(24, 44, 240, 175)
	s.head(24, 44, 240, "fish", cfg.In1Name, stOn[rybSt])
	for ph := 1; ph <= 3; ph++ {
		y := 108.0 + float64(ph-1)*36
		onE := fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)
		vE := fmt.Sprintf("sensor.sim_ryb_l%d_vin", ph)
		aE := fmt.Sprintf("sensor.sim_ryb_l%d_load", ph)
		ps := rybPhase(st, ph, contRyb)
		dotCol := phCol(st, onE, vE, 200, 250)
		if ps == "lost" {
			dotCol = cOrg
		}
		s.dot(44, y-5, 5, dotCol)
		s.t(60, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if st.On(onE) {
			v := st.Num(vE)
			a := st.Num(aE)
			s.t(252, y, 13, cTxt, "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else if ps == "lost" {
			s.t(252, y, 12, cOrg, "end", "потеря связи")
		} else {
			s.t(252, y, 12, cRed, "end", "обрыв")
		}
	}
	// связь RS-485 со счётчиком ввода (живая, если хоть одна фаза отвечает)
	rybRS := (st.Available("sensor.sim_ryb_l1_on") || st.Available("sensor.sim_ryb_l2_on") || st.Available("sensor.sim_ryb_l3_on")) && !emuStale
	if rybRS {
		s.t(34, 210, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(34, 210, 10, cRed, "start", "RS-485 ✕")
	}
	// Стабилизаторы — по центру между вводами, с увеличенным интервалом
	stabX := []float64{360, 625, 890}
	for i := 0; i < 3; i++ {
		ph := i + 1
		x := stabX[i]
		p := fmt.Sprintf("sensor.sim_ryb_l%d", ph)
		linkOk := st.State(p+"_link") == "ok" && !emuStale
		linkCol := cGrn
		if !linkOk {
			linkCol = cRed
		}
		s.box(x, 44, 190, 175)
		s.head(x, 44, 190, "sine", fmt.Sprintf("Стаб L%d", ph), linkCol)
		// связь RS-485 (как у контактора) — внизу слева
		if linkOk {
			s.t(x+10, 213, 10, cSub, "start", "RS-485 ✓")
		} else {
			s.t(x+10, 213, 10, cRed, "start", "RS-485 ✕")
		}
		if !linkOk {
			// Стабилизатор офлайн. Линия — ОТДЕЛЬНЫЙ источник: если она под
			// напряжением (датчик линии / инвертор) — стабилизатор просто в обходе
			// (байпас), питание идёт мимо. Иначе питание не подтверждено.
			lineAlive := rybPhase(st, ph, contRyb) != "bad"
			if lineAlive {
				s.t(x+95, 92, 14, cOrg, "middle", "ТРАНЗИТ (байпас)")
				s.t(x+95, 110, 10, cSub, "middle", "стабилизатор офлайн")
				// ТЕКУЩЕЕ напряжение линии (её данные доступны, источник отдельный)
				s.t(x+95, 142, 17, cTxt, "middle", fmt.Sprintf("%d В", st.Int(p+"_vin")))
				s.t(x+95, 160, 10, cSub, "middle", "на линии (живое)")
			} else {
				s.t(x+95, 100, 14, cRed, "middle", "НЕТ СВЯЗИ")
				s.t(x+95, 122, 11, cSub, "middle", "питание не подтверждено")
			}
			// последнее, что отдал САМ стабилизатор (выход/ступень) — до сбоя
			if st.LastState(p+"_vout") != "" {
				s.t(x+95, 188, 10, cSub, "middle", fmt.Sprintf("посл. от стаб.: выход %dВ · ст %d", st.LastInt(p+"_vout"), st.LastInt(p+"_step")))
			}
			if info := st.LostInfo(p + "_vout"); info != "" {
				s.t(x+180, 213, 10, cOrg, "end", "молчит "+info)
			}
			continue
		}
		mc, mt := cBlu, "стабилизация"
		if st.State(p+"_mode") == "transit" {
			mc, mt = cSub, "транзит"
		}
		s.t(x+95, 100, 12, mc, "middle", mt)
		loadA := st.Num(p + "_load")
		row := func(n int, label, val, col string) {
			s.t(x+14, 124+float64(n)*22, 11, cSub, "start", label)
			s.t(x+176, 124+float64(n)*22, 12, col, "end", val)
		}
		row(0, "вход → выход", fmt.Sprintf("%d → %dВ", st.Int(p+"_vin"), st.Int(p+"_vout")), cTxt)
		row(1, "ступень", fmt.Sprintf("%d", st.Int(p+"_step")), cTxt)
		row(2, "нагрузка", fmt.Sprintf("%.0fА · %.2fкВт", loadA, loadA*st.Num(p+"_vout")/1000), cTxt)
		row(3, "U мин/макс", fmt.Sprintf("%d / %dВ", st.Int(p+"_vmin"), st.Int(p+"_vmax")), cTxt)
		if !st.On(p + "_on") {
			if rybPhase(st, ph, contRyb) == "lost" {
				s.t(x+180, 213, 10, cOrg, "end", "потеря (датчик)")
			} else {
				s.t(x+180, 213, 10, cRed, "end", "линия отключена")
			}
		}
	}
	// Ввод2 Зелёный — карточка того же размера, что и Рыбхоз (240×175), у правого края
	s.box(1180, 44, 240, 175)
	s.head(1180, 44, 240, "regen", cfg.In2Name, map[string]string{"on": cGrn, "bad": cOrg, "off": cGry}[grnSt])
	// состояние/направление — честно: когда ввод не запитан, не пишем «потребление»
	if grnSt == "off" {
		s.t(1300, 94, 12, cGry, "middle", "ввод отключён")
	} else {
		dt, dc := "потребление", cBlu
		if st.State("sensor.sim_green_dir") == "export" {
			dt, dc = "отдача ↑", cGrn
		}
		s.t(1300, 94, 12, dc, "middle", dt)
	}
	for ph := 1; ph <= 3; ph++ {
		y := 120.0 + float64(ph-1)*32
		onE := fmt.Sprintf("sensor.sim_green_l%d_on", ph)
		vE := fmt.Sprintf("sensor.sim_green_l%d_v", ph)
		aE := fmt.Sprintf("sensor.sim_green_l%d_a", ph)
		s.dot(1200, y-5, 5, phCol(st, onE, vE, 200, 250))
		s.t(1216, y, 13, cTxt, "start", fmt.Sprintf("L%d", ph))
		if st.On(onE) {
			v := st.Num(vE)
			a := st.Num(aE)
			s.t(1408, y, 13, cTxt, "end", fmt.Sprintf("%dВ / %.0fА / %.2fкВт", int(v), a, v*a/1000))
		} else {
			s.t(1408, y, 12, cGry, "end", "— нет —")
		}
	}
	// связь RS-485 со счётчиком ввода
	grnRS := (st.Available("sensor.sim_green_l1_on") || st.Available("sensor.sim_green_l2_on") || st.Available("sensor.sim_green_l3_on")) && !emuStale
	if grnRS {
		s.t(1190, 210, 10, cSub, "start", "RS-485 ✓")
	} else {
		s.t(1190, 210, 10, cRed, "start", "RS-485 ✕")
	}
}
