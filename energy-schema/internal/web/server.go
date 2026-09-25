// Package web wires the renderer to Home Assistant: it polls entity states,
// writes the SVG/HTML to /config/www (served at /local/) and serves the live
// schematic over the add-on ingress port.
package web

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"energy-schema/internal/config"
	"energy-schema/internal/hass"
	"energy-schema/internal/scada"
	"energy-schema/internal/solar"
)

const (
	listen       = ":8099"
	wwwDir       = "/homeassistant/www"
	pollInterval = 2 * time.Second // опрос HA (раньше 5с) — меньше задержка до экрана
)

// Server renders and serves the schematic.
type Server struct {
	cfg    config.Config
	store  *hass.Store
	client *hass.Client
	solar  *solar.Provider // прогноз генерации (nil, если стринги/координаты не заданы)
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
	// имя пользователя HA в логе — только когда ему отказано в управлении:
	// это нужно, чтобы вписать его в control_users; всем остальным — не логируем
	if canCtl == "false" {
		log.Printf("index: view-only user=%q id=%q (add to control_users to allow)",
			r.Header.Get("X-Remote-User-Display-Name"), r.Header.Get("X-Remote-User-Id"))
	}
	// schematic.svg рендерится вживую при каждой загрузке; перезагружаем раз в 1с
	// (а не каждые cfg.Refresh) — минимальная задержка отображения данных
	_, _ = fmt.Fprintf(w, indexHTML, canCtl, "schematic.svg", 1)
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
	go s.loopBMS()
	go s.loopCharge()
	go s.loopGridRows()
	if s.cfg.BMSDashboard != "" {
		go s.ensureDashboard(s.cfg.BMSDashboard)
	}
	// прогноз генерации (геометрия + Open-Meteo) — если заданы стринги и координаты
	if len(s.cfg.PVStrings) > 0 {
		if lat, lon, elev, err := s.client.Location(); err != nil {
			log.Println("solar: не удалось получить координаты HA:", err)
		} else if lat == 0 && lon == 0 {
			log.Println("solar: координаты HA не заданы (0,0) — прогноз по геометрии выключен")
		} else {
			arrays := make([]solar.Array, 0, len(s.cfg.PVStrings))
			for _, ps := range s.cfg.PVStrings {
				arrays = append(arrays, solar.Array{Name: ps.Name, KWp: ps.KWp, TiltDeg: ps.Tilt, AzDeg: ps.Azimuth, Bifacial: ps.Bifacial})
			}
			s.solar = &solar.Provider{
				Loc:     solar.Location{Lat: lat, Lon: lon, AltM: elev},
				Arrays:  arrays,
				ACLimit: s.cfg.PVACLimitKW,
				TZ:      time.Local.String(),
				HTTP:    &http.Client{Timeout: 15 * time.Second},
				Cal:     solar.NewCalibrator("/data/calib.json", solar.GeoHash(arrays)),
			}
			go s.loopSolarForecast()
			go s.loopCalib()
			log.Printf("solar: провайдер активен — %d стрингов, AC-лимит %.0f кВт, tz %s, %.4f,%.4f", len(arrays), s.cfg.PVACLimitKW, time.Local.String(), lat, lon)
		}
	}
	// после инициализации s.solar (loopPersist читает s.solar без блокировки)
	go s.loopPersist()
	http.HandleFunc("/schematic.svg", s.handleSVG)
	http.HandleFunc("/control", s.handleControl)
	http.HandleFunc("/", s.handleIndex)
	log.Println("energy-schema add-on on", listen)
	return http.ListenAndServe(listen, nil)
}
