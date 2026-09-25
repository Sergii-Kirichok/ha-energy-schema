package hass

// Solarman-service and state-push helpers (kept apart from the core REST client).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ReadHoldingRegisters reads `count` Modbus holding registers starting at
// addr from the Solarman-integration device that owns deviceEntity, via the
// solarman.read_holding_registers service (return_response). Used for values
// the integration profile does not expose (e.g. Deye BMS SOH, register 10006).
func (c *Client) ReadHoldingRegisters(deviceEntity string, addr, count int) (map[int]int, error) {
	tpl, _ := json.Marshal(map[string]string{"template": "{{ device_id('" + deviceEntity + "') }}"})
	req, err := http.NewRequest(http.MethodPost, c.APIBase+"/template", bytes.NewReader(tpl))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// /api/template answers with plain text, not JSON
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	devB, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	dev := strings.TrimSpace(string(devB))
	if resp.StatusCode != 200 || dev == "" || dev == "None" {
		return nil, fmt.Errorf("device_id(%s): status=%d body=%q", deviceEntity, resp.StatusCode, dev)
	}
	body, _ := json.Marshal(map[string]any{"device": dev, "address": addr, "count": count})
	req, err = http.NewRequest(http.MethodPost, c.APIBase+"/services/solarman/read_holding_registers?return_response", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		ServiceResponse map[string]int `json:"service_response"`
	}
	if err := c.doJSON("read_holding_registers", req, &out); err != nil {
		return nil, err
	}
	regs := make(map[int]int, len(out.ServiceResponse))
	for k, v := range out.ServiceResponse {
		if n, err := strconv.Atoi(k); err == nil {
			regs[n] = v
		}
	}
	if len(regs) == 0 {
		return nil, fmt.Errorf("read_holding_registers %d+%d: empty service_response", addr, count)
	}
	return regs, nil
}

// SetState creates/updates an HA entity state via POST /api/states/<id>.
// Such entities are not in the entity registry (no UI rename) but show up in
// dashboards/history and live until HA restarts — the caller re-posts periodically.
func (c *Client) SetState(entity, state string, attrs map[string]any) error {
	body, _ := json.Marshal(map[string]any{"state": state, "attributes": attrs})
	req, err := http.NewRequest(http.MethodPost, c.APIBase+"/states/"+entity, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doJSON("set state "+entity, req, &out)
}
