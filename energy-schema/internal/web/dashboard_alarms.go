package web

// Ошибки инвертора на дашборде (alarms.go): на главной — красная карточка
// только когда ошибки есть (conditional, первой в списке с карточкой «Батарея»),
// и отдельная вкладка «Ошибки» — текущие + журнал за неделю. Добавляются один
// раз; маркеры — текст карточки и path вкладки.
const (
	alarmMainMarker = "## ⚠ Ошибки инвертора"
	alarmViewPath   = "errors"
	alarmTextTpl    = "{{ state_attr('" + alarmEntity + "', 'text') }}"
)

func dashAlarmMain() map[string]any {
	return map[string]any{
		"type":       "conditional",
		"conditions": []any{map[string]any{"condition": "state", "entity": alarmEntity, "state_not": "OK"}},
		"card": map[string]any{"type": "markdown", "content": alarmMainMarker + "\n" + alarmTextTpl +
			"\n\n*Подробно — вкладка «Ошибки» или тап по ⚠ на схеме.*"},
	}
}

func dashAlarmView() map[string]any {
	src := []any{alarmEntity, "sensor.deye_sun_30k_device_state", "sensor.deye_sun_30k_device_alarm", "sensor.deye_sun_30k_device_fault",
		"binary_sensor.deye_sun_30k_battery_alarm", "binary_sensor.deye_sun_30k_battery_fault"}
	return map[string]any{"title": "Ошибки", "path": alarmViewPath, "icon": "mdi:alert-octagon", "cards": []any{
		map[string]any{"type": "markdown", "content": "### Текущие ошибки\n{% if is_state('" + alarmEntity + "', 'OK') %}✅ Ошибок нет{% else %}" + alarmTextTpl + "{% endif %}"},
		map[string]any{"type": "entities", "title": "Инвертор сейчас", "entities": src},
		map[string]any{"type": "logbook", "title": "История ошибок", "entities": src, "hours_to_show": 168},
	}}
}

func patchAlarms(node any) (bool, error) {
	changed := false
	if findMarkdownWith(node, alarmMainMarker) == nil {
		if cards := findCardsHolding(node, findEntitiesCard(node)); cards != nil {
			holder, key := cards[0].(map[string]any), cards[1].(string)
			holder[key] = append([]any{dashAlarmMain()}, holder[key].([]any)...)
			changed = true
		}
	}
	root, _ := node.(map[string]any)
	views, _ := root["views"].([]any)
	for _, v := range views {
		if vm, _ := v.(map[string]any); vm["path"] == alarmViewPath {
			return changed, nil
		}
	}
	if views != nil {
		root["views"] = append(views, dashAlarmView())
		changed = true
	}
	return changed, nil
}
