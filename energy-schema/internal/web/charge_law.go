package web

import (
	"math"

	"energy-schema/internal/config"
)

// Закон регулирования тока заряда (чистая функция, тестируется). Записи в
// инвертор ложатся в его энергонезависимую память (ресурс ячеек конечен),
// поэтому закон даёт мало разных значений: спад — ступенями, у цели —
// гистерезис удержания.
const (
	taperSteps = 4.0 // ступеней спада от «снижать с» до цели (100/75/50/25 % лимита)
	holdHyst   = 2.0 // % ниже цели: удержание не снимается, пока SOC не упадёт сюда
)

// chargeInput — всё, что нужно закону регулирования.
type chargeInput struct {
	SOC, MaxA, TaperSOC, TargetSOC float64
	FullDue                        bool
	Holding                        bool   // прошлый тик был удержанием (гистерезис)
	CellLevel                      string // "ok" | "warn" | "bad" (см. cellVerdict)
	Night                          bool   // нет генерации — стоим на полном токе к утру
	Tune                           config.ChargeTuning
}

// chargeSetpoint возвращает ток заряда (целые амперы, ≥1) и режим для сенсора.
func chargeSetpoint(in chargeInput) (float64, string) {
	// ночью ток не нужен, а утро должно начаться с полного лимита даже без
	// связи с HA — поэтому на ночь пишем «Общий лимит», а не 0/спад
	if in.Night {
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
	// «снижать с» ≥ цели — ошибка настройки (30.09: 99 % при цели 90 → полный
	// ток до цели без спада). Спад всё равно нужен: начинаем за taperGap до цели.
	if in.TaperSOC >= target {
		in.TaperSOC = target - taperGap
	}
	hold := func() {
		a, mode = 0, mode+"/hold" // 0 = стоп заряда: даже 1 в регистре (×2 канала) набивает до 100 % за день
		if !in.Tune.ZeroOnTarget {
			a = chargeMinA
		}
	}
	switch {
	case in.Holding && !in.FullDue && in.SOC >= target-holdHyst:
		hold() // без дребезга 0↔1 у цели (05.10: 4 пары записей за день)
	case in.SOC < in.TaperSOC:
		a, mode = in.MaxA, "max"
	case in.SOC < target:
		f := (target - in.SOC) / (target - in.TaperSOC)
		a = in.MaxA * math.Ceil(f*taperSteps) / taperSteps
		mode += "/taper"
	case in.FullDue && in.SOC < 100:
		a, mode = chargeMinA, "full/trickle"
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
