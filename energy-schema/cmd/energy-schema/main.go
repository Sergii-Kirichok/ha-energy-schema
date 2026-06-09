// Command energy-schema is a Home Assistant add-on that renders a live
// single-line diagram (SVG) of the BobRIXOS energy system.
package main

import (
	"log"
	"os"

	"energy-schema/internal/config"
	"energy-schema/internal/hass"
	"energy-schema/internal/web"
)

func main() {
	cfg := config.Load("/data/options.json", os.Getenv("SUPERVISOR_TOKEN"))
	log.Printf("start: tokenlen=%d apiBase=%s", len(cfg.Token), cfg.APIBase)

	store := hass.NewStore()
	client := hass.NewClient(cfg.APIBase, cfg.Token)
	srv := web.New(cfg, store, client)

	log.Fatal(srv.Run())
}
