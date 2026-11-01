package web

import "maps"

// Карточка «Диагностика АКБ» — контролы диагностического прогона тока
// (charge_probe.go): кнопка, статус и профиль-ползунки. Добавляется один раз
// рядом с карточкой «Батарея»; маркер — кнопка прогона.
var dashProbeCard = map[string]any{
	"type":               "entities",
	"show_header_toggle": false,
	"title":              "Диагностика АКБ",
	"entities": []any{
		map[string]any{"entity": chargeProbeEntity, "name": "Прогон тока"},
		map[string]any{"entity": chargeProbeStatus, "name": "Статус"},
		map[string]any{"entity": probeHighAEntity, "name": "Ток шага, А"},
		map[string]any{"entity": probeHighMinEntity, "name": "Минут шага"},
		map[string]any{"entity": probeLowAEntity, "name": "Ток паузы, А"},
		map[string]any{"entity": probeLowMinEntity, "name": "Минут паузы"},
		map[string]any{"entity": probeCyclesEntity, "name": "Циклов"},
	},
}

// patchProbeCard добавляет карточку диагностики один раз (маркер — кнопка
// прогона). patchChargeCard в списке патчей идёт раньше и успевает вынести
// строки прогона из карточки «Заряд АКБ», так что present их тут уже не видит.
func patchProbeCard(node any) (bool, error) {
	return appendCardOnce(node, findCardWith(node, chargeProbeEntity) != nil, maps.Clone(dashProbeCard))
}
