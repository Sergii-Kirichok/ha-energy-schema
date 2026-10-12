package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"energy-schema/internal/hass"
)

// haCall — один запрос к фейковому HA: путь после /api и разобранное тело.
type haCall struct {
	path string
	body map[string]any
}

// haFake — HA для регулятора: регистры Solarman на чтение, все сервисы и
// POST /api/states записываются. fail — подстрока пути, на которую отвечаем 500.
type haFake struct {
	mu    sync.Mutex
	regs  map[int]int
	calls []haCall
	fail  string
}

func (f *haFake) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	path := strings.TrimPrefix(r.URL.Path, "/api")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != "" && strings.Contains(path, f.fail) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		return
	}
	switch {
	case path == "/template":
		_, _ = w.Write([]byte("dev1"))
	case strings.Contains(path, "read_holding_registers"):
		var in struct{ Address, Count int }
		_ = json.Unmarshal(b, &in)
		out := map[string]int{}
		for a := in.Address; a < in.Address+in.Count; a++ {
			out[itoa(a)] = f.regs[a]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"service_response": out})
	case strings.HasPrefix(path, "/services/"), strings.HasPrefix(path, "/states/"):
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		f.calls = append(f.calls, haCall{path, body})
		_, _ = w.Write([]byte("{}"))
	default:
		http.NotFound(w, r)
	}
}

// find — все вызовы с этим путём (например "/services/number/set_value").
func (f *haFake) find(path string) []haCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []haCall
	for _, c := range f.calls {
		if c.path == path {
			out = append(out, c)
		}
	}
	return out
}

// setValues — value записей number.set_value по сущности.
func (f *haFake) setValues(entity string) []float64 {
	var out []float64
	for _, c := range f.find("/services/number/set_value") {
		if c.body["entity_id"] == entity {
			out = append(out, c.body["value"].(float64))
		}
	}
	return out
}

// state — последний опубликованный state сущности и его атрибуты.
func (f *haFake) state(entity string) (string, map[string]any, bool) {
	cs := f.find("/states/" + entity)
	if len(cs) == 0 {
		return "", nil, false
	}
	c := cs[len(cs)-1]
	attrs, _ := c.body["attributes"].(map[string]any)
	s, _ := c.body["state"].(string)
	return s, attrs, true
}

func (f *haFake) reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func newHAFakeServer(t *testing.T, f *haFake) *Server {
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	return &Server{client: hass.NewClient(srv.URL+"/api", "T"), store: hass.NewStore(),
		ovr: loadOverrides(filepath.Join(dir, "o.json")), cells: loadCellStats(filepath.Join(dir, "cs.json"))}
}
