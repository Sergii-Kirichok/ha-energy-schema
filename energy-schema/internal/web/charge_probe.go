package web

import (
	"fmt"
	"log"
	"math"
	"time"

	"energy-schema/internal/hass"
)

// Диагностический прогон тока («пробник»). По кнопке в HA гоняет заряд
// ступенями: шаг высоким током probe_high_a на probe_high_min минут, затем
// «пауза» низким током probe_low_a на probe_low_min минут — и так probe_cycles
// циклов. Пока прогон идёт, он перебивает авто-уставку и балансировку (но едет
// на цикле авто-регулятора — тот должен быть включён, иначе прогон на паузе).
// Сам ток в батарею берётся из источника КАК ЕСТЬ: пробник только задаёт
// потолок (рег. 108). Чтобы реально текло 10 А — нужен источник (солнце или
// включённый вручную «Заряд от сети»). Писать по-модульно (BMU) — дело внешней
// программы BatteryWise; здесь ничего не логируем, только даём профиль и статус.
// Защита: прогон сам останавливается на SOC 100 % или на верхе ячейки
// (BalanceStopV / BMS снял лимит заряда) — полный/разбалансированный пакет не
// дёргаем. Каждая смена фазы — одна запись рег. 108 (в обход 10-мин гапа):
// probe_cycles×2 записей за прогон в энергонезависимую память — это ручной
// диагностический режим, не постоянный.
const (
	chargeProbeEntity  = "input_boolean.energy_schema_charge_probe"
	chargeProbeStatus  = "sensor.energy_schema_charge_probe_status"
	probeHighAEntity   = "input_number.energy_schema_probe_high_a"
	probeHighMinEntity = "input_number.energy_schema_probe_high_min"
	probeLowAEntity    = "input_number.energy_schema_probe_low_a"
	probeLowMinEntity  = "input_number.energy_schema_probe_low_min"
	probeCyclesEntity  = "input_number.energy_schema_probe_cycles"
	probeHigh          = "high"
	probeLow           = "low"
)

// probeHelpers — кнопка-прогон и ползунки профиля (крутятся без пересборки).
func probeHelpers() []hass.Helper {
	return []hass.Helper{
		{Domain: "input_boolean", ID: chargeProbeEntity[len("input_boolean."):], Name: "Диагностика: прогон тока", Icon: "mdi:pulse"},
		{Domain: "input_number", ID: probeHighAEntity[len("input_number."):], Name: "Диагностика: ток шага", Min: 1, Max: 100, Step: 1, Initial: 10, Unit: "A", Icon: "mdi:current-dc"},
		{Domain: "input_number", ID: probeHighMinEntity[len("input_number."):], Name: "Диагностика: минут шага", Min: 1, Max: 10, Step: 0.5, Initial: 3, Unit: "мин", Icon: "mdi:timer"},
		{Domain: "input_number", ID: probeLowAEntity[len("input_number."):], Name: "Диагностика: ток паузы", Min: 1, Max: 20, Step: 1, Initial: 2, Unit: "A", Icon: "mdi:current-dc"},
		{Domain: "input_number", ID: probeLowMinEntity[len("input_number."):], Name: "Диагностика: минут паузы", Min: 1, Max: 15, Step: 0.5, Initial: 5, Unit: "мин", Icon: "mdi:timer-sand"},
		{Domain: "input_number", ID: probeCyclesEntity[len("input_number."):], Name: "Диагностика: циклов", Min: 1, Max: 40, Step: 1, Initial: 18, Unit: "шт", Icon: "mdi:repeat"},
	}
}

func (s *Server) ensureProbeHelpers() {
	for _, h := range probeHelpers() {
		if created, err := s.client.EnsureHelper(h); err != nil {
			log.Printf("probe: helper %s.%s: %v", h.Domain, h.ID, err)
		} else if created {
			log.Printf("probe: created %s.%s", h.Domain, h.ID)
		}
	}
}

func (st *chargeState) probeActive() bool { return st.ProbePhase != "" }

// probeConf — профиль из ползунков с запасными значениями, пока HA не прочитан;
// ток шага не выше общего лимита maxA.
func (s *Server) probeConf(maxA float64) (hiA, hiMin, loA, loMin float64, cycles int) {
	numOr := func(e string, def float64) float64 {
		if v := s.store.Num(e); v > 0 {
			return v
		}
		return def
	}
	hiA = math.Min(numOr(probeHighAEntity, 10), math.Max(1, maxA))
	hiMin = numOr(probeHighMinEntity, 3)
	loA = numOr(probeLowAEntity, 2)
	loMin = numOr(probeLowMinEntity, 5)
	cycles = int(numOr(probeCyclesEntity, 18))
	return
}

// probeTop — батарею дальше гонять нельзя: SOC 100 % или верх ячейки.
func (s *Server) probeTop(soc float64) bool {
	if soc >= 100 {
		return true
	}
	t := s.cfg.Charge
	if s.store.Available(bmsCellMaxEntity) && s.store.Num(bmsCellMaxEntity) >= t.BalanceStopV {
		return true
	}
	limit, known := s.store.AttrNum(bmsLimitsEntity, "charge_a"), s.store.Attr(bmsLimitsEntity, "charge_a") != ""
	return known && limit <= 0
}

// probeTick — шаг диагностического прогона. ok — ток ведёт прогон (amps, mode);
// иначе обычная регуляция. Требует включённого авто-регулятора. Статус — каждый
// тик. Переключение фаз — по времени из ползунков; верх/100 % останавливают.
func (s *Server) probeTick(st *chargeState, soc, maxA float64, auto bool) (amps float64, mode string, ok bool) {
	on := s.store.On(chargeProbeEntity)
	hiA, hiMin, loA, loMin, cycles := s.probeConf(maxA)
	now := time.Now()
	defer func() {
		_ = s.client.SetState(chargeProbeStatus, probeText(st, on, auto, hiA, loA, cycles), map[string]any{
			"friendly_name": "Диагностика тока", "icon": "mdi:pulse",
			"phase": st.ProbePhase, "cycle": st.ProbeCycle, "cycles": cycles})
	}()
	switch {
	case !on:
		if st.ProbePhase != "" {
			log.Printf("probe: off after %d cycles", st.ProbeCycle)
			st.ProbePhase = ""
			st.save(chargeFile)
		}
		return 0, "", false
	case !auto:
		return 0, "", false // прогон едет на цикле авто-регулятора; выключен — пауза
	case st.ProbePhase == "":
		st.ProbePhase, st.ProbeCycle, st.ProbeStart, st.ProbeAt, st.ProbeNote = probeHigh, 0, now, now, ""
		st.save(chargeFile)
		log.Printf("probe: start — %.0f А %g мин / %.0f А %g мин × %d", hiA, hiMin, loA, loMin, cycles)
	}
	if s.probeTop(soc) {
		st.ProbeNote = fmt.Sprintf("остановлено на верху: SOC %.0f%%, циклов %d", soc, st.ProbeCycle)
		log.Printf("probe: %s", st.ProbeNote)
		s.probeOff(st)
		return 0, "", false
	}
	switch st.ProbePhase {
	case probeHigh:
		if time.Since(st.ProbeAt) >= dmin(hiMin) {
			st.ProbePhase, st.ProbeAt = probeLow, now
			st.save(chargeFile)
		}
	case probeLow:
		if time.Since(st.ProbeAt) >= dmin(loMin) {
			st.ProbeCycle++
			if st.ProbeCycle >= cycles {
				st.ProbeNote = fmt.Sprintf("прогон завершён: %d циклов", st.ProbeCycle)
				log.Printf("probe: %s", st.ProbeNote)
				s.probeOff(st)
				return 0, "", false
			}
			st.ProbePhase, st.ProbeAt = probeHigh, now
			st.save(chargeFile)
		}
	}
	if st.ProbePhase == probeHigh {
		return math.Max(chargeMinA, hiA), "probe/high", true
	}
	return math.Max(chargeMinA, loA), "probe/low", true
}

func dmin(m float64) time.Duration { return time.Duration(m * float64(time.Minute)) }

func (s *Server) probeOff(st *chargeState) {
	st.ProbePhase = ""
	st.save(chargeFile)
	if err := s.client.CallService("input_boolean", "turn_off", map[string]any{"entity_id": chargeProbeEntity}); err != nil {
		log.Println("probe: helper off:", err)
	}
}

// probeText — строка статуса: фаза, ток, цикл, сколько осталось до смены фазы.
func probeText(st *chargeState, on, auto bool, hiA, loA float64, cycles int) string {
	switch {
	case !on && st.ProbeNote != "":
		return st.ProbeNote
	case !on:
		return "выкл"
	case !auto:
		return "пауза — авто-регулятор выключен"
	case st.ProbePhase == probeHigh:
		return fmt.Sprintf("шаг %.0f А · цикл %d/%d · идёт %.0f с", hiA, st.ProbeCycle+1, cycles, time.Since(st.ProbeAt).Seconds())
	case st.ProbePhase == probeLow:
		return fmt.Sprintf("пауза %.0f А · цикл %d/%d · идёт %.0f с", loA, st.ProbeCycle+1, cycles, time.Since(st.ProbeAt).Seconds())
	}
	return "старт…"
}
