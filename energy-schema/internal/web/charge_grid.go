package web

import "log"

// Deye с разрешённым зарядом от сети, пока SOC ниже слота Time of Use, гонит
// ВСЁ солнце в батарею, а дом питает из сети. Регистр 128 («лимит от сети»)
// ограничивает только прямой ток сеть→АКБ, поэтому фактически батарея
// заряжается сетью сверх лимита (02.10 утром: солнце 2,4 кВт в АКБ, дом 2,8 кВт
// из сети). Противодействие: днём выключаем «Заряд от сети», ночью включаем
// обратно — ночные слоты ToU продолжают работать как настроено в инверторе.
const (
	gridChargeSwitch = "switch.deye_sun_30k_battery_grid_charging"
	noGridDayHelper  = "input_boolean.energy_schema_charge_no_grid_day"
	shutdownSOCNum   = "number.deye_sun_30k_battery_shutdown_soc"
	gridSafetyMargin = 5.0 // % над порогом отключения: ниже — заряд от сети разрешён и днём
)

// gridChargeWanted — должен ли быть разрешён заряд от сети сейчас.
func gridChargeWanted(night, blockDay bool, soc, shutdownSOC float64) bool {
	if !blockDay || night {
		return true
	}
	return soc < shutdownSOC+gridSafetyMargin
}

// syncGridCharge приводит переключатель «Заряд от сети» к нужному положению;
// пишет только при расхождении (состояние берётся из опроса HA).
func (s *Server) syncGridCharge(night bool, soc float64) {
	if s.ovr.get("grid_charge") {
		return // переведено вручную на странице инвертора
	}
	cur := s.store.State(gridChargeSwitch)
	if cur != "on" && cur != "off" {
		return // сущности нет / недоступна — не трогаем
	}
	want := gridChargeWanted(night, s.store.On(noGridDayHelper), soc, s.store.Num(shutdownSOCNum))
	if (cur == "on") == want {
		return
	}
	svc := "turn_off"
	if want {
		svc = "turn_on"
	}
	if err := s.client.CallService("switch", svc, map[string]any{"entity_id": gridChargeSwitch}); err != nil {
		log.Println("charge: grid charging switch:", err)
		return
	}
	log.Printf("charge: grid charging %s (night=%v, soc %.0f%%)", map[bool]string{true: "ON", false: "OFF"}[want], night, soc)
}
