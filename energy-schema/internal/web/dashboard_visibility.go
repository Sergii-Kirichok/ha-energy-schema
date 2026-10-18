package web

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Карточки дашборда с органами управления (хелперы регулятора, переключатели)
// видны только пользователям из control_users — через условие видимости HA
// `{condition: user, users: [id…]}`. Хелперы периода графиков — для всех.
// Пустой control_users — условие снимается (всё видно всем, как раньше).
var controlDomains = map[string]bool{"input_boolean": true, "input_number": true, "input_button": true,
	"input_text": true, "input_select": true, "switch": true, "number": true, "button": true, "select": true}

var viewOnlyHelpers = map[string]bool{"input_select.diapazon_grafika": true, "input_boolean.sglazhivanie_grafikov": true}

func isControl(id string) bool {
	d, _, ok := strings.Cut(id, ".")
	return ok && controlDomains[d] && !viewOnlyHelpers[id]
}

// hasControl: в карточке есть сущность-орган управления (ключи entity /
// entity_id / entities; условия видимости и conditions — не управление).
func hasControl(n any, idKey bool) bool {
	switch v := n.(type) {
	case string:
		return idKey && isControl(v)
	case []any:
		for _, c := range v {
			if hasControl(c, idKey) {
				return true
			}
		}
	case map[string]any:
		for k, c := range v {
			if k != "conditions" && k != "visibility" && hasControl(c, k == "entity" || k == "entity_id" || k == "entities") {
				return true
			}
		}
	}
	return false
}

// setUserVisibility leaves exactly one user condition with ids (none when ids
// is empty), keeping the card's other conditions. Reports a change.
func setUserVisibility(card map[string]any, ids []string) bool {
	before, _ := json.Marshal(card["visibility"])
	vis, _ := card["visibility"].([]any)
	kept := []any{}
	for _, c := range vis {
		if cm, ok := c.(map[string]any); ok && cm["condition"] == "user" {
			continue
		}
		kept = append(kept, c)
	}
	if len(ids) > 0 {
		users := make([]any, len(ids))
		for i, id := range ids {
			users[i] = id
		}
		kept = append(kept, map[string]any{"condition": "user", "users": users})
	}
	if len(kept) == 0 {
		delete(card, "visibility")
	} else {
		card["visibility"] = kept
	}
	after, _ := json.Marshal(card["visibility"])
	return string(before) != string(after)
}

// patchControlVisibility sets the user condition on every top-level card
// (view cards and section cards) that holds a control.
func patchControlVisibility(node any, ids []string) bool {
	cfg, _ := node.(map[string]any)
	views, _ := cfg["views"].([]any)
	changed := false
	for _, v := range views {
		vm, _ := v.(map[string]any)
		lists := []any{vm["cards"]}
		secs, _ := vm["sections"].([]any)
		for _, s := range secs {
			if sm, ok := s.(map[string]any); ok {
				lists = append(lists, sm["cards"])
			}
		}
		for _, l := range lists {
			cards, _ := l.([]any)
			for _, c := range cards {
				if cm, ok := c.(map[string]any); ok && hasControl(cm, false) && setUserVisibility(cm, ids) {
					changed = true
				}
			}
		}
	}
	return changed
}

// controlUserIDs maps control_users to HA user ids the way userAllowed
// matches ingress users: display name (username when it is empty) or id.
func (s *Server) controlUserIDs() ([]string, error) {
	if len(s.cfg.ControlUsers) == 0 {
		return nil, nil
	}
	users, err := s.client.AuthUsers()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, want := range s.cfg.ControlUsers {
		want = strings.TrimSpace(want)
		for _, u := range users {
			name := u.Name
			if name == "" {
				name = u.Username
			}
			if want != "" && !u.SystemGenerated && (strings.EqualFold(want, name) || want == u.ID) {
				ids = append(ids, u.ID)
			}
		}
	}
	if len(ids) == 0 {
		// не прятать управление от всех из-за опечатки в имени
		return nil, fmt.Errorf("control_users %v match no HA user", s.cfg.ControlUsers)
	}
	return ids, nil
}

func (s *Server) patchControlVisibility(node any) (bool, error) {
	ids, err := s.controlUserIDs()
	if err != nil {
		return false, err
	}
	return patchControlVisibility(node, ids), nil
}
