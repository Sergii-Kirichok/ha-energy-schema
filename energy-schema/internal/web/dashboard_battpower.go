package web

// Графики apexcharts (10 вариантов «Батарея. Потребление и уровень заряда.» в
// блоке периода и «Профиль мощности — сегодня») рисовали мощность АКБ по
// sensor.deye_sun_30k_battery_power — это лишь один из двух каналов. Переводим
// каждую ссылку дашборда на template-сенсор flowBattCorrect (уже в кВт):
// у серии с «return x / 1000;» transform убираем, у серии без transform (ось в
// Вт) добавляем «x * 1000», чтобы шкала не съехала. Прочие поля не трогаем.
const (
	battPowerToKW = "return x / 1000;"
	battPowerToW  = "return x * 1000;"
)

func patchBattPower(node any) (bool, error) { return swapBattPower(node, false), nil }

// swapBattPower — series: n лежит в series карточки custom:apexcharts-card.
func swapBattPower(n any, series bool) bool {
	changed := false
	switch v := n.(type) {
	case map[string]any:
		if v["entity"] == flowBattHalved {
			v["entity"], changed = flowBattCorrect, true
			if series {
				switch v["transform"] {
				case battPowerToKW:
					delete(v, "transform")
				case nil:
					v["transform"] = battPowerToW
				}
			}
		}
		for k, c := range v {
			if swapBattPower(c, k == "series" && v["type"] == "custom:apexcharts-card") {
				changed = true
			}
		}
	case []any:
		for _, c := range v {
			if swapBattPower(c, series) {
				changed = true
			}
		}
	}
	return changed
}
