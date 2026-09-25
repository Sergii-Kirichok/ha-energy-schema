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
	"type":  "entities",
	"title": "Заряд АКБ",
	"entities": []any{
		map[string]any{"entity": dashChargeMarker, "name": "Авто-регулятор"},
		map[string]any{"entity": "sensor.energy_schema_charge_setpoint", "name": "Уставка тока"},
		map[string]any{"entity": "input_number.energy_schema_charge_max_a", "name": "Общий лимит, А"},
		map[string]any{"entity": "input_number.energy_schema_charge_grid_a", "name": "Лимит от сети, А"},
		map[string]any{"entity": "input_number.energy_schema_charge_taper_soc", "name": "Снижать ток с, %"},
		map[string]any{"entity": "input_number.energy_schema_charge_target_soc", "name": "Цель на ночь, %"},
		map[string]any{"entity": "input_number.energy_schema_charge_full_days", "name": "Полный заряд раз в, дней"},
		map[string]any{"entity": "sensor.energy_schema_charge_next_full", "name": "Следующий 100 %"},
	},
}

// patchChargeCard appends dashChargeCard next to the battery card unless a card
// with the marker entity already exists anywhere in the dashboard.
func patchChargeCard(node any) (bool, error) {
	return appendCardOnce(node, findCardWith(node, dashChargeMarker) != nil, dashChargeCard)
}

// Разбег ячеек цветом: entities-строку раскрасить нативно нельзя (card-mod не
// стоит), поэтому рядом — gauge с зонами зелёный/жёлтый/красный (0/30/100 мВ).
var dashDeltaGauge = map[string]any{
	"type": "gauge", "entity": "sensor.energy_schema_bms_cell_delta", "name": "Разбег ячеек",
	"unit": "mV", "min": 0, "max": 150, "needle": true,
	"severity": map[string]any{"green": 0, "yellow": cellDeltaOKmV, "red": cellDeltaWarnmV},
}

func patchDeltaGauge(node any) (bool, error) {
	// маркер — именно gauge на этой сущности (строка в списке не считается)
	return appendCardOnce(node, findOwnEntityCard(node, "gauge", "sensor.energy_schema_bms_cell_delta") != nil, dashDeltaGauge)
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

// findCardsHolding returns [parentMap, key] of the []any that contains target.
func findCardsHolding(node any, target map[string]any) []any {
	m, ok := node.(map[string]any)
	if !ok {
		if l, ok := node.([]any); ok {
			for _, c := range l {
				if r := findCardsHolding(c, target); r != nil {
					return r
				}
			}
		}
		return nil
	}
	for k, v := range m {
		if l, ok := v.([]any); ok {
			for _, c := range l {
				if cm, ok := c.(map[string]any); ok && sameMap(cm, target) {
					return []any{m, k}
				}
			}
		}
		if r := findCardsHolding(v, target); r != nil {
			return r
		}
	}
	return nil
}

func sameMap(a, b map[string]any) bool { return fmt.Sprintf("%p", a) == fmt.Sprintf("%p", b) }

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
	for name, patch := range map[string]func(any) (bool, error){"charge card": patchChargeCard, "delta gauge": patchDeltaGauge} {
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
	if len(add) == 0 {
		if changed {
			card["entities"] = ents
		}
		return changed, nil
	}
	out := append([]any{}, ents[:anchor+1]...)
	out = append(out, add...)
	out = append(out, ents[anchor+1:]...)
	card["entities"] = out
	return true, nil
}

func entityID(e any) string {
	switch v := e.(type) {
	case string:
		return v
	case map[string]any:
		id, _ := v["entity"].(string)
		return id
	}
	return ""
}

func findEntitiesCard(node any) map[string]any {
	if c := findCardWith(node, dashAnchorEntity); c != nil {
		return c
	}
	return findCardWith(node, bmsSOHEntity)
}

// findCardWith returns the first entities card whose rows include entity.
func findCardWith(node any, entity string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == "entities" {
			if ents, ok := v["entities"].([]any); ok {
				for _, e := range ents {
					if entityID(e) == entity {
						return v
					}
				}
			}
		}
		for _, child := range v {
			if c := findCardWith(child, entity); c != nil {
				return c
			}
		}
	case []any:
		for _, child := range v {
			if c := findCardWith(child, entity); c != nil {
				return c
			}
		}
	}
	return nil
}

// findOwnEntityCard returns the first card of the given type whose own
// `entity` is entity (gauge, tile, ...).
func findOwnEntityCard(node any, typ, entity string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == typ && v["entity"] == entity {
			return v
		}
		for _, child := range v {
			if c := findOwnEntityCard(child, typ, entity); c != nil {
				return c
			}
		}
	case []any:
		for _, child := range v {
			if c := findOwnEntityCard(child, typ, entity); c != nil {
				return c
			}
		}
	}
	return nil
}
