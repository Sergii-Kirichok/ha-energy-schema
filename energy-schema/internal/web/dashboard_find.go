package web

import (
	"fmt"
	"strings"
)

// findCardsHolding returns [parentMap, key] of the []any that contains target.
func findCardsHolding(node any, target map[string]any) []any {
	m, ok := node.(map[string]any)
	if !ok {
		if l, ok := node.([]any); ok {
			for _, c := range l {
				if r := findCardsHolding(c, target); r != nil {
					return r
				}
			}
		}
		return nil
	}
	for k, v := range m {
		if l, ok := v.([]any); ok {
			for _, c := range l {
				if cm, ok := c.(map[string]any); ok && sameMap(cm, target) {
					return []any{m, k}
				}
			}
		}
		if r := findCardsHolding(v, target); r != nil {
			return r
		}
	}
	return nil
}

func sameMap(a, b map[string]any) bool { return fmt.Sprintf("%p", a) == fmt.Sprintf("%p", b) }

func entityID(e any) string {
	switch v := e.(type) {
	case string:
		return v
	case map[string]any:
		id, _ := v["entity"].(string)
		return id
	}
	return ""
}

func findEntitiesCard(node any) map[string]any {
	if c := findCardWith(node, dashAnchorEntity); c != nil {
		return c
	}
	return findCardWith(node, bmsSOHEntity)
}

// findCardWith returns the first entities card whose rows include entity.
func findCardWith(node any, entity string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == "entities" {
			if ents, ok := v["entities"].([]any); ok {
				for _, e := range ents {
					if entityID(e) == entity {
						return v
					}
				}
			}
		}
		for _, child := range v {
			if c := findCardWith(child, entity); c != nil {
				return c
			}
		}
	case []any:
		for _, child := range v {
			if c := findCardWith(child, entity); c != nil {
				return c
			}
		}
	}
	return nil
}

// findOwnEntityCard returns the first card of the given type whose own
// `entity` is entity (gauge, tile, ...).
func findOwnEntityCard(node any, typ, entity string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == typ && v["entity"] == entity {
			return v
		}
		for _, child := range v {
			if c := findOwnEntityCard(child, typ, entity); c != nil {
				return c
			}
		}
	case []any:
		for _, child := range v {
			if c := findOwnEntityCard(child, typ, entity); c != nil {
				return c
			}
		}
	}
	return nil
}

// removeOwnEntityCard deletes every card of type typ whose own entity is
// entity from any cards list; reports whether something was removed.
func removeOwnEntityCard(node any, typ, entity string) bool {
	removed := false
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if l, ok := child.([]any); ok {
				kept := l[:0:0]
				for _, c := range l {
					if cm, ok := c.(map[string]any); ok && cm["type"] == typ && cm["entity"] == entity {
						removed = true
						continue
					}
					kept = append(kept, c)
				}
				v[k] = kept
				child = kept
			}
			if removeOwnEntityCard(child, typ, entity) {
				removed = true
			}
		}
	case []any:
		for _, c := range v {
			if removeOwnEntityCard(c, typ, entity) {
				removed = true
			}
		}
	}
	return removed
}

// findMarkdownWith returns the first markdown card whose content mentions substr.
func findMarkdownWith(node any, substr string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == "markdown" {
			if c, _ := v["content"].(string); strings.Contains(c, substr) {
				return v
			}
		}
		for _, child := range v {
			if c := findMarkdownWith(child, substr); c != nil {
				return c
			}
		}
	case []any:
		for _, child := range v {
			if c := findMarkdownWith(child, substr); c != nil {
				return c
			}
		}
	}
	return nil
}

// findOwnTypeCard returns the first card whose type is typ.
func findOwnTypeCard(node any, typ string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == typ {
			return v
		}
		for _, child := range v {
			if c := findOwnTypeCard(child, typ); c != nil {
				return c
			}
		}
	case []any:
		for _, child := range v {
			if c := findOwnTypeCard(child, typ); c != nil {
				return c
			}
		}
	}
	return nil
}
