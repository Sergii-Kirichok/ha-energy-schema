// Package config loads the add-on runtime options (/data/options.json)
// on top of built-in defaults, plus the Supervisor token from the environment.
package config

import (
	"encoding/json"
	"os"
	"strings"
)

// Config holds all runtime options for the renderer and HA client.
type Config struct {
	Refresh int    // seconds between HTML auto-reloads
	APIBase string // HA REST API base, e.g. http://homeassistant:8123/api
	Token   string // bearer token (LLAT or Supervisor)

	Title    string
	In1Name  string
	In2Name  string
	PVLabels [3]string

	BattCap                         float64
	HomeMax, HomeT1, HomeT2, HomeT3 float64
	PVMax, PVT1, PVT2, PVT3         float64
}

// Default returns the built-in defaults applied before options are loaded.
func Default() Config {
	return Config{
		Refresh:  3,
		APIBase:  "http://supervisor/core/api",
		Title:    "Энергосистема",
		In1Name:  "Рыбхоз",
		In2Name:  "Зелёный",
		PVLabels: [3]string{"Поле 1", "Поле 2", "Поле 3"},
		BattCap:  30.0,
		HomeMax:  30.0, HomeT1: 3.0, HomeT2: 5.0, HomeT3: 25.0,
		PVMax: 33.0, PVT1: 5.0, PVT2: 20.0, PVT3: 25.0,
	}
}

// options mirrors the add-on options schema in config.yaml.
type options struct {
	RefreshSeconds int     `json:"refresh_seconds"`
	HaURL          string  `json:"ha_url"`
	HaToken        string  `json:"ha_token"`
	Title          string  `json:"title"`
	In1Name        string  `json:"in1_name"`
	In2Name        string  `json:"in2_name"`
	Pv1            string  `json:"pv1_label"`
	Pv2            string  `json:"pv2_label"`
	Pv3            string  `json:"pv3_label"`
	BattCap        float64 `json:"batt_capacity_kwh"`
	HomeMax        float64 `json:"home_max"`
	HomeT1         float64 `json:"home_t1"`
	HomeT2         float64 `json:"home_t2"`
	HomeT3         float64 `json:"home_t3"`
	PvMax          float64 `json:"pv_max"`
	PvT1           float64 `json:"pv_t1"`
	PvT2           float64 `json:"pv_t2"`
	PvT3           float64 `json:"pv_t3"`
}

// Load returns Default() with the options file and supervisorToken applied.
// A missing or unparseable options file leaves the defaults (plus env token).
func Load(optionsPath, supervisorToken string) Config {
	c := Default()
	c.Token = supervisorToken
	if b, err := os.ReadFile(optionsPath); err == nil {
		var o options
		if json.Unmarshal(b, &o) == nil {
			c.apply(o)
		}
	}
	return c
}

// apply overlays non-zero option fields onto the config.
func (c *Config) apply(o options) {
	if o.RefreshSeconds > 0 {
		c.Refresh = o.RefreshSeconds
	}
	if o.Title != "" {
		c.Title = o.Title
	}
	if o.In1Name != "" {
		c.In1Name = o.In1Name
	}
	if o.In2Name != "" {
		c.In2Name = o.In2Name
	}
	for i, v := range []string{o.Pv1, o.Pv2, o.Pv3} {
		if v != "" {
			c.PVLabels[i] = v
		}
	}
	if o.BattCap > 0 {
		c.BattCap = o.BattCap
	}
	if o.HomeMax > 0 {
		c.HomeMax = o.HomeMax
	}
	if o.HomeT1 > 0 {
		c.HomeT1 = o.HomeT1
	}
	if o.HomeT2 > 0 {
		c.HomeT2 = o.HomeT2
	}
	if o.HomeT3 > 0 {
		c.HomeT3 = o.HomeT3
	}
	if o.PvMax > 0 {
		c.PVMax = o.PvMax
	}
	if o.PvT1 > 0 {
		c.PVT1 = o.PvT1
	}
	if o.PvT2 > 0 {
		c.PVT2 = o.PvT2
	}
	if o.PvT3 > 0 {
		c.PVT3 = o.PvT3
	}
	// LLAT path: only switch APIBase off the Supervisor default when a token is given.
	if o.HaToken != "" {
		c.Token = o.HaToken
		u := o.HaURL
		if u == "" {
			u = "http://homeassistant:8123"
		}
		c.APIBase = strings.TrimRight(u, "/") + "/api"
	}
}
