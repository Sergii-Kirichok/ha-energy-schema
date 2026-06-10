package hass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client reads entity states from the Home Assistant REST API.
type Client struct {
	APIBase string
	Token   string
	HTTP    *http.Client
}

// NewClient builds a Client with a sane default timeout.
func NewClient(apiBase, token string) *Client {
	return &Client{
		APIBase: apiBase,
		Token:   token,
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// FetchStates GETs /states and returns an entity_id -> Entity map (state +
// last_changed, so the store can track how long a device has been offline).
func (c *Client) FetchStates() (map[string]Entity, error) {
	req, err := http.NewRequest(http.MethodGet, c.APIBase+"/states", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var arr []struct {
		EntityID    string                     `json:"entity_id"`
		State       string                     `json:"state"`
		LastChanged string                     `json:"last_changed"`
		Attributes  map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("decode states (status=%d tokenlen=%d): %w", resp.StatusCode, len(c.Token), err)
	}
	m := make(map[string]Entity, len(arr))
	for _, e := range arr {
		lc, _ := time.Parse(time.RFC3339Nano, e.LastChanged)
		var attrs map[string]string
		for k, raw := range e.Attributes {
			if v, ok := scalarStr(raw); ok {
				if attrs == nil {
					attrs = map[string]string{}
				}
				attrs[k] = v
			}
		}
		m[e.EntityID] = Entity{State: e.State, LastChanged: lc, Attrs: attrs}
	}
	return m, nil
}

// ForecastDay is one day of the HA daily weather forecast.
type ForecastDay struct {
	Time      time.Time
	Condition string
	Cloud     float64 // cloud coverage, %
}

// DailyForecast calls the weather.get_forecasts service and returns the daily
// forecast for the entity (HA 2024.3+: forecasts are not state attributes).
func (c *Client) DailyForecast(entity string) ([]ForecastDay, error) {
	body, _ := json.Marshal(map[string]string{"entity_id": entity, "type": "daily"})
	req, err := http.NewRequest(http.MethodPost, c.APIBase+"/services/weather/get_forecasts?return_response", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		ServiceResponse map[string]struct {
			Forecast []struct {
				Datetime      string  `json:"datetime"`
				Condition     string  `json:"condition"`
				CloudCoverage float64 `json:"cloud_coverage"`
			} `json:"forecast"`
		} `json:"service_response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode forecast (status=%d): %w", resp.StatusCode, err)
	}
	fc, ok := out.ServiceResponse[entity]
	if !ok {
		return nil, fmt.Errorf("no forecast for %s (status=%d)", entity, resp.StatusCode)
	}
	days := make([]ForecastDay, 0, len(fc.Forecast))
	for _, f := range fc.Forecast {
		t, _ := time.Parse(time.RFC3339, f.Datetime)
		days = append(days, ForecastDay{Time: t, Condition: f.Condition, Cloud: f.CloudCoverage})
	}
	return days, nil
}

// scalarStr stringifies a scalar JSON attribute (string/number/bool); it skips
// arrays/objects (e.g. weather forecast lists) and null.
func scalarStr(raw json.RawMessage) (string, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "", false
	}
	switch s[0] {
	case '[', '{':
		return "", false
	case '"':
		var str string
		if json.Unmarshal(raw, &str) == nil {
			return str, true
		}
		return "", false
	default:
		return s, true // number / bool verbatim
	}
}
