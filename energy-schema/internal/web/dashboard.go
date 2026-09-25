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

// ensureDashboard adds the BMS rows to the battery card if they are missing.
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
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == "entities" {
			if ents, ok := v["entities"].([]any); ok {
				for _, e := range ents {
					if id := entityID(e); id == dashAnchorEntity || id == bmsSOHEntity {
						return v
					}
				}
			}
		}
		for _, child := range v {
			if c := findEntitiesCard(child); c != nil {
				return c
			}
		}
	case []any:
		for _, child := range v {
			if c := findEntitiesCard(child); c != nil {
				return c
			}
		}
	}
	return nil
}
