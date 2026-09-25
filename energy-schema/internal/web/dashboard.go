package web

import (
	"encoding/json"
	"fmt"
	"log"
)

// Карточка «Батарея» на дашборде Bobrixos-Energy (entities-card с
// sensor.deye_sun_30k_battery_soh). Аддон один раз при старте дописывает в неё
// строки BMS: SOH от BMS вместо расчётного, ячейки min/max, разбег, баланс,
// циклы. Идемпотентно: если строки уже есть — ничего не трогает. Выключается
// опцией bms_dashboard: "" (или переименованием дашборда).
const dashAnchorEntity = "sensor.deye_sun_30k_battery_soh"

var dashBMSRows = []map[string]any{
	{"entity": bmsSOHEntity, "name": "Здоровье (SOH)"},
	{"entity": "sensor.energy_schema_bms_cell_max", "name": "Ячейка макс"},
	{"entity": "sensor.energy_schema_bms_cell_min", "name": "Ячейка мин"},
	{"entity": "sensor.energy_schema_bms_cell_delta", "name": "Разбег ячеек"},
	{"entity": "sensor.energy_schema_bms_balance", "name": "Баланс ячеек"},
	{"entity": "sensor.energy_schema_bms_cycles", "name": "Циклы (BMS)"},
}

// Карточка «Заряд АКБ» — хелперы регулятора + его сенсоры; добавляется в ту же
// секцию, где карточка «Батарея», если её ещё нет (маркер — переключатель авто).
const dashChargeMarker = "input_boolean.energy_schema_charge_auto"

var dashChargeCard = map[string]any{
	"type":               "entities",
	"show_header_toggle": false,
	"title":              "Заряд АКБ",
	"entities": []any{
		map[string]any{"entity": dashChargeMarker, "name": "Авто-регулятор"},
		map[string]any{"entity": "sensor.energy_schema_charge_setpoint", "name": "Уставка тока"},
		map[string]any{"entity": "input_number.energy_schema_charge_max_a", "name": "Общий лимит, А"},
		map[string]any{"entity": "input_number.energy_schema_charge_grid_a", "name": "Лимит от сети, А"},
		map[string]any{"entity": "input_number.energy_schema_charge_taper_soc", "name": "Снижать ток с, %"},
		map[string]any{"entity": "input_number.energy_schema_charge_target_soc", "name": "Цель на ночь, %"},
		map[string]any{"entity": "input_number.energy_schema_charge_full_days", "name": "Полный заряд раз в, дней"},
		map[string]any{"entity": "input_boolean.energy_schema_charge_full_now", "name": "Полный заряд сейчас"},
		map[string]any{"entity": "sensor.energy_schema_charge_next_full", "name": "Следующий 100 %"},
	},
}

// patchChargeCard appends dashChargeCard next to the battery card unless a card
// with the marker entity already exists anywhere in the dashboard.
func patchChargeCard(node any) (bool, error) {
	card := findCardWith(node, dashChargeMarker)
	if card == nil {
		return appendCardOnce(node, false, dashChargeCard)
	}
	changed := false
	// общий переключатель в заголовке включал бы ВСЕ тумблеры сразу (в т.ч.
	// «Полный заряд сейчас») — убираем
	if card["show_header_toggle"] != false {
		card["show_header_toggle"], changed = false, true
	}
	// карточка есть — дорисовать строки, появившиеся в новых версиях, на их
	// место по шаблону (строки пользователя и порядок сохраняются)
	ents, _ := card["entities"].([]any)
	have := map[string]bool{}
	for _, e := range ents {
		have[entityID(e)] = true
	}
	tmpl := dashChargeCard["entities"].([]any)
	for i, r := range tmpl {
		id := entityID(r)
		if have[id] {
			continue
		}
		pos := 0 // сразу после ближайшей предыдущей строки шаблона, что уже есть
	prev:
		for k := i - 1; k >= 0; k-- {
			for j, e := range ents {
				if entityID(e) == entityID(tmpl[k]) {
					pos = j + 1
					break prev
				}
			}
		}
		ents = append(ents[:pos], append([]any{r}, ents[pos:]...)...)
		have[id], changed = true, true
	}
	if changed {
		card["entities"] = ents
	}
	return changed, nil
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
	raw, err := s.client.LovelaceConfig(urlPath)
	if err != nil {
		log.Printf("dashboard %s: %v", urlPath, err)
		return
	}
	var cfg any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Printf("dashboard %s: decode: %v", urlPath, err)
		return
	}
	changed, err := patchBatteryCard(cfg)
	if err != nil {
		log.Printf("dashboard %s: %v", urlPath, err)
		return
	}
	for name, patch := range map[string]func(any) (bool, error){"charge card": patchChargeCard, "delta cleanup": patchDeltaCleanup, "grid rows": patchGridRows, "flow battery": patchFlowBattery} {
		if c2, err := patch(cfg); err != nil {
			log.Printf("dashboard %s: %s: %v", urlPath, name, err)
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
	log.Printf("dashboard %s: battery card extended with %d BMS rows", urlPath, len(dashBMSRows))
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
			if m, ok := e.(map[string]any); ok {
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
		if id := entityID(e); id != "" && seen[id] {
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
	if md := findMarkdownWith(node, dashDeltaEntity); md != nil {
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
