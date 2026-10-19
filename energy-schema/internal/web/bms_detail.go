package web

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

// Детализация BMS стойки (MODBUS V104, блок «second level BMS», сверено с
// живыми значениями 03.10). Инвертор отдаёт только сводку стойки, но для
// крайних ячеек — номер ячейки И номер модуля («local»): по ним копим
// статистику и ищем слабую ячейку.
const (
	// 10053/10056 — модуль (1..12), 10054/10057 — ячейка в модуле (1..16):
	// по истории 16.10 первое бывает только до 12, второе — до 16
	regCellMaxMod = 10053 // модуль с самой высокой ячейкой
	regCellMinMod = 10056 // модуль с самой низкой ячейкой
	regCellMaxN   = 10054 // …ячейка в модуле
	regCellMinN   = 10057
	regTempMax    = 10059 // °C + 40
	regTempMaxMod = 10060
	regTempMaxN   = 10061
	regTempMin    = 10062
	regTempMinMod = 10063
	regTempMinN   = 10064
	regChgLimit   = 10065 // 0.1 A
	regDsgLimit   = 10066 // 0.1 A
	regPacks      = 10070
	regOverChg    = 10071 // перезарядов за жизнь
	regOverDsg    = 10072
	regInsulation = 10095 // кОм
	bmsDetailFrom = 10057
	bmsDetailTo   = 10095
	cellStatsFile = "/data/cellstats.json"
	statLoadA     = 5.0                                  // учитываем только под нагрузкой: |I| > 5 А
	bmsCells      = "sensor.energy_schema_bms_cells"     // «3.349 / 3.376 В»
	bmsCellsLoc   = "sensor.energy_schema_bms_cells_loc" // «М6·№5 / М9·№9»
	bmsTemps      = "sensor.energy_schema_bms_temps"     // «17 / 19 °C»
)

func tempC(raw int) float64 { return float64(raw - 40) }

func cellLoc(mod, cell int) string { return fmt.Sprintf("М%d·№%d", mod, cell) }

// cellStats — сколько раз ячейка была самой низкой при разряде и самой
// высокой при заряде. Слабая ячейка проседает первой под нагрузкой и первой
// упирается вверх при заряде — она и будет лидером счётчиков.
type cellStats struct {
	mu     sync.Mutex
	path   string
	Since  time.Time      `json:"since"`
	Dsg    int            `json:"dsg_samples"`
	Chg    int            `json:"chg_samples"`
	MinDsg map[string]int `json:"min_on_discharge"`
	MaxChg map[string]int `json:"max_on_charge"`
	Rack   string         `json:"rack"`   // серийник BMU стойки (10032..10039)
	Cycles int            `json:"cycles"` // счётчик циклов BMS при последней выборке
	LocV2  bool           `json:"loc_v2"` // подписи «М·№» исправлены (16.10): модуль и ячейка были перепутаны
	saved  time.Time
}

func loadCellStats(path string) *cellStats {
	c := &cellStats{path: path, Since: time.Now(), MinDsg: map[string]int{}, MaxChg: map[string]int{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, c)
	}
	if c.MinDsg == nil {
		c.MinDsg = map[string]int{}
	}
	if c.MaxChg == nil {
		c.MaxChg = map[string]int{}
	}
	if !c.LocV2 { // старые ключи «М<ячейка>·№<модуль>» → «М<модуль>·№<ячейка>»
		c.MinDsg, c.MaxChg, c.LocV2 = swapLocs(c.MinDsg), swapLocs(c.MaxChg), true
	}
	return c
}

// add учитывает одну выборку; battA — ток АКБ (HA: «−» заряд, «+» разряд).
func (c *cellStats) add(battA float64, minLoc, maxLoc string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case battA > statLoadA:
		c.Dsg++
		c.MinDsg[minLoc]++
	case battA < -statLoadA:
		c.Chg++
		c.MaxChg[maxLoc]++
	default:
		return
	}
	if time.Since(c.saved) > 10*time.Minute {
		b, _ := json.Marshal(c)
		_ = os.WriteFile(c.path, b, 0o644)
		c.saved = time.Now()
	}
}

type cellRank struct {
	Loc   string  `json:"loc"`
	Count int     `json:"count"`
	Share float64 `json:"share"` // %
}

// top — ячейки по убыванию частоты (до n штук).
func top(m map[string]int, total, n int) []cellRank {
	var r []cellRank
	for k, v := range m {
		r = append(r, cellRank{Loc: k, Count: v, Share: float64(v) * 100 / float64(max(1, total))})
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Count > r[j].Count || r[i].Count == r[j].Count && r[i].Loc < r[j].Loc })
	if len(r) > n {
		r = r[:n]
	}
	return r
}

func (c *cellStats) summary() (weak string, attrs map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	md, mc := top(c.MinDsg, c.Dsg, 5), top(c.MaxChg, c.Chg, 5)
	weak = "мало данных"
	if len(md) > 0 && c.Dsg >= 30 {
		weak = fmt.Sprintf("%s — %.0f%% разрядов", md[0].Loc, md[0].Share)
	}
	return weak, map[string]any{"friendly_name": "Слабая ячейка (статистика)", "icon": "mdi:battery-alert-variant-outline",
		"min_on_discharge": md, "max_on_charge": mc, "discharge_samples": c.Dsg, "charge_samples": c.Chg,
		"since": c.Since.Format("2006-01-02")}
}

// publishDetail — сенсоры детализации стойки + накопление статистики.
func (s *Server) publishDetail(r map[int]int) error {
	minLoc := cellLoc(r[regCellMinMod], r[regCellMinN])
	maxLoc := cellLoc(r[regCellMaxMod], r[regCellMaxN])
	if !s.cells.sameRack(rackSerial(r), r[regCycles]) {
		log.Printf("bms: new battery rack (BMU %s, cycles %d) — cell statistics reset", rackSerial(r), r[regCycles])
	}
	s.cells.add(s.store.Num("sensor.deye_sun_30k_battery_current"), minLoc, maxLoc)
	weak, wattrs := s.cells.summary()
	t := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "unit_of_measurement": "°C", "device_class": "temperature", "state_class": "measurement"}
	}
	pubs := []struct {
		id, state string
		attrs     map[string]any
	}{
		{"sensor.energy_schema_bms_cell_max_loc", maxLoc, map[string]any{"friendly_name": "Ячейка макс — где", "icon": "mdi:map-marker-up"}},
		{"sensor.energy_schema_bms_cell_min_loc", minLoc, map[string]any{"friendly_name": "Ячейка мин — где", "icon": "mdi:map-marker-down"}},
		{"sensor.energy_schema_bms_temp_max", fmt.Sprintf("%.0f", tempC(r[regTempMax])),
			withLoc(t("Темп. ячеек макс"), r[regTempMaxMod], r[regTempMaxN])},
		{"sensor.energy_schema_bms_temp_min", fmt.Sprintf("%.0f", tempC(r[regTempMin])),
			withLoc(t("Темп. ячеек мин"), r[regTempMinMod], r[regTempMinN])},
		{"sensor.energy_schema_bms_overcharge", fmt.Sprint(r[regOverChg]), map[string]any{"friendly_name": "Перезарядов (BMS)",
			"icon": "mdi:battery-alert", "state_class": "total_increasing", "over_discharge": r[regOverDsg]}},
		{"sensor.energy_schema_bms_insulation", fmt.Sprint(r[regInsulation]), map[string]any{"friendly_name": "Изоляция",
			"unit_of_measurement": "kΩ", "icon": "mdi:shield-check", "state_class": "measurement"}},
		{"sensor.energy_schema_bms_limits", fmt.Sprintf("%.0f / %.0f А", float64(r[regChgLimit])/10, float64(r[regDsgLimit])/10),
			map[string]any{"friendly_name": "Лимиты BMS заряд / разряд", "icon": "mdi:current-dc", "packs": r[regPacks],
				"charge_a": float64(r[regChgLimit]) / 10, "discharge_a": float64(r[regDsgLimit]) / 10}},
		{"sensor.energy_schema_bms_weak_cell", weak, wattrs},
		// мин / макс одной строкой (дашборд): напряжения, где они, температуры
		{bmsCells, fmt.Sprintf("%.3f / %.3f В", float64(r[regCellMin])/1000, float64(r[regCellMax])/1000),
			map[string]any{"friendly_name": "Ячейки мин / макс", "icon": "mdi:battery-arrow-up-outline"}},
		{bmsCellsLoc, minLoc + " / " + maxLoc, map[string]any{"friendly_name": "Ячейки мин / макс — где", "icon": "mdi:map-marker"}},
		{bmsTemps, fmt.Sprintf("%.0f / %.0f °C", tempC(r[regTempMin]), tempC(r[regTempMax])),
			map[string]any{"friendly_name": "Темп. ячеек мин / макс", "icon": "mdi:thermometer",
				"where": cellLoc(r[regTempMinMod], r[regTempMinN]) + " / " + cellLoc(r[regTempMaxMod], r[regTempMaxN])}},
	}
	for _, p := range pubs {
		if err := s.client.SetState(p.id, p.state, p.attrs); err != nil {
			return fmt.Errorf("publish %s: %w", p.id, err)
		}
	}
	return nil
}

func withLoc(a map[string]any, mod, n int) map[string]any { a["where"] = cellLoc(mod, n); return a }

// rackSerial — серийник BMU стойки: ASCII в регистрах 10032..10039 (по 2 символа).
func rackSerial(r map[int]int) string {
	b := make([]byte, 0, 16)
	for a := 10032; a <= 10039; a++ {
		for _, c := range []byte{byte(r[a] >> 8), byte(r[a])} {
			if c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' {
				b = append(b, c)
			}
		}
	}
	return string(b)
}

// sameRack обнуляет статистику, если стойка сменилась: другой серийник BMU
// или счётчик циклов заметно упал (заменили модули при том же блоке BMU).
func (c *cellStats) sameRack(serial string, cycles int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	same := serial == "" || c.Rack == serial
	if c.Rack != "" && cycles+5 < c.Cycles {
		same = false
	}
	if !same {
		c.Since, c.Dsg, c.Chg, c.MinDsg, c.MaxChg = time.Now(), 0, 0, map[string]int{}, map[string]int{}
		c.saved = time.Time{} // сохранить при следующей выборке
	}
	if serial != "" {
		c.Rack = serial
	}
	c.Cycles = cycles
	return same
}

func swapLocs(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[swapLoc(k)] += v
	}
	return out
}

// swapLoc — «М<a>·№<b>» → «М<b>·№<a>» (подписи до исправления 16.10).
func swapLoc(k string) string {
	var a, b int
	if n, _ := fmt.Sscanf(k, "М%d·№%d", &a, &b); n == 2 {
		return cellLoc(b, a)
	}
	return k
}
