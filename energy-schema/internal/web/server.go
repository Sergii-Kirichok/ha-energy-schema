// Package web wires the renderer to Home Assistant: it polls entity states,
// writes the SVG/HTML to /config/www (served at /local/) and serves the live
// schematic over the add-on ingress port.
package web

import (
	"fmt"
	"log"
	"net/http"
	"os"
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
const indexHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;background:#0f1115;height:100%%}#c{width:100%%}svg{width:100%%;height:auto;display:block}</style></head><body><div id="c"></div><script>
function load(){fetch('%s?t='+Date.now()).then(function(r){return r.text()}).then(function(t){document.getElementById('c').innerHTML=t})}
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

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, indexHTML, "schematic.svg", s.cfg.Refresh)
}

func (s *Server) writeFiles() {
	if err := os.WriteFile(wwwDir+"/energy_schema.svg", []byte(s.render()), 0644); err != nil {
		log.Println("write svg:", err)
	}
}

func (s *Server) writeWrapper() {
	page := fmt.Sprintf(indexHTML, "energy_schema.svg", s.cfg.Refresh)
	if err := os.WriteFile(wwwDir+"/energy_schema.html", []byte(page), 0644); err != nil {
		log.Println("write wrapper:", err)
	}
}

// weatherEntity is the HA weather entity used for the autonomy forecast.
const weatherEntity = "weather.forecast_home_assistant"

// loopForecast refreshes the daily weather forecast every 30 minutes.
func (s *Server) loopForecast() {
	for {
		if days, err := s.client.DailyForecast(weatherEntity); err != nil {
			log.Println("forecast:", err)
		} else {
			s.store.SetForecast(days)
		}
		time.Sleep(30 * time.Minute)
	}
}

// loop refreshes the state snapshot and the on-disk SVG on a fixed cadence.
func (s *Server) loop() {
	for {
		if m, err := s.client.FetchStates(); err != nil {
			log.Println("fetch:", err)
		} else {
			s.store.Replace(m)
		}
		s.writeFiles()
		time.Sleep(pollInterval)
	}
}

// Run starts the background poll loop and the HTTP server (blocking).
func (s *Server) Run() error {
	_ = os.MkdirAll(wwwDir, 0755)
	s.writeWrapper()
	go s.loop()
	go s.loopForecast()
	http.HandleFunc("/schematic.svg", s.handleSVG)
	http.HandleFunc("/", s.handleIndex)
	log.Println("energy-schema add-on on", listen)
	return http.ListenAndServe(listen, nil)
}
