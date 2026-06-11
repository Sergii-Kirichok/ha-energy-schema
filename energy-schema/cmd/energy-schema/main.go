// Command energy-schema is a Home Assistant add-on that renders a live
// single-line diagram (SVG) of the BobRIXOS energy system.
package main

import (
	"log"
	"os"
	"time"
	_ "time/tzdata" // встроенная база TZ (контейнер аддона без zoneinfo)

	"energy-schema/internal/config"
	"energy-schema/internal/hass"
	"energy-schema/internal/web"
)

func main() {
	cfg := config.Load("/data/options.json", os.Getenv("SUPERVISOR_TOKEN"))
	log.Printf("start: tokenlen=%d apiBase=%s", len(cfg.Token), cfg.APIBase)

	store := hass.NewStore()
	client := hass.NewClient(cfg.APIBase, cfg.Token)
	// метки времени — в локальном поясе HA (контейнер аддона работает в UTC)
	if tz, err := client.TimeZone(); err == nil && tz != "" {
		if loc, e := time.LoadLocation(tz); e == nil {
			time.Local = loc
			log.Printf("timezone: %s", tz)
		} else {
			log.Printf("timezone %q load: %v", tz, e)
		}
	} else if err != nil {
		log.Printf("timezone fetch: %v", err)
	}
	srv := web.New(cfg, store, client)

	log.Fatal(srv.Run())
}
