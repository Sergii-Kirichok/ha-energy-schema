package hass

import (
	"log"
	"strings"
)

// spikeGuard не пускает в пики (за день и за 24 ч) одиночный выброс мощности:
// 18.10 на шкале «Дом» стоял пик ~25 кВт, которого нет в истории HA (нагрузка
// за трое суток ≤ 9 кВт). Скачок *_power больше чем на spikeJumpW и втрое выше
// прошлого замера ждёт подтверждения следующим опросом (не ниже половины
// выброса); не подтвердился — отбрасывается с записью в лог.
const spikeJumpW = 10000

type spikeGuard struct {
	prev map[string]float64 // прошлый замер
	pend map[string]float64 // выброс, ждущий подтверждения
}

// peaks возвращает значения, которые можно учесть в пиках (0, 1 или 2 шт.).
func (g *spikeGuard) peaks(id string, v float64) []float64 {
	if g.prev == nil {
		g.prev, g.pend = map[string]float64{}, map[string]float64{}
	}
	prev, seen := g.prev[id]
	g.prev[id] = v
	var out []float64
	if p, ok := g.pend[id]; ok {
		delete(g.pend, id)
		if v >= p/2 {
			out = append(out, p)
		} else {
			log.Printf("store: %s spike %.0f ignored (prev %.0f, next %.0f)", id, p, prev, v)
		}
	}
	if seen && strings.HasSuffix(id, "_power") && v-prev > spikeJumpW && v > 3*prev {
		g.pend[id] = v
		return out
	}
	return append(out, v)
}
