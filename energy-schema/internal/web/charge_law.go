package web

import (
	"math"

	"energy-schema/internal/config"
)

// Закон регулирования тока заряда (чистая функция, тестируется). Записи в
// инвертор ложатся в его энергонезависимую память (ресурс ячеек конечен),
// поэтому значений всего два: полный «Общий лимит» до цели и 0 на цели —
// в солнечный день 2 записи регистра 108 (стоп на цели, вечером — полный ток
// на утро), в пасмурный, когда цель не достигнута, — ни одной. Плавный спад
// убран (12.10): ~0,2C для LiFePO4 мягко, а каждая ступень — лишняя запись.
const holdHyst = 2.0 // % ниже цели: удержание не снимается, пока SOC не упадёт сюда

// chargeInput — всё, что нужно закону регулирования.
type chargeInput struct {
	SOC, MaxA, TargetSOC float64
	FullDue              bool
	Holding              bool   // прошлый тик был удержанием (гистерезис)
	CellLevel            string // "ok" | "warn" | "bad" (см. cellVerdict)
	Night                bool   // нет генерации — стоим на полном токе к утру
	Tune                 config.ChargeTuning
}

// chargeSetpoint возвращает ток заряда (целые амперы, ≥1) и режим для сенсора.
func chargeSetpoint(in chargeInput) (float64, string) {
	// ночью ток не нужен, а утро должно начаться с полного лимита даже без
	// связи с HA — поэтому на ночь пишем «Общий лимит», а не 0. Полный заряд
	// ночью идёт по своим правилам (хвост малым током), иначе ушёл бы «Общим лимитом».
	if in.Night && !(in.FullDue && in.SOC < 100) {
		return math.Max(chargeMinA, math.Round(in.MaxA)), "night-ready"
	}
	// Полный заряд — не «тянуть к 100 % большим током», а добрать минимальным
	// после цели на ночь: медленный хвост и есть балансировка ячеек.
	target := in.TargetSOC
	mode := "night"
	if in.FullDue {
		mode = "full"
	}
	var a float64
	hold := func() {
		a, mode = 0, mode+"/hold" // 0 = стоп заряда: даже 1 в регистре (×2 канала) набивает до 100 % за день
		if !in.Tune.ZeroOnTarget {
			a = chargeMinA
		}
	}
	switch {
	case in.Holding && !in.FullDue && in.SOC >= target-holdHyst:
		hold() // без дребезга у цели (05.10: 4 пары записей за день)
	case in.SOC < target:
		a, mode = in.MaxA, "max"
	case in.FullDue && in.SOC < 100:
		// до порога хвоста — «Общим лимитом», последние проценты — малым током
		// (мягкий подход к колену LiFePO4, BMS успевает выровнять ячейки)
		a, mode = in.MaxA, "full"
		if t := in.Tune.FullTailSOC; t > 0 && in.SOC >= t {
			a, mode = math.Min(in.MaxA, math.Max(chargeMinA, in.Tune.FullTailA)), "full/tail"
		}
	default:
		hold()
	}
	if in.CellLevel == "bad" && in.SOC >= in.Tune.CellCapSOC && a > in.Tune.CellCapA {
		a, mode = in.Tune.CellCapA, mode+"/cells"
	}
	if a <= 0 {
		return 0, mode
	}
	return math.Max(chargeMinA, math.Round(a)), mode
}
