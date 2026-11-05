package web

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// Ошибки инвертора. Solarman (deye_p3) отдаёт «Device Alarm» (рег. 553–554) и
// «Device Fault» (555–558) одной строкой и знает лишь часть битов — остальное
// у него «Problem». Поэтому, как только HA показывает не «OK», читаем эти шесть
// регистров сами и раскладываем по кодам: бит n слов неисправностей — F(n+1)
// (сверено с профилем: бит 12 — F13 «смена режима», бит 63 — F64 «перегрев»),
// предупреждения — W(n+1) по той же схеме. Итог — sensor.energy_schema_inverter_alarms
// (состояние — коды или OK, история — в журнале HA), схема и страница инвертора.
const (
	alarmEntity   = "sensor.energy_schema_inverter_alarms"
	alarmRegFirst = 553 // 553–554 предупреждения, 555–558 неисправности
	alarmEvery    = 10 * time.Second
	alarmReadGap  = 30 * time.Second // Solarman обновляет эти регистры раз в 30 с
	alarmRefresh  = 10 * time.Minute // переотправка того же состояния (HA перезапускался)
)

var warnText = map[int]string{
	2: "отказ вентилятора", 3: "ошибка фаз сети (чередование / обрыв фазы)", 4: "нет связи со счётчиком",
	31: "батарея потеряна", 32: "сбой связи параллельной системы",
}

var faultText = map[int]string{
	1: "обратная полярность PV", 7: "сбой плавного пуска DC/DC", 10: "сбой вспомогательного питания",
	13: "смена режима работы", 15: "перегрузка по току AC (программная)", 16: "утечка на землю (GFCI)",
	18: "перегрузка по току AC (аппаратная)", 19: "общий аппаратный сбой", 20: "перегрузка по току DC",
	22: "аварийная остановка", 23: "утечка тока AC", 24: "низкое сопротивление изоляции PV",
	26: "разбаланс шины DC", 29: "ошибка CAN параллельной системы", 34: "перегрузка выхода AC",
	35: "нет сети AC", 41: "остановка параллельной системы", 42: "напряжение сети вне диапазона",
	46: "неисправность батареи (резерв)", 47: "частота сети выше нормы", 48: "частота сети ниже нормы",
	55: "высокое напряжение шины DC", 56: "низкое напряжение шины DC / батареи", 58: "нет связи с BMS",
	62: "остановка по DRM", 63: "дуговой пробой (ARC)", 64: "перегрев радиатора",
}

// decodeAlarms — коды из регистров 553–558: «F42 — напряжение сети вне диапазона».
func decodeAlarms(r map[int]int) []string {
	var out []string
	add := func(prefix string, first, bits int, text map[int]string) {
		for i := 0; i < bits; i++ {
			if r[first+i/16]>>(i%16)&1 == 0 {
				continue
			}
			t, ok := text[i+1]
			if !ok {
				t = "код Deye — см. инструкцию инвертора"
			}
			out = append(out, fmt.Sprintf("%s%02d — %s", prefix, i+1, t))
		}
	}
	add("F", alarmRegFirst+2, 64, faultText) // неисправности — первыми, они серьёзнее
	add("W", alarmRegFirst, 32, warnText)
	return out
}

// haAlarms — что видит HA; пусто — всё в норме.
func (s *Server) haAlarms() []string {
	var out []string
	st := s.store.State("sensor.deye_sun_30k_device_state")
	if st == "Alarm" || st == "Fault" {
		out = append(out, "состояние инвертора: "+st)
	}
	for e, name := range map[string]string{"sensor.deye_sun_30k_device_fault": "неисправность", "sensor.deye_sun_30k_device_alarm": "предупреждение"} {
		if v := s.store.State(e); v != "" && v != "OK" && s.store.Available(e) {
			out = append(out, name+": "+v)
		}
	}
	for e, name := range map[string]string{"binary_sensor.deye_sun_30k_battery_fault": "АКБ: авария BMS", "binary_sensor.deye_sun_30k_battery_alarm": "АКБ: тревога BMS"} {
		if s.store.On(e) {
			out = append(out, name)
		}
	}
	return out
}

// mergeAlarms — коды из регистров плюс то, чего в 553–558 нет (АКБ, состояние);
// регистры не прочитались или пусты — всё, что видит HA.
func mergeAlarms(regs, ha []string) []string {
	if len(regs) == 0 {
		return ha
	}
	for _, h := range ha {
		if strings.HasPrefix(h, "АКБ") || strings.HasPrefix(h, "состояние") {
			regs = append(regs, h)
		}
	}
	return regs
}

func (s *Server) currentAlarms() []string {
	if p := s.alarms.Load(); p != nil {
		return *p
	}
	return nil
}

// loopAlarms — следит за ошибками и публикует сенсор (state — коды через пробел).
func (s *Server) loopAlarms() {
	var items []string
	var readAt, sentAt, since time.Time
	sent := "-"
	for ; ; time.Sleep(alarmEvery) {
		ha := s.haAlarms()
		switch {
		case len(ha) == 0:
			items, readAt = nil, time.Time{}
		case time.Since(readAt) >= alarmReadGap:
			readAt = time.Now()
			s.mb.Lock()
			r, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, alarmRegFirst, 6)
			s.mb.Unlock()
			if err != nil {
				log.Println("alarms: read 553..558:", err)
			}
			items = mergeAlarms(decodeAlarms(r), ha)
		}
		state := "OK"
		if len(items) > 0 {
			var codes []string
			for _, it := range items {
				codes = append(codes, strings.SplitN(it, " — ", 2)[0])
			}
			state = strings.Join(codes, " ")
			if len(state) > 200 {
				state = state[:200]
			}
			if since.IsZero() {
				since = time.Now()
			}
		} else {
			since = time.Time{}
		}
		cp := append([]string(nil), items...)
		s.alarms.Store(&cp)
		if state == sent && time.Since(sentAt) < alarmRefresh {
			continue
		}
		if state != sent {
			log.Printf("alarms: %s %v", state, items)
		}
		icon := map[bool]string{true: "mdi:alert-octagon", false: "mdi:check-circle-outline"}[len(items) > 0]
		if err := s.client.SetState(alarmEntity, state, map[string]any{"friendly_name": "Инвертор: ошибки", "icon": icon,
			"items": items, "text": strings.Join(items, "\n"), "since": isoOrEmpty(since)}); err == nil {
			sent, sentAt = state, time.Now()
		}
	}
}
