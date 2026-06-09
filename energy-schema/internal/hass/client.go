package hass

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// FetchStates GETs /states and returns an entity_id -> state map.
func (c *Client) FetchStates() (map[string]string, error) {
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
		EntityID string `json:"entity_id"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("decode states (status=%d tokenlen=%d): %w", resp.StatusCode, len(c.Token), err)
	}
	m := make(map[string]string, len(arr))
	for _, e := range arr {
		m[e.EntityID] = e.State
	}
	return m, nil
}
