package web

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Карточка «Батарея» на дашборде Bobrixos-Energy (entities-card с
// sensor.deye_sun_30k_battery_soh). Аддон один раз при старте дописывает в неё
// строки BMS: SOH от BMS вместо расчётного, ячейки min/max, разбег, баланс,
// циклы. Идемпотентно: если строки уже есть — ничего не трогает. Выключается
// опцией bms_dashboard: "" (или переименованием дашборда).
const dashAnchorEntity = "sensor.deye_sun_30k_battery_soh"

var dashBMSRows = []map[string]any{
	{"entity": bmsSOHEntity, "name": "Здоровье (SOH)"},
	{"entity": bmsCells, "name": "Ячейки мин / макс"},
	{"entity": "sensor.energy_schema_bms_cell_delta", "name": "Разбег ячеек"},
	{"entity": "sensor.energy_schema_bms_balance", "name": "Баланс ячеек"},
	{"entity": "sensor.energy_schema_bms_cycles", "name": "Циклы (BMS)"},
}

// appendCardOnce appends card to the cards list holding the battery card
// unless `present` says it is already there.
func appendCardOnce(node any, present bool, card map[string]any) (bool, error) {
	if present {
		return false, nil
	}
	cards := findCardsHolding(node, findEntitiesCard(node))
	if cards == nil {
		return false, fmt.Errorf("no cards list holding the battery card")
	}
	holder, key := cards[0].(map[string]any), cards[1].(string)
	holder[key] = append(holder[key].([]any), card)
	return true, nil
}

// ensureDashboard adds the BMS rows to the battery card and the charge card if missing.
func (s *Server) ensureDashboard(urlPath string) {
	// HA может ещё не принимать соединения (27.10: «connection refused» сразу
	// после обновления аддона — дашборд остался неправленым) — повторяем
	raw, err := s.client.LovelaceConfig(urlPath)
	for try := 1; err != nil && try < 10; try++ {
		log.Printf("dashboard %s: %v — retry in 30 s", urlPath, err)
		time.Sleep(30 * time.Second)
		raw, err = s.client.LovelaceConfig(urlPath)
	}
	if err != nil {
		log.Printf("dashboard %s: %v", urlPath, err)
		return
	}
	var cfg any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Printf("dashboard %s: decode: %v", urlPath, err)
		return
	}
	mm, _ := patchMinMax(cfg) // первым: иначе карточка АКБ получит сводную строку дважды
	changed, err := patchBatteryCard(cfg)
	if err != nil {
		log.Printf("dashboard %s: %v", urlPath, err)
		return
	}
	changed = changed || mm
	for _, p := range []struct {
		name  string
		patch func(any) (bool, error)
	}{{"charge card", patchChargeCard}, {"delta cleanup", patchDeltaCleanup}, {"grid rows", patchGridRows}, {"grid card", patchGridCard},
		{"bms table", patchBMSTable}, {"bms cards", patchBMSCards}, {"flow battery", patchFlowBattery}, {"battery power", patchBattPower}, {"gauges", patchGauges},
		{"control visibility", s.patchControlVisibility}} { // последним: карточки, созданные выше, тоже
		if c2, err := p.patch(cfg); err != nil {
			log.Printf("dashboard %s: %s: %v", urlPath, p.name, err)
		} else {
			changed = changed || c2
		}
	}
	if !changed {
		return
	}
	out, _ := json.Marshal(cfg)
	if err := s.client.SaveLovelaceConfig(urlPath, out); err != nil {
		log.Printf("dashboard %s: save: %v", urlPath, err)
		return
	}
	log.Printf("dashboard %s: updated", urlPath)
}

// patchBatteryCard walks the decoded dashboard JSON, finds the first entities
// card containing dashAnchorEntity and inserts the missing BMS rows right after
// the anchor row. Returns whether anything changed.
func patchBatteryCard(node any) (bool, error) {
	card := findEntitiesCard(node)
	if card == nil {
		return false, fmt.Errorf("no entities card with %s or %s found", dashAnchorEntity, bmsSOHEntity)
	}
	ents, _ := card["entities"].([]any)
	have := map[string]bool{}
	anchor, changed := -1, false
	for i, e := range ents {
		id := entityID(e)
		have[id] = true
		if id == flowBattHalved {
			if m, ok := innerRow(e).(map[string]any); ok {
				m["entity"] = flowBattCorrect
			} else {
				ents[i] = map[string]any{"entity": flowBattCorrect, "name": "Мощность"}
			}
			have[flowBattCorrect], changed = true, true
		}
		if id == dashAnchorEntity && anchor < 0 {
			// расчётный SOH Solarman (92 % при 97 % от BMS) заменяем нашей строкой
			// на том же месте — две строки «здоровье» только путают
			ents[i] = dashBMSRows[0]
			have[bmsSOHEntity] = true
			anchor, changed = i, true
		} else if id == bmsSOHEntity && anchor < 0 {
			anchor = i
		}
	}
	var add []any
	for _, r := range dashBMSRows[1:] {
		if !have[r["entity"].(string)] {
			add = append(add, r)
		}
	}
	out := append([]any{}, ents[:anchor+1]...)
	out = append(out, add...)
	out = append(out, ents[anchor+1:]...)
	// дубли одной сущности (например, два SOH после старых версий) — оставляем первую
	seen := map[string]bool{}
	ded := out[:0]
	for _, e := range out {
		if id := rowKey(e); id != "" && seen[id] {
			continue
		} else if id != "" {
			seen[id] = true
		}
		ded = append(ded, e)
	}
	if len(add) == 0 && len(ded) == len(ents) && !changed {
		return false, nil
	}
	card["entities"] = ded
	return true, nil
}

// Карточка «Поток энергии» (power-flow-card-plus) показывала мощность АКБ из
// sensor.*_battery_power — Solarman отдаёт половину (батарея на двух каналах
// инвертора). Переключаем на template-сенсор V×I, который считает верно.
const (
	flowBattHalved  = "sensor.deye_sun_30k_battery_power"
	flowBattCorrect = "sensor.deye_battery_power_kw"
)

func patchFlowBattery(node any) (bool, error) {
	card := findOwnTypeCard(node, "custom:power-flow-card-plus")
	if card == nil {
		return false, nil
	}
	ents, _ := card["entities"].(map[string]any)
	batt, _ := ents["battery"].(map[string]any)
	if batt == nil || batt["entity"] != flowBattHalved {
		return false, nil
	}
	batt["entity"] = flowBattCorrect
	return true, nil
}

// Разбег ячеек отдельной карточкой пользователю не нужен: старые варианты
// (gauge, markdown, conditional tiles) вычищаем, строка остаётся в карточке.
const dashDeltaEntity = "sensor.energy_schema_bms_cell_delta"

func patchDeltaCleanup(node any) (bool, error) {
	changed := removeOwnEntityCard(node, "gauge", dashDeltaEntity)
	// только старая строка разбега, а не таблица батарей (в ней тот же сенсор)
	if md := findMarkdownWith(node, "▲ Разбег ячеек"); md != nil {
		changed = removeCard(node, md) || changed
	}
	for c := findConditionalTile(node, dashDeltaEntity); c != nil; c = findConditionalTile(node, dashDeltaEntity) {
		if !removeCard(node, c) {
			break
		}
		changed = true
	}
	return changed, nil
}
