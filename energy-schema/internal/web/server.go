// Package web wires the renderer to Home Assistant: it polls entity states,
// writes the SVG/HTML to /config/www (served at /local/) and serves the live
// schematic over the add-on ingress port.
package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"energy-schema/internal/config"
	"energy-schema/internal/hass"
	"energy-schema/internal/scada"
)

const (
	listen       = ":8099"
	wwwDir       = "/homeassistant/www"
	pollInterval = 5 * time.Second
)

// indexHTML auto-reloads the given SVG file every refresh seconds.
// %s = svg filename, %d = refresh seconds.
const indexHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;height:100%%;overflow:hidden;background:#0f1115}#c{position:fixed;inset:0}#c svg{width:100%%;height:100%%;display:block}#vo{position:fixed;top:8px;right:12px;background:#1f2937;color:#9ca3af;font:12px sans-serif;padding:4px 10px;border-radius:8px;opacity:.85;z-index:9}</style></head><body><div id="c"></div><div id="vo" style="display:none">только просмотр</div><script>
var CANCTL=%s;
function ask(act,val){
 var m='Выполнить действие?';
 if(act==='avr_src'){m='Переключить питание Дома на: '+(val==='reserve'?'Резерв (стабилизаторы)':'Инвертор')+'?';}
 if(act==='contactor'){m='Переключить контактор на: '+(val==='in2'?'Ввод 2 (Зелёный)':'Ввод 1 (Рыбхоз)')+'?';}
 if(act==='gen_start'){m='Запустить генератор?';}
 if(act==='gen_stop'){m='Остановить генератор?';}
 if(act==='gen_heater'){m=(val==='on'?'Включить':'Выключить')+' подогрев генератора?';}
 if(!confirm(m))return;
 fetch('control?act='+act+'&val='+val).then(function(r){
  if(!r.ok){r.text().then(function(t){alert('Не выполнено: '+t);});}else{setTimeout(load,400);}
 }).catch(function(e){alert('Ошибка связи: '+e);});
}
function wire(){if(!CANCTL)return;var els=document.querySelectorAll('#c [data-act]');for(var i=0;i<els.length;i++){(function(el){el.addEventListener('click',function(){ask(el.getAttribute('data-act'),el.getAttribute('data-val'));});})(els[i]);}}
var T0=Date.now();
function load(){fetch('%s?t='+Date.now()).then(function(r){return r.text()}).then(function(t){var c=document.getElementById('c');c.innerHTML=t;var s=c.querySelector('svg');if(s&&s.setCurrentTime){try{s.setCurrentTime((Date.now()-T0)/1000);}catch(e){}}wire();})}
if(!CANCTL){document.getElementById('vo').style.display='block';}
load();setInterval(load,%d000);</script></body></html>`

// Server renders and serves the schematic.
type Server struct {
	cfg    config.Config
	store  *hass.Store
	client *hass.Client
}

// New builds a Server.
func New(cfg config.Config, store *hass.Store, client *hass.Client) *Server {
	return &Server{cfg: cfg, store: store, client: client}
}

func (s *Server) render() string { return scada.Render(s.store, s.cfg) }

func (s *Server) handleSVG(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(s.render()))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	canCtl := "false"
	if s.userAllowed(r) {
		canCtl = "true"
	}
	// разово помогает узнать точное имя пользователя HA для control_users
	log.Printf("index: user=%q id=%q control=%s",
		r.Header.Get("X-Remote-User-Display-Name"), r.Header.Get("X-Remote-User-Id"), canCtl)
	fmt.Fprintf(w, indexHTML, canCtl, "schematic.svg", s.cfg.Refresh)
}

// userAllowed reports whether the HA user making this ingress request may control
// (АВР/контактор/генератор). Empty ControlUsers = everyone may (backward compat).
// HA Supervisor ingress forwards the user via X-Remote-User-* headers.
func (s *Server) userAllowed(r *http.Request) bool {
	if len(s.cfg.ControlUsers) == 0 {
		return true
	}
	name := r.Header.Get("X-Remote-User-Display-Name")
	if name == "" {
		name = r.Header.Get("X-Remote-User-Name")
	}
	id := r.Header.Get("X-Remote-User-Id")
	for _, u := range s.cfg.ControlUsers {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if strings.EqualFold(u, name) || u == id {
			return true
		}
	}
	return false
}

func (s *Server) writeFiles() {
	if err := os.WriteFile(wwwDir+"/energy_schema.svg", []byte(s.render()), 0644); err != nil {
		log.Println("write svg:", err)
	}
}

func (s *Server) writeWrapper() {
	// файловый враппер в /local/ открывают вне ingress (эндпоинт /control недоступен),
	// поэтому он всегда «только просмотр» — удобен для дашбордов и ТВ.
	page := fmt.Sprintf(indexHTML, "false", "energy_schema.svg", s.cfg.Refresh)
	if err := os.WriteFile(wwwDir+"/energy_schema.html", []byte(page), 0644); err != nil {
		log.Println("write wrapper:", err)
	}
}

// simURL — базовый адрес эмулятора. Пока действия управления (переключение
// источника АВР) роутятся в него; на реальной системе это станут вызовы сервиса
// HA / запись по Modbus. Эмулятор отдаёт GET /set?id=&v= и тут же пушит в HA.
const simURL = "http://192.168.0.16:8088"

// simSet pushes a value to the emulator, which immediately publishes it to HA.
func (s *Server) simSet(id, v string) error {
	resp, err := http.Get(simURL + "/set?id=" + url.QueryEscape(id) + "&v=" + url.QueryEscape(v))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// handleControl performs a control action requested by a tap on the schematic.
// Safety (e.g. AVR manual-only) is enforced here server-side — never trust the
// client. Dangerous actions still require a confirm dialog in the UI.
func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
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
		w.Write([]byte("ok"))
	case "contactor": // переключение ввода контактора Ввод1↔Ввод2 (sim_contactor off/on)
		if s.store.State("sensor.sim_contactor_link") == "lost" {
			http.Error(w, "нет связи с контактором (RS-485)", http.StatusConflict)
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
		w.Write([]byte("ok"))
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
		w.Write([]byte("ok"))
	default:
		http.Error(w, "неизвестное действие", http.StatusBadRequest)
	}
}

// weatherEntity is the HA weather entity used for the autonomy forecast.
const weatherEntity = "weather.forecast_home_assistant"

// productionEntity is the cumulative lifetime PV energy sensor; its long-term
// statistics give us real daily generation (the recorder keeps only ~2 days of
// raw history, but statistics persist for a year).
const productionEntity = "sensor.deye_sun_30k_total_production"

// loopForecast refreshes the daily weather forecast every 30 minutes.
func (s *Server) loopForecast() {
	for {
		if days, err := s.client.DailyForecast(weatherEntity); err != nil {
			log.Println("forecast:", err)
		} else {
			s.store.SetForecast(days)
		}
		if hrs, err := s.client.HourlyForecast(weatherEntity); err != nil {
			log.Println("hourly forecast:", err)
		} else {
			s.store.SetHourly(hrs)
		}
		time.Sleep(30 * time.Minute)
	}
}

// loopPVHistory refreshes the empirical generation baseline from long-term
// statistics every 3 hours: best recent day (clear-day proxy) and the average.
// Forecasting tomorrow's yield off real recent days beats a fixed nameplate
// guess — in winter the "clear day" is far below a summer one.
func (s *Server) loopPVHistory() {
	for {
		if daily, err := s.client.DailyProduction(productionEntity, 10); err != nil {
			log.Println("pv history:", err)
		} else if len(daily) > 0 {
			best, sum := 0.0, 0.0
			for _, v := range daily {
				if v > best {
					best = v
				}
				sum += v
			}
			s.store.SetPVStats(best, sum/float64(len(daily)), len(daily))
			log.Printf("pv history: %d days, best %.0f kWh, avg %.0f kWh", len(daily), best, sum/float64(len(daily)))
		}
		time.Sleep(3 * time.Hour)
	}
}

// rollSeedEntities have their rolling 24h min/max seeded from history at
// startup so the home/battery markers don't reset to "now" on every restart.
var rollSeedEntities = []string{
	"sensor.deye_sun_30k_load_power",
	"sensor.deye_sun_30k_battery",
}

// rollFile persists the rolling 24h buffers across restarts (the HA recorder
// drops peaks our 5s poll catches). /data is the add-on's persistent volume.
const rollFile = "/data/roll.json"

// rollPersistEntities are the entities whose 24h min/avg/max must survive a
// restart (battery SOC peak, home load min/avg/max).
var rollPersistEntities = []string{
	"sensor.deye_sun_30k_battery",
	"sensor.deye_sun_30k_load_power",
}

// loopPersist saves the rolling buffers to disk every minute.
func (s *Server) loopPersist() {
	for {
		time.Sleep(60 * time.Second)
		if err := s.store.SaveRoll(rollFile, rollPersistEntities); err != nil {
			log.Println("save roll:", err)
		}
	}
}

// pvDayMaxEntities have today's peak seeded from history (sun "Max today").
var pvDayMaxEntities = []string{
	"sensor.deye_sun_30k_pv_power",
	"sensor.deye_sun_30k_pv1_power",
	"sensor.deye_sun_30k_pv2_power",
	"sensor.deye_sun_30k_pv3_power",
}

// seedRolls pre-fills the rolling 24h windows from recorder history so a freshly
// restarted add-on already reflects the true last-24h min/max, not just values
// seen since boot.
func (s *Server) seedRolls() {
	since := time.Now().Add(-24 * time.Hour)
	for _, e := range rollSeedEntities {
		pts, err := s.client.History(e, since)
		if err != nil {
			log.Println("seed history:", e, err)
			continue
		}
		for _, p := range pts {
			s.store.SeedRoll(e, p.Time, p.Value)
		}
		log.Printf("seed %s: %d points (24h)", e, len(pts))
	}
	// sun "Max today" peaks — seed from today's history so they survive a restart
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, e := range pvDayMaxEntities {
		pts, err := s.client.History(e, midnight)
		if err != nil {
			log.Println("seed daymax:", e, err)
			continue
		}
		kwh := 0.0 // интеграл мощности по трапециям — энергия с начала дня
		for i, p := range pts {
			s.store.SeedDayMax(e, p.Time, p.Value)
			if i > 0 {
				if d := p.Time.Sub(pts[i-1].Time).Hours(); d > 0 && d < 0.5 {
					kwh += (pts[i-1].Value + p.Value) / 2 / 1000 * d
				}
			}
		}
		s.store.SetDayEnergy(e, kwh)
		log.Printf("seed %s: %d points, %.1f kWh today", e, len(pts), kwh)
	}
}

// trackReconnect derives the inverter→grid reconnection state from the device:
// grid present (qualified) but the inverter relay not yet bonded to it = waiting.
// The device exposes only the delay setpoint, not a live countdown, so the Store
// times it — restarting on each re-entry (a failed attempt drops the relay again).
func (s *Server) trackReconnect() {
	// «сеть присутствует» — по НАПРЯЖЕНИЮ фаз (оно появляется сразу при возврате
	// сети), а не по binary_sensor.grid (тот включается лишь в момент подключения,
	// поэтому окно ожидания «напряжение есть, но не подключился» им не поймать).
	gridPresent := s.store.Num("sensor.deye_sun_30k_grid_l1_voltage") > 150 ||
		s.store.Num("sensor.deye_sun_30k_grid_l2_voltage") > 150 ||
		s.store.Num("sensor.deye_sun_30k_grid_l3_voltage") > 150
	bonded := strings.Contains(s.store.State("sensor.deye_sun_30k_device_relay"), "Grid")
	total := s.store.Num("number.deye_sun_30k_grid_reconnection_time")
	s.store.UpdateReconnect(gridPresent && !bonded, gridPresent, total)
}

// loop refreshes the state snapshot and the on-disk SVG on a fixed cadence.
func (s *Server) loop() {
	for {
		if m, err := s.client.FetchStates(); err != nil {
			log.Println("fetch:", err)
		} else {
			s.store.Replace(m)
			s.trackReconnect()
		}
		s.writeFiles()
		time.Sleep(pollInterval)
	}
}

// loopAnim re-renders the on-disk SVG ~once a second so the marching flow arrows
// move smoothly on the TV (host re-fetches /local/energy_schema.svg; rsvg can't
// play SMIL). Data itself refreshes on the slower poll loop.
func (s *Server) loopAnim() {
	for {
		time.Sleep(time.Second)
		s.writeFiles()
	}
}

// Run starts the background poll loop and the HTTP server (blocking).
func (s *Server) Run() error {
	_ = os.MkdirAll(wwwDir, 0755)
	s.writeWrapper()
	// restore persisted 24h buffers BEFORE polling starts (peaks the recorder lost)
	if err := s.store.LoadRoll(rollFile); err != nil {
		log.Println("roll: no persisted file yet (ok on first run):", err)
	} else {
		log.Println("roll: restored 24h buffers from", rollFile)
	}
	// зафиксировать текущие локальные сутки ДО старта опроса/сидинга, иначе первый
	// Replace (видя пустой dayYMD) обнулит подсеянный из истории суточный пик
	s.store.InitDayBoundary()
	go s.seedRolls()
	go s.loop()
	go s.loopAnim()
	go s.loopForecast()
	go s.loopPVHistory()
	go s.loopPersist()
	http.HandleFunc("/schematic.svg", s.handleSVG)
	http.HandleFunc("/control", s.handleControl)
	http.HandleFunc("/", s.handleIndex)
	log.Println("energy-schema add-on on", listen)
	return http.ListenAndServe(listen, nil)
}
