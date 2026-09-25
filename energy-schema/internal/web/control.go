package web

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

// simURL — базовый адрес эмулятора. Пока действия управления (переключение
// источника АВР) роутятся в него; на реальной системе это станут вызовы сервиса
// HA / запись по Modbus. Эмулятор отдаёт GET /set?id=&v= и тут же пушит в HA.
const simURL = "http://192.168.0.16:8088"

// simSet pushes a value to the emulator, which immediately publishes it to HA.
func (s *Server) simSet(id, v string) error {
	// s.client.HTTP — тот же клиент с таймаутом 20с, иначе зависший эмулятор
	// навсегда блокирует обработчик /control
	resp, err := s.client.HTTP.Get(simURL + "/set?id=" + url.QueryEscape(id) + "&v=" + url.QueryEscape(v))
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("emulator status %d", resp.StatusCode)
	}
	return nil
}

// handleControl performs a control action requested by a tap on the schematic.
// Safety (e.g. AVR manual-only) is enforced here server-side — never trust the
// client. Dangerous actions still require a confirm dialog in the UI.
func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	// CSRF: только POST с той же страницы. GET-ссылка/<img src> с чужого сайта не
	// должна переключать контактор. Sec-Fetch-Site ставит браузер (подделать
	// нельзя); если заголовка нет (старый браузер/прокси срезал) — пропускаем.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" && sfs != "same-origin" {
		http.Error(w, "cross-site request rejected", http.StatusForbidden)
		return
	}
	if !s.userAllowed(r) {
		http.Error(w, "только просмотр — нет прав управления", http.StatusForbidden)
		return
	}
	act, val := r.URL.Query().Get("act"), r.URL.Query().Get("val")
	switch act {
	case "avr_src": // переключение источника АВР Инвертор↔Резерв — только в РУЧНОМ
		if s.store.State("sensor.sim_avr_mode") != "manual" {
			http.Error(w, "АВР в авто — переключение недоступно", http.StatusConflict)
			return
		}
		if val != "inverter" && val != "reserve" {
			http.Error(w, "недопустимый источник", http.StatusBadRequest)
			return
		}
		if err := s.simSet("sim_avr_pos", val); err != nil {
			http.Error(w, "нет связи с устройством: "+err.Error(), http.StatusBadGateway)
			return
		}
		// учёт переключений (всего / сегодня)
		_ = s.simSet("sim_avr_switches", fmt.Sprintf("%.0f", s.store.Num("sensor.sim_avr_switches")+1))
		_ = s.simSet("sim_avr_switches_today", fmt.Sprintf("%.0f", s.store.Num("sensor.sim_avr_switches_today")+1))
		log.Printf("control: avr_src -> %s", val)
		_, _ = w.Write([]byte("ok"))
	case "avr3_src": // АВР ген.: Дом от АВР дома ↔ от генератора — только в РУЧНОМ
		if s.store.State("sensor.sim_avr3_mode") != "manual" {
			http.Error(w, "АВР ген. в авто — переключение недоступно", http.StatusConflict)
			return
		}
		if s.store.State("sensor.sim_avr3_link") != "ok" {
			http.Error(w, "нет связи с АВР ген. (RS-485)", http.StatusConflict)
			return
		}
		if val != "main" && val != "gen" {
			http.Error(w, "недопустимый вход", http.StatusBadRequest)
			return
		}
		if err := s.simSet("sim_avr3_pos", val); err != nil {
			http.Error(w, "нет связи с устройством: "+err.Error(), http.StatusBadGateway)
			return
		}
		log.Printf("control: avr3_src -> %s", val)
		_, _ = w.Write([]byte("ok"))
	case "contactor": // переключение ввода контактора Ввод1↔Ввод2 (sim_contactor off/on)
		if s.store.State("sensor.sim_contactor_link") == "lost" {
			http.Error(w, "нет связи с АВР вводов (RS-485)", http.StatusConflict)
			return
		}
		v := ""
		switch val {
		case "in1":
			v = "off"
		case "in2":
			v = "on"
		default:
			http.Error(w, "недопустимый ввод", http.StatusBadRequest)
			return
		}
		if err := s.simSet("sim_contactor", v); err != nil {
			http.Error(w, "нет связи с устройством: "+err.Error(), http.StatusBadGateway)
			return
		}
		log.Printf("control: contactor -> %s", val)
		_, _ = w.Write([]byte("ok"))
	case "param": // быстрые настройки с оборота карточки АКБ (хелперы регулятора заряда)
		msg, err := s.quickParam(val)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("control: param %s", msg)
		_, _ = w.Write([]byte("ok"))
	case "gen_start", "gen_stop", "gen_heater": // управление генератором — только в АВТО
		if s.store.State("sensor.sim_gen_mode") != "auto" {
			http.Error(w, "генератор в ручном режиме — управление недоступно", http.StatusConflict)
			return
		}
		var id, v string
		switch act {
		case "gen_start":
			id, v = "sim_gen_state", "running"
		case "gen_stop":
			id, v = "sim_gen_state", "off"
		case "gen_heater":
			if val != "on" && val != "off" {
				http.Error(w, "bad val", http.StatusBadRequest)
				return
			}
			id, v = "sim_gen_coolant_heater", val
		}
		if err := s.simSet(id, v); err != nil {
			http.Error(w, "нет связи с устройством: "+err.Error(), http.StatusBadGateway)
			return
		}
		log.Printf("control: %s -> %s", act, v)
		_, _ = w.Write([]byte("ok"))
	default:
		http.Error(w, "неизвестное действие", http.StatusBadRequest)
	}
}
