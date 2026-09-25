package scada

import "math"

// flows — потоки между карточками (рисуются под ними).
func (f *frame) flows() {
	st, s := f.st, f.s
	stOn, contOn, contRyb, avrPos, avrStuck := f.stOn, f.contOn, f.contRyb, f.avrPos, f.avrStuck
	grnSt, exporting, gridIn, bp, pvtot, genRun := f.grnSt, f.exporting, f.gridIn, f.bp, f.pvtot, f.genRun

	// ===== FLOWS (under boxes) =====
	// Рыбхоз: КАЖДАЯ фаза — своё состояние; обрыв одной не валит остальные.
	// L2/L3 разведены по высоте (верх y=30 и y=14), стояк L2 правее (x=328) —
	// чтобы маркеры аварии («?»/✕) не наезжали на L1 и друг на друга.
	s.flow(cGrn, rybPhase(st, 1, contRyb), 2, false, 264, 108, 360, 108)
	s.flow(cGrn, rybPhase(st, 2, contRyb), 2, false, 264, 144, 328, 144, 328, 30, 720, 30, 720, 44)
	s.flow(cGrn, rybPhase(st, 3, contRyb), 2, false, 264, 180, 284, 180, 284, 14, 985, 14, 985, 44)
	// выходы 3 стабилизаторов -> общая шина (y=275) -> Контактор и АВР(резерв)
	out1, out2, out3 := stabOut(st, 1, contRyb), stabOut(st, 2, contRyb), stabOut(st, 3, contRyb)
	// стабилизаторы отдают мощность, только если есть потребитель: контактор на
	// Ввод1 ИЛИ АВР в резерве. Иначе выходы/шина — серые штрихованые (нет нагрузки).
	stabConsumer := contRyb || avrPos == "reserve"
	og := func(realSt string) string {
		if stabConsumer {
			return realSt
		}
		return "off"
	}
	s.flow(cGrn, og(out1), 3, false, 455, 219, 455, 275)
	s.flow(cGrn, og(out2), 3, false, 720, 219, 720, 275)
	s.flow(cGrn, og(out3), 3, false, 985, 219, 985, 275)
	// шина: зелёная если все выходы в норме; оранжевая при потере; серая штрихованая
	// если всё off ИЛИ нет потребителя.
	busSt := "off"
	if out1 == "on" && out2 == "on" && out3 == "on" {
		busSt = "on"
	} else if out1 != "off" || out2 != "off" || out3 != "off" {
		busSt = "lost"
	}
	if !stabConsumer {
		busSt = "off"
	}
	busDash := ""
	switch busSt {
	case "lost":
		busDash = "6 5"
	case "off":
		busDash = "7 7"
	}
	s.poly(stOn[busSt], 3, busDash, 455, 275, 985, 275)
	// шина -> контактор: активна только когда контактор на Ввод1 (стабилизаторы);
	// заводим к центру карточки контактора (x≈132, рядом с Ввод2 на 156)
	s.flow(cGrn, map[bool]string{true: busSt, false: "off"}[contRyb], 3, false, 455, 275, 132, 275, 132, 300)
	// шина -> АВР(резерв): активна только когда АВР в резерве. Падаем вертикально
	// прямо с шины в точке x=822 (раньше шёл 985->905 по самой шине — наложение).
	s.flow(cGrn, map[bool]string{true: busSt, false: "off"}[avrPos == "reserve"], 3, false, 822, 275, 822, 300)
	// точки на узлах шины стабилизаторов (соединение по правилам): выходы стабов и
	// ответвления к контактору/АВР
	for _, jx := range []float64{455, 720, 822} {
		s.dot(jx, 275, 3.5, cSub)
	}
	// Ввод2 -> Контактор: активна только когда контактор на Ввод2; выходим снизу
	// карточки Зелёного (x=1300) и заводим к центру контактора (x=156)
	s.flow(cBlu, map[bool]string{true: grnSt, false: "off"}[contOn], 2, exporting, 1300, 219, 1300, 252, 156, 252, 156, 300)
	// Контактор -> Инвертор: цвет по активному вводу (зелёный=стабилизаторы, синий=Зелёный)
	cSt := "on"
	if !gridIn {
		cSt = "bad"
	}
	inCol := cGrn
	if contOn {
		inCol = cBlu
	}
	s.flow(inCol, cSt, 2, false, 264, 380, 320, 380)
	// Инвертор -> АВР (осн.). Если АВР залип на резерве, а инвертор всё ещё кормит —
	// рисуем оранжевым (поток есть там, где его быть не должно).
	if avrStuck {
		s.flow(cOrg, "on", 4, false, 660, 380, 717, 380)
	} else {
		s.flow(cGrn, map[bool]string{true: "on", false: "off"}[avrPos == "inverter"], 4, false, 660, 380, 717, 380)
	}
	// АВР -> Дом
	// АВР дома -> АВР ген. (вход 2) -> Дом; генератор -> АВР ген. (вход 1) напрямую,
	// минуя инвертор: питание Дома, когда нет ни Ввода 1, ни инвертора
	a3 := avr3Pos(st)
	s.flow(cGrn, map[bool]string{true: "on", false: "off"}[a3 != "gen"], 3, false, 917, 380, 973, 380)
	s.flow(cGrn, "on", 3, false, 1133, 380, 1190, 380)
	// Батарея <-> Инвертор
	// Батарея <-> Инвертор: движение только при заряде/разряде; в покое (idle) — статичная линия.
	// Горизонталь на одном уровне с линией генератора (y=494).
	if math.Abs(bp) > 20 {
		s.flow(cPur, "on", math.Abs(bp)/1000, bp < 0, 174, 520, 174, 494, 390, 494, 390, 475)
	} else {
		s.poly(cPur, 3, "", 174, 520, 174, 494, 390, 494, 390, 475)
	}
	// PV -> Инвертор
	s.flow(cAmb, map[bool]string{true: "on", false: "off"}[pvtot > 30], pvtot/1000, false, 460, 520, 460, 475)
	// Генератор -> Инвертор: одна силовая линия. Управляющий сигнал отдельной линией
	// не рисуем — он показан значком «G» в правом нижнем углу карточки инвертора.
	// одна общая линия от генератора: ствол вверх до узла на y=494, дальше
	// ветка влево в инвертор (вход GEN) и ветка вверх в АВР ген. (вход 1)
	genOn := map[bool]string{true: "on", false: "off"}
	s.flow(cGrn, genOn[genRun], 2, false, genTrunkX, 520, genTrunkX, 494)
	s.flow(cGrn, genOn[genRun], 2, false, genTrunkX, 494, 508, 494, 508, 475)
	s.flow(cGrn, genOn[genRun && a3 == "gen"], 2, false, genTrunkX, 494, genTrunkX, 475)
	s.dot(genTrunkX, 494, 3.5, cSub)
}
