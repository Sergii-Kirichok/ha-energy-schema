package web

import (
	"fmt"
	"maps"
	"slices"
)

// Карточка «Заряд АКБ» — хелперы регулятора + его сенсоры; добавляется в ту же
// секцию, где карточка «Батарея», если её ещё нет (маркер — переключатель авто).
const dashChargeMarker = chargeAutoHelper

const (
	dashSetpointEntity  = "sensor.energy_schema_charge_setpoint"
	dashSetpointName    = "Текущее ограничение тока заряда"
	dashSetpointOldName = "Уставка тока"
)

// autoRowIf — строка авто-регулятора, видимая при balance == state.
func autoRowIf(state string, row map[string]any) map[string]any {
	return map[string]any{"type": "conditional",
		"conditions": []any{map[string]any{"entity": chargeBalanceEntity, "state": state}}, "row": row}
}

// Порядок: переключатели, показания, ползунки, последние две — «Применить» и
// «Изменения». Пока идёт балансировка, авто-регулятор — только надпись.
var dashChargeCard = map[string]any{
	"type":               "entities",
	"show_header_toggle": false,
	"title":              "Заряд АКБ",
	"entities": []any{
		map[string]any{"entity": chargeBalanceEntity, "name": "Балансировка по верху"},
		autoRowIf("off", map[string]any{"entity": dashChargeMarker, "name": "Авто-регулятор"}),
		autoRowIf("on", map[string]any{"type": "simple-entity", "entity": dashChargeMarker, "name": "Авто-регулятор (ведёт балансировка)"}),
		map[string]any{"entity": chargeFullNowEntity, "name": "Полный заряд сейчас"},
		map[string]any{"entity": gridChargeSwitch, "name": "Заряд от сети (вручную)"},
		map[string]any{"entity": chargeBalanceStatus, "name": "Балансировка"},
		map[string]any{"entity": dashSetpointEntity, "name": dashSetpointName},
		map[string]any{"entity": chargeFullPlan, "name": "Полный заряд: прогноз"},
		map[string]any{"entity": "sensor.energy_schema_charge_next_full", "name": "Следующий 100 %"},
		map[string]any{"entity": "input_number.energy_schema_charge_max_a", "name": "Общий лимит, А"},
		map[string]any{"entity": "input_number.energy_schema_charge_grid_a", "name": "Лимит от сети, А"},
		map[string]any{"entity": "input_number.energy_schema_charge_target_soc", "name": "Цель на ночь, %"},
		map[string]any{"entity": "input_number.energy_schema_charge_full_days", "name": "Полный заряд раз в, дней"},
		map[string]any{"entity": chargeApplyBt, "name": "Применить изменения"},
		map[string]any{"entity": chargePending, "name": "Изменения"},
	},
}

// rowKey — сущность строки; у условной строки — ещё и её условия (два
// варианта авто-регулятора — разные строки шаблона).
func rowKey(e any) string {
	k := entityID(e)
	if m, ok := e.(map[string]any); ok && m["type"] == "conditional" {
		cs, _ := m["conditions"].([]any)
		for _, c := range cs {
			if cm, ok := c.(map[string]any); ok {
				k += fmt.Sprintf("@%v=%v", cm["entity"], cm["state"])
			}
		}
	}
	return k
}

// patchChargeCard appends dashChargeCard next to the battery card unless a card
// with the marker entity already exists; an existing card is migrated and
// reordered to the template (rows kept as objects, user rows before the last two).
func patchChargeCard(node any) (bool, error) {
	card := findCardWith(node, dashChargeMarker)
	if card == nil {
		return appendCardOnce(node, false, maps.Clone(dashChargeCard)) // копия: дальше патчи пишут в карточку
	}
	changed := false
	// общий переключатель в заголовке включал бы ВСЕ тумблеры сразу — убираем
	if card["show_header_toggle"] != false {
		card["show_header_toggle"], changed = false, true
	}
	ents, _ := card["entities"].([]any)
	var before []string
	kept := ents[:0:0]
	for _, e := range ents {
		before = append(before, rowKey(e))
		m, _ := e.(map[string]any)
		switch {
		case entityID(e) == "input_number.energy_schema_charge_taper_soc":
			continue // «Снижать ток с» убран (0.6.12)
		case m != nil && m["entity"] == "input_boolean.energy_schema_charge_no_grid_day":
			m["entity"], m["name"] = gridChargeSwitch, "Заряд от сети (вручную)" // хелпер убран (0.6.11)
		case m != nil && m["entity"] == dashSetpointEntity && m["name"] == dashSetpointOldName:
			m["name"], changed = dashSetpointName, true // своё имя пользователя не трогаем
		case m != nil && m["entity"] == dashChargeMarker && m["type"] != "conditional":
			e = autoRowIf("off", m) // простая строка авто → вариант «без балансировки», имя своё
		case m == nil && entityID(e) == dashChargeMarker:
			e = autoRowIf("off", map[string]any{"entity": dashChargeMarker, "name": "Авто-регулятор"})
		}
		kept = append(kept, e)
	}
	tmpl := dashChargeCard["entities"].([]any)
	idx := map[string]int{}
	for i, r := range tmpl {
		idx[rowKey(r)] = i
	}
	slot, extra := make([]any, len(tmpl)), []any{}
	for _, e := range kept {
		switch i, ok := idx[rowKey(e)]; {
		case !ok:
			extra = append(extra, e) // строка пользователя
		case slot[i] == nil:
			slot[i] = e
		} // повтор строки шаблона — убираем
	}
	for i := range slot {
		if slot[i] == nil {
			slot[i] = tmpl[i] // появилась в новой версии
		}
	}
	n := len(slot) - 2
	out := append(append(append([]any{}, slot[:n]...), extra...), slot[n:]...)
	var after []string
	for _, e := range out {
		after = append(after, rowKey(e))
	}
	if !changed && slices.Equal(before, after) {
		return false, nil
	}
	card["entities"] = out
	return true, nil
}
