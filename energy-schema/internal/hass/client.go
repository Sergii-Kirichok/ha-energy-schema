package hass

import (
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
