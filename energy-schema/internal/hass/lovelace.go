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

// LovelaceResource — ресурс дашбордов (JS-модуль карточки), `lovelace/resources`.
type LovelaceResource struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

// LovelaceResources lists the dashboard resources.
func (c *Client) LovelaceResources() ([]LovelaceResource, error) {
	w, err := c.wsAuth()
	if err != nil {
		return nil, err
	}
	defer w.close()
	var resp struct {
		Result []LovelaceResource `json:"result"`
	}
	if err = w.call(1, map[string]interface{}{"type": "lovelace/resources"}, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// SaveLovelaceResource creates a JS module resource (id == "") or points an
// existing one at a new url (cache-busting version).
func (c *Client) SaveLovelaceResource(id, url string) error {
	w, err := c.wsAuth()
	if err != nil {
		return err
	}
	defer w.close()
	msg := map[string]interface{}{"type": "lovelace/resources/create", "res_type": "module", "url": url}
	if id != "" {
		msg["type"], msg["resource_id"] = "lovelace/resources/update", id
	}
	return w.call(1, msg, nil)
}

// AuthUser — пользователь HA из `config/auth/list` (только для админ-токена).
type AuthUser struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Username        string `json:"username"`
	SystemGenerated bool   `json:"system_generated"`
}

// AuthUsers lists HA users via WebSocket `config/auth/list`.
func (c *Client) AuthUsers() ([]AuthUser, error) {
	w, err := c.wsAuth()
	if err != nil {
		return nil, err
	}
	defer w.close()
	var resp struct {
		Result []AuthUser `json:"result"`
	}
	if err = w.call(1, map[string]interface{}{"type": "config/auth/list"}, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}
