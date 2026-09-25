package hass

import "encoding/json"

// LovelaceConfig returns the storage-mode dashboard config for urlPath
// (e.g. "home-energy") as raw JSON, via WebSocket `lovelace/config`.
func (c *Client) LovelaceConfig(urlPath string) (json.RawMessage, error) {
	w, err := c.wsAuth()
	if err != nil {
		return nil, err
	}
	defer w.close()
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err = w.call(1, map[string]interface{}{"type": "lovelace/config", "url_path": urlPath}, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// SaveLovelaceConfig writes the whole dashboard config back (`lovelace/config/save`).
// HA persists it to .storage and pushes the update to open frontends.
func (c *Client) SaveLovelaceConfig(urlPath string, cfg json.RawMessage) error {
	w, err := c.wsAuth()
	if err != nil {
		return err
	}
	defer w.close()
	return w.call(1, map[string]interface{}{"type": "lovelace/config/save", "url_path": urlPath, "config": cfg}, nil)
}
