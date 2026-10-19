package hass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CallService invokes domain.service with data (POST /api/services/<domain>/<service>).
func (c *Client) CallService(domain, service string, data map[string]any) error {
	body, _ := json.Marshal(data)
	req, err := http.NewRequest(http.MethodPost, c.APIBase+"/services/"+domain+"/"+service, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var out any
	return c.doJSON(domain+"."+service, req, &out)
}

// HasEntity reports whether HA currently has a state for entity.
func (c *Client) HasEntity(entity string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, c.APIBase+"/states/"+entity, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case 200:
		return true, nil
	case 404:
		return false, nil
	}
	return false, fmt.Errorf("states/%s: status=%d", entity, resp.StatusCode)
}

// Helper describes an input_number / input_boolean to create in HA. ID is the
// ascii object id (entity becomes <domain>.<ID>); Name is the friendly name
// applied afterwards through the entity registry, so Cyrillic names don't
// change the entity id.
type Helper struct {
	Domain  string // "input_number" | "input_boolean"
	ID      string
	Name    string
	Min     float64
	Max     float64
	Step    float64
	Initial float64
	Unit    string
	Icon    string
}

// EnsureHelper creates the helper if its entity does not exist yet, then sets
// the friendly name via the entity registry. Existing helpers are left as-is
// (user values win). Returns true when it created something.
func (c *Client) EnsureHelper(h Helper) (bool, error) {
	entity := h.Domain + "." + h.ID
	if ok, err := c.HasEntity(entity); err != nil || ok {
		return false, err
	}
	w, err := c.wsAuth()
	if err != nil {
		return false, err
	}
	defer w.close()
	msg := map[string]interface{}{"type": h.Domain + "/create", "name": h.ID, "icon": h.Icon}
	switch h.Domain {
	case "input_number":
		msg["min"], msg["max"], msg["step"], msg["initial"], msg["mode"] = h.Min, h.Max, h.Step, h.Initial, inputNumberMode
		if h.Unit != "" {
			msg["unit_of_measurement"] = h.Unit
		}
	}
	// input_boolean — без initial: с ним HA при каждом запуске ставит это значение
	// вместо сохранённого (19.10: перезапуск HA выключил балансировку)
	if err := w.call(1, msg, nil); err != nil {
		return false, fmt.Errorf("create %s: %w", entity, err)
	}
	// HA slugifies the name into the object id; our ids are already slugs.
	if err := w.call(2, map[string]interface{}{"type": "config/entity_registry/update",
		"entity_id": entity, "name": strings.TrimSpace(h.Name)}, nil); err != nil {
		return true, fmt.Errorf("rename %s: %w", entity, err)
	}
	if h.Domain == "input_boolean" && h.Initial != 0 {
		if err := c.CallService("input_boolean", "turn_on", map[string]any{"entity_id": entity}); err != nil {
			return true, fmt.Errorf("default on %s: %w", entity, err)
		}
	}
	return true, nil
}

// inputNumberMode — режим отображения input_number: "slider" — строка обычной
// высоты (у "box" поле ввода высокое и в entities-карточке не ужимается).
const inputNumberMode = "box"

// EnsureNumberMode switches an existing input_number helper (object id `id`)
// to inputNumberMode if its current mode differs. HA validates input_number/update
// against the full schema, so all fields are sent (name = object id, as created).
func (c *Client) EnsureNumberMode(h Helper, current string) (bool, error) {
	if current == inputNumberMode {
		return false, nil
	}
	w, err := c.wsAuth()
	if err != nil {
		return false, err
	}
	defer w.close()
	msg := map[string]interface{}{"type": "input_number/update", "input_number_id": h.ID, "name": h.ID,
		"min": h.Min, "max": h.Max, "step": h.Step, "mode": inputNumberMode, "icon": h.Icon}
	if h.Unit != "" {
		msg["unit_of_measurement"] = h.Unit
	}
	if err := w.call(1, msg, nil); err != nil {
		return false, fmt.Errorf("mode %s: %w", h.ID, err)
	}
	return true, nil
}

// DropBoolInitial снимает поле initial у наших input_boolean (ids — object id):
// с ним HA при каждом запуске ставит initial вместо сохранённого значения.
// input_boolean/update заменяет запись целиком, поэтому отправляем имя и
// иконку как есть, без initial. Возвращает число исправленных.
func (c *Client) DropBoolInitial(ids []string) (int, error) {
	w, err := c.wsAuth()
	if err != nil {
		return 0, err
	}
	defer w.close()
	var resp struct {
		Result []map[string]any `json:"result"`
	}
	if err := w.call(1, map[string]interface{}{"type": "input_boolean/list"}, &resp); err != nil {
		return 0, fmt.Errorf("input_boolean/list: %w", err)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	n, seq := 0, 2
	for _, it := range resp.Result {
		id, _ := it["id"].(string)
		if _, has := it["initial"]; !has || !want[id] {
			continue
		}
		msg := map[string]interface{}{"type": "input_boolean/update", "input_boolean_id": id, "name": it["name"]}
		if ic, ok := it["icon"].(string); ok && ic != "" {
			msg["icon"] = ic
		}
		if err := w.call(seq, msg, nil); err != nil {
			return n, fmt.Errorf("update %s: %w", id, err)
		}
		seq++
		n++
	}
	return n, nil
}
