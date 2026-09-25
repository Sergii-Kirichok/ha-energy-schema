package web

import (
	"fmt"
	"log"
	"strconv"
	"time"
)

// BMS: интеграция Solarman считает sensor.*_battery_soh по накопленному заряду
// с номиналом 48 В — для HV-батареи (630 В) это ~92 % вместо 97 % от BMS.
// Настоящие данные лежат в блоке Deye battery (MODBUS RTU V104): 10006 SOH,
// 10046 циклы, 10052/10055 макс/мин ячейка (мВ) + номера 10053/10056. Читаем
// блок раз в минуту через сервис solarman.read_holding_registers и публикуем
// как сенсоры HA (POST /api/states) — для дашборда Bobrixos-Energy; SOH ещё и
// как виртуальную сущность для схемы.
const (
	bmsDeviceEntity = "sensor.deye_sun_30k_battery" // любая сущность устройства Solarman
	bmsSOHEntity    = "sensor.energy_schema_bms_soh"

	regSOH      = 10006
	regCycles   = 10046
	regCellMax  = 10052
	regCellMaxN = 10053
	regCellMin  = 10055
	regCellMinN = 10056
)

// Пороги для LFP-ячеек. ponytail: эмпирика по типичным HV LFP (3.2 В номинал);
// подстроить, если BMS Deye-HV покажет другую норму на покое.
const (
	cellDeltaOKmV   = 30   // разбег в покое до 30 мВ — выровнены
	cellDeltaWarnmV = 100  // 30..100 — балансировка идёт (в заряде большим током разбег растёт), выше — разбаланс
	cellMaxWarnMV   = 3450 // верхняя ячейка выше — снизить ток заряда
	cellMaxRedMV    = 3550 // близко к отсечке BMS — заряд только каплей
)

// cellVerdict — уровень (ok/warn/bad) и подсказка по разбегу и верхней ячейке.
func cellVerdict(maxMV, minMV int) (level, note string) {
	d := maxMV - minMV
	switch {
	case maxMV >= cellMaxRedMV:
		return "bad", "верхняя ячейка у отсечки — заряд каплей"
	case maxMV >= cellMaxWarnMV:
		return "warn", "снизить ток заряда"
	case d > cellDeltaWarnmV:
		return "bad", "разбаланс — заряжать малым током"
	case d > cellDeltaOKmV:
		return "warn", "балансируется — не добавлять ток"
	default:
		return "ok", "выровнены — можно добавлять ток"
	}
}

func (s *Server) loopBMS() {
	lastLine := ""
	for {
		regs, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, regSOH, regCellMinN-regSOH+1)
		if err != nil {
			log.Println("bms:", err)
		} else if line, err := s.publishBMS(regs); err != nil {
			log.Println("bms:", err)
		} else if line != lastLine {
			log.Println("bms:", line)
			lastLine = line
		}
		time.Sleep(time.Minute)
	}
}

// publishBMS validates the block and pushes it to HA; returns a one-line summary.
func (s *Server) publishBMS(r map[int]int) (string, error) {
	soh, cyc := r[regSOH], r[regCycles]
	maxMV, minMV := r[regCellMax], r[regCellMin]
	if soh < 1 || soh > 100 || maxMV < 2000 || maxMV > 4500 || minMV < 2000 || minMV > maxMV {
		return "", fmt.Errorf("block out of range: soh=%d max=%d min=%d", soh, maxMV, minMV)
	}
	s.store.SetVirtual(bmsSOHEntity, strconv.Itoa(soh))
	level, note := cellVerdict(maxMV, minMV)
	mv := func(name, icon string) map[string]any {
		return map[string]any{"friendly_name": name, "unit_of_measurement": "V", "device_class": "voltage",
			"state_class": "measurement", "icon": icon}
	}
	pubs := []struct {
		id, state string
		attrs     map[string]any
	}{
		{bmsSOHEntity, strconv.Itoa(soh), map[string]any{"friendly_name": "Здоровье (SOH, BMS)",
			"unit_of_measurement": "%", "state_class": "measurement", "icon": "mdi:battery-heart", "source": "register 10006"}},
		{"sensor.energy_schema_bms_cell_max", fmt.Sprintf("%.3f", float64(maxMV)/1000),
			withCell(mv("Ячейка макс", "mdi:battery-arrow-up"), r[regCellMaxN])},
		{"sensor.energy_schema_bms_cell_min", fmt.Sprintf("%.3f", float64(minMV)/1000),
			withCell(mv("Ячейка мин", "mdi:battery-arrow-down"), r[regCellMinN])},
		{"sensor.energy_schema_bms_cell_delta", strconv.Itoa(maxMV - minMV), map[string]any{
			"friendly_name": "Разбег ячеек", "unit_of_measurement": "mV", "state_class": "measurement", "icon": "mdi:delta",
			"cell_min": fmt.Sprintf("%.3f В", float64(minMV)/1000), "cell_max": fmt.Sprintf("%.3f В", float64(maxMV)/1000), "verdict": note}},
		{"sensor.energy_schema_bms_balance", note, map[string]any{"friendly_name": "Баланс ячеек", "level": level,
			"delta_mv": maxMV - minMV, "icon": map[string]string{"ok": "mdi:check-circle", "warn": "mdi:alert", "bad": "mdi:alert-octagon"}[level]}},
		{"sensor.energy_schema_bms_cycles", strconv.Itoa(cyc), map[string]any{"friendly_name": "Циклы (BMS)",
			"state_class": "total_increasing", "icon": "mdi:battery-sync", "source": "register 10046"}},
	}
	for _, p := range pubs {
		if err := s.client.SetState(p.id, p.state, p.attrs); err != nil {
			return "", fmt.Errorf("publish %s: %w", p.id, err)
		}
	}
	return fmt.Sprintf("soh %d%% cycles %d cell max %d mV (#%d) min %d mV (#%d) Δ%d — %s",
		soh, cyc, maxMV, r[regCellMaxN], minMV, r[regCellMinN], maxMV-minMV, note), nil
}

func withCell(a map[string]any, n int) map[string]any { a["cell"] = n; return a }
