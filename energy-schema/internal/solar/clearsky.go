// Package solar computes clear-sky PV potential and plane-of-array irradiance
// from panel geometry alone (no external services). It is used as the physical
// ceiling for the forecast, the reference for self-calibration, and the offline
// fallback when no weather/radiation source is reachable.
package solar

import (
	"math"
	"time"
)

// Array describes one PV string's geometry. AzDeg is a COMPASS azimuth
// (0=N, 90=E, 180=S, 270=W). Bifacial is the rear-side gain fraction
// (0.12 = +12%): front-side clear-sky / GTI models miss it, so it is applied
// explicitly — and output is intentionally NOT capped at nameplate KWp, because
// rear-side gain and edge-of-cloud over-irradiance legitimately exceed it.
type Array struct {
	Name     string
	KWp      float64
	TiltDeg  float64 // 0 = horizontal, 90 = vertical
	AzDeg    float64 // compass: 0=N, 90=E, 180=S, 270=W
	Bifacial float64 // rear-side extra gain fraction (0 = monofacial)
}

// Location is the site position (degrees, metres).
type Location struct {
	Lat, Lon, AltM float64
}

// linkeTurbidity — representative monthly Linke turbidity for ~50°N (Jan..Dec).
var linkeTurbidity = [12]float64{2.6, 2.9, 3.2, 3.4, 3.5, 3.5, 3.6, 3.5, 3.2, 3.0, 2.8, 2.6}

const (
	albedo    = 0.20   // ground reflectance for the isotropic reflected term
	prDC      = 0.96   // wiring/soiling/conversion losses (temperature handled separately)
	tempCoeff = 0.0038 // power loss per K above 25°C (≈ -0.38%/K)
)

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }

// SunPos returns the sun's elevation and COMPASS azimuth (degrees) at UTC time
// t for the given site. Spencer declination + equation of time.
func SunPos(t time.Time, lat, lon float64) (elevDeg, azDeg float64) {
	t = t.UTC()
	n := float64(t.YearDay())
	utcH := float64(t.Hour()) + float64(t.Minute())/60 + float64(t.Second())/3600
	g := 2 * math.Pi / 365 * (n - 1 + (utcH-12)/24)
	decl := 0.006918 - 0.399912*math.Cos(g) + 0.070257*math.Sin(g) -
		0.006758*math.Cos(2*g) + 0.000907*math.Sin(2*g) -
		0.002697*math.Cos(3*g) + 0.00148*math.Sin(3*g)
	eot := 229.18 * (0.000075 + 0.001868*math.Cos(g) - 0.032077*math.Sin(g) -
		0.014615*math.Cos(2*g) - 0.040849*math.Sin(2*g))
	tst := math.Mod(utcH*60+eot+4*lon+1440, 1440) // true solar time, minutes
	ha := rad(tst/4 - 180)                        // hour angle (0 at solar noon)
	la := rad(lat)
	sinEl := math.Sin(la)*math.Sin(decl) + math.Cos(la)*math.Cos(decl)*math.Cos(ha)
	sinEl = math.Max(-1, math.Min(1, sinEl))
	el := math.Asin(sinEl)
	cosEl := math.Cos(el)
	az := 0.0
	if cosEl > 1e-6 {
		cosAz := (math.Sin(decl) - math.Sin(el)*math.Sin(la)) / (cosEl * math.Cos(la))
		cosAz = math.Max(-1, math.Min(1, cosAz))
		az = math.Acos(cosAz) // 0..π, measured from north
		if ha > 0 {           // afternoon → west of south
			az = 2*math.Pi - az
		}
	}
	return deg(el), deg(az)
}

// clearSky returns GHI/DNI/DHI (W/m²) at a given solar elevation using the
// Ineichen–Perez clear-sky model. month is 1..12, n is day-of-year.
func clearSky(elevDeg, altM, n float64, month int) (ghi, dni, dhi float64) {
	if elevDeg <= 0.5 {
		return 0, 0, 0
	}
	el := rad(elevDeg)
	sinEl := math.Sin(el)
	tl := linkeTurbidity[(month-1)%12]
	am := 1 / (sinEl + 0.50572*math.Pow(elevDeg+6.07995, -1.6364)) // Kasten–Young
	i0 := 1366.1 * (1 + 0.033*math.Cos(2*math.Pi*n/365))
	fh1 := math.Exp(-altM / 8000)
	fh2 := math.Exp(-altM / 1250)
	cg1 := 5.09e-5*altM + 0.868
	cg2 := 3.92e-5*altM + 0.0387
	ghi = cg1 * i0 * sinEl * math.Exp(-cg2*am*(fh1+fh2*(tl-1))) * math.Exp(0.01*math.Pow(am, 1.8))
	if ghi < 0 {
		ghi = 0
	}
	b := 0.664 + 0.163/fh1
	dni = b * i0 * math.Exp(-0.09*am*(tl-1))
	if dni < 0 {
		dni = 0
	}
	if dni*sinEl > ghi { // beam-horizontal cannot exceed global
		dni = ghi / sinEl
	}
	dhi = ghi - dni*sinEl
	if dhi < 0 {
		dhi = 0
	}
	return
}

// poa returns plane-of-array irradiance (W/m²) via isotropic-sky transposition.
func poa(ghi, dni, dhi, sunElDeg, sunAzDeg, tiltDeg, panelAzDeg float64) float64 {
	el := rad(sunElDeg)
	tilt := rad(tiltDeg)
	cosAOI := math.Sin(el)*math.Cos(tilt) + math.Cos(el)*math.Sin(tilt)*math.Cos(rad(sunAzDeg-panelAzDeg))
	if cosAOI < 0 {
		cosAOI = 0 // sun behind the panel plane (hard cut-off — key for vertical arrays)
	}
	direct := dni * cosAOI
	diffuse := dhi * (1 + math.Cos(tilt)) / 2
	reflected := ghi * albedo * (1 - math.Cos(tilt)) / 2
	return direct + diffuse + reflected
}

// ArrayPowerKW returns the array's DC power (kW) at given POA irradiance and
// ambient temperature. NOT capped at nameplate KWp — bifacial rear-gain and
// over-irradiance legitimately exceed it.
func ArrayPowerKW(a Array, poaWm2, tAmbC float64) float64 {
	if poaWm2 <= 0 || a.KWp <= 0 {
		return 0
	}
	tCell := tAmbC + 0.03*poaWm2 // NOCT≈45°C → ~0.03 K per W/m²
	etaT := 1 - tempCoeff*math.Max(0, tCell-25)
	return a.KWp * poaWm2 / 1000 * etaT * prDC * (1 + a.Bifacial)
}

// InstantKW returns total instantaneous clear-sky DC power (kW) of all arrays at
// UTC time t, clipped to acLimitKW on the SUM (inverter ceiling, 0 = no clip).
func InstantKW(t time.Time, loc Location, arrays []Array, acLimitKW float64) float64 {
	el, az := SunPos(t, loc.Lat, loc.Lon)
	if el <= 0.5 {
		return 0
	}
	tt := t.UTC()
	ghi, dni, dhi := clearSky(el, loc.AltM, float64(tt.YearDay()), int(tt.Month()))
	const tAmb = 20.0 // nominal ambient for the clear-sky ceiling
	sum := 0.0
	for _, a := range arrays {
		sum += ArrayPowerKW(a, poa(ghi, dni, dhi, el, az, a.TiltDeg, a.AzDeg), tAmb)
	}
	if acLimitKW > 0 && sum > acLimitKW {
		sum = acLimitKW
	}
	return sum
}

// HourlyClearSky returns clear-sky energy (kWh) for each local hour 0..23 of the
// given local date, integrating at 10-minute steps with AC clipping on the sum.
func HourlyClearSky(date time.Time, loc Location, arrays []Array, acLimitKW float64) [24]float64 {
	var out [24]float64
	y, m, d := date.Date()
	tz := date.Location()
	const step = 10 * time.Minute
	for h := 0; h < 24; h++ {
		hStart := time.Date(y, m, d, h, 0, 0, 0, tz)
		e := 0.0
		for off := time.Duration(0); off < time.Hour; off += step {
			e += InstantKW(hStart.Add(off), loc, arrays, acLimitKW) * step.Hours()
		}
		out[h] = e
	}
	return out
}

// SolarNoon returns the local time of solar noon (max elevation) on the date.
func SolarNoon(date time.Time, loc Location) time.Time {
	y, m, d := date.Date()
	tz := date.Location()
	best := time.Date(y, m, d, 12, 0, 0, 0, tz)
	bestEl := -90.0
	for min := 0; min < 1440; min++ {
		t := time.Date(y, m, d, 0, min, 0, 0, tz)
		if el, _ := SunPos(t, loc.Lat, loc.Lon); el > bestEl {
			bestEl, best = el, t
		}
	}
	return best
}
