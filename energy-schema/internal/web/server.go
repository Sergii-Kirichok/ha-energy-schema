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
		for _, p := range pts {
			s.store.SeedDayMax(e, p.Time, p.Value)
		}
		log.Printf("seed daymax %s: %d points (today)", e, len(pts))
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
	go s.seedRolls()
	go s.loop()
	go s.loopForecast()
	go s.loopPVHistory()
	http.HandleFunc("/schematic.svg", s.handleSVG)
	http.HandleFunc("/", s.handleIndex)
	log.Println("energy-schema add-on on", listen)
	return http.ListenAndServe(listen, nil)
}
