package solar

import (
	"math"
	"net/http"
	"time"
)

// Provider builds the hourly + daily generation forecast from, in priority:
// Open-Meteo GTI (radiation on each plane), clear-sky × cloud (offline fallback),
// or bare clear-sky (last resort).
type Provider struct {
	Loc     Location
	Arrays  []Array
	ACLimit float64
	TZ      string // IANA name for Open-Meteo
	HTTP    *http.Client
}

// Snapshot mirrors the fields the Store/renderer need.
type Snapshot struct {
	StartDay                   time.Time
	HourlyKWh                  [72]float64
	Today, TodayLeft, Tomorrow float64
	Source                     string
}

// CloudSource supplies fallback per-hour cloud (implemented by *hass.Store).
type CloudSource interface {
	HourlyCloud(daysAhead int) ([24]float64, [24]bool)
}

// Build computes the forecast snapshot for the local day containing now.
func (p *Provider) Build(now time.Time, cs CloudSource) Snapshot {
	tz := now.Location()
	y, m, d := now.Date()
	startDay := time.Date(y, m, d, 0, 0, 0, 0, tz)
	snap := Snapshot{StartDay: startDay}

	// physical clear-sky ceiling per hour over the 72h horizon
	var clr [72]float64
	for day := 0; day < 3; day++ {
		hc := HourlyClearSky(startDay.AddDate(0, 0, day), p.Loc, p.Arrays, p.ACLimit)
		copy(clr[day*24:day*24+24], hc[:])
	}

	raw := p.fromOpenMeteo(startDay)
	src := "open-meteo"
	if raw == nil {
		if cs != nil {
			if r := fromCloud(clr, cs); r != nil {
				raw, src = r, "met.no"
			}
		}
	}
	if raw == nil {
		var r [72]float64
		for i := range clr {
			r[i] = clr[i] * 0.7
		}
		raw, src = &r, "clearsky"
	}

	for i := 0; i < 72; i++ {
		v := raw[i]
		if lim := 1.3 * clr[i]; lim > 0 && v > lim { // не выше 1.3× физического потолка
			v = lim
		}
		if v < 0 {
			v = 0
		}
		snap.HourlyKWh[i] = v
	}

	for h := 0; h < 24; h++ {
		snap.Today += snap.HourlyKWh[h]
		snap.Tomorrow += snap.HourlyKWh[24+h]
	}
	if cur := now.Sub(startDay).Hours(); cur >= 0 && cur < 24 {
		ch := int(cur)
		snap.TodayLeft += snap.HourlyKWh[ch] * (1 - (cur - float64(ch))) // остаток текущего часа
		for h := ch + 1; h < 24; h++ {
			snap.TodayLeft += snap.HourlyKWh[h]
		}
	}
	snap.Source = src
	return snap
}

// fromOpenMeteo returns the raw hourly kWh from Open-Meteo, or nil on failure.
func (p *Provider) fromOpenMeteo(startDay time.Time) *[72]float64 {
	hours, err := FetchOpenMeteo(p.Loc, p.Arrays, p.TZ, p.HTTP)
	if err != nil || len(hours) == 0 {
		return nil
	}
	var out [72]float64
	any := false
	for _, oh := range hours {
		idx := int(oh.HourStart.Sub(startDay).Hours() + 0.5)
		if idx < 0 || idx >= 72 {
			continue
		}
		sum := 0.0
		for i, a := range p.Arrays {
			sum += ArrayPowerKW(a, oh.GTI[i], oh.TempC)
		}
		if p.ACLimit > 0 && sum > p.ACLimit {
			sum = p.ACLimit
		}
		out[idx] = sum // кВт × 1ч = кВт·ч
		if sum > 0 {
			any = true
		}
	}
	if !any {
		return nil
	}
	return &out
}

// fromCloud modulates the clear-sky ceiling by hourly cloud (Kasten–Czeplak).
func fromCloud(clr [72]float64, cs CloudSource) *[72]float64 {
	var out [72]float64
	any := false
	for day := 0; day < 3; day++ {
		cloud, okm := cs.HourlyCloud(day)
		for h := 0; h < 24; h++ {
			i := day*24 + h
			if okm[h] {
				c := cloud[h] / 100
				out[i] = clr[i] * (1 - 0.75*math.Pow(c, 3.4))
				any = true
			} else {
				out[i] = clr[i] * 0.7
			}
		}
	}
	if !any {
		return nil
	}
	return &out
}
