package solar

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// OMHour is one hour of Open-Meteo data, aligned so HourStart is the LOCAL start
// of the hour [HourStart, HourStart+1h). Open-Meteo radiation is a preceding-hour
// mean, so the sample labelled L describes [L-1h, L) → HourStart = L-1h.
type OMHour struct {
	HourStart time.Time
	GTI       []float64 // W/m² per array, same order as the arrays argument
	TempC     float64
	CloudPct  float64
}

type omResp struct {
	Hourly struct {
		Time  []string   `json:"time"`
		GTI   []*float64 `json:"global_tilted_irradiance"`
		Temp  []*float64 `json:"temperature_2m"`
		Cloud []*float64 `json:"cloud_cover"`
	} `json:"hourly"`
}

// omAzimuth converts a COMPASS azimuth (0=N,90=E,180=S,270=W) to Open-Meteo's
// convention (0=S, -90=E, +90=W, ±180=N).
func omAzimuth(compass float64) float64 {
	a := compass - 180
	if a < -180 {
		a += 360
	}
	return a
}

// FetchOpenMeteo queries Open-Meteo GTI (one request per array orientation,
// since tilt/azimuth differ) and returns the merged hourly series sorted by time.
// tz is an IANA name (e.g. "Europe/Kyiv"); "" falls back to "auto".
func FetchOpenMeteo(loc Location, arrays []Array, tz string, httpc *http.Client) ([]OMHour, error) {
	if len(arrays) == 0 {
		return nil, fmt.Errorf("no arrays")
	}
	if tz == "" {
		tz = "auto"
	}
	loct, _ := time.LoadLocation(tz)
	if loct == nil {
		loct = time.Local
	}
	var times []string
	var temp, cloud []*float64
	gtiByArray := make([][]*float64, len(arrays))
	for i, a := range arrays {
		q := url.Values{}
		q.Set("latitude", fmt.Sprintf("%.5f", loc.Lat))
		q.Set("longitude", fmt.Sprintf("%.5f", loc.Lon))
		q.Set("hourly", "global_tilted_irradiance,temperature_2m,cloud_cover")
		q.Set("tilt", fmt.Sprintf("%.0f", a.TiltDeg))
		q.Set("azimuth", fmt.Sprintf("%.0f", omAzimuth(a.AzDeg)))
		q.Set("timezone", tz)
		q.Set("forecast_days", "3")
		req, _ := http.NewRequest(http.MethodGet, "https://api.open-meteo.com/v1/forecast?"+q.Encode(), nil)
		req.Header.Set("User-Agent", "ha-energy-schema/solar")
		resp, err := httpc.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("open-meteo status %d", resp.StatusCode)
		}
		var r omResp
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		gtiByArray[i] = r.Hourly.GTI
		if i == 0 {
			times, temp, cloud = r.Hourly.Time, r.Hourly.Temp, r.Hourly.Cloud
		}
	}
	out := make([]OMHour, 0, len(times))
	for j := range times {
		lbl, err := time.ParseInLocation("2006-01-02T15:04", times[j], loct)
		if err != nil {
			continue
		}
		h := OMHour{HourStart: lbl.Add(-time.Hour), GTI: make([]float64, len(arrays))}
		for i := range arrays {
			if j < len(gtiByArray[i]) && gtiByArray[i][j] != nil {
				h.GTI[i] = math.Max(0, *gtiByArray[i][j])
			}
		}
		if j < len(temp) && temp[j] != nil {
			h.TempC = *temp[j]
		}
		if j < len(cloud) && cloud[j] != nil {
			h.CloudPct = *cloud[j]
		}
		out = append(out, h)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].HourStart.Before(out[b].HourStart) })
	return out, nil
}
