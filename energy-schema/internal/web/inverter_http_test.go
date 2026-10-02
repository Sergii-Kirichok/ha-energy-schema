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

// fakeInverter — HA с интеграцией Solarman: регистры в памяти, read/write
// через сервисы; refuse — регистр, который инвертор «не принимает».
type fakeInverter struct {
	mu     sync.Mutex
	regs   map[int]int
	writes []int
	refuse int
}

func (f *fakeInverter) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var in map[string]float64
	_ = json.Unmarshal(b, &in)
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/template"):
		_, _ = w.Write([]byte("dev1"))
	case strings.Contains(r.URL.Path, "read_holding_registers"):
		out := map[string]int{}
		for a := int(in["address"]); a < int(in["address"]+in["count"]); a++ {
			out[itoa(a)] = f.regs[a]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"service_response": out})
	case strings.Contains(r.URL.Path, "write_holding_register"):
		reg := int(in["register"])
		f.writes = append(f.writes, reg)
		if reg != f.refuse {
			f.regs[reg] = int(in["value"])
		}
		_, _ = w.Write([]byte("[]"))
	default:
		http.NotFound(w, r)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func newInvServer(t *testing.T, f *fakeInverter) *Server {
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return &Server{client: hass.NewClient(srv.URL+"/api", "T"), store: hass.NewStore(), ovr: loadOverrides(filepath.Join(t.TempDir(), "o.json"))}
}

func liveRegs() map[int]int {
	r := map[int]int{104: 15, 108: 10, 109: 50, 110: 1, 115: 25, 116: 35, 117: 30, 128: 3, 129: 0, 130: 0,
		141: 1, 142: 1, 146: 255, 180: 70, 185: 2650, 186: 1500, 187: 5150, 188: 4800}
	for i, t := range []int{100, 500, 900, 1400, 1600, 2100} {
		r[148+i], r[166+i], r[172+i] = t, 30+5*i, 1
	}
	return r
}

func save(t *testing.T, s *Server, body string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, "/inverter/save", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.inverterSave(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestInverterSave(t *testing.T) {
	f := &fakeInverter{regs: liveRegs(), refuse: 104}
	s := newInvServer(t, f)
	// v_high меняется, max_charge_a в «авто» — не должен писаться, zero_export «не принимается»
	code, out := save(t, s, `{"values":{"v_high":260,"max_charge_a":30,"zero_export_w":200},"manual":{}}`)
	if code != 200 {
		t.Fatalf("code %d %v", code, out)
	}
	if f.regs[185] != 2600 || f.regs[108] != 10 {
		t.Errorf("regs after save: 185=%d (want 2600), 108=%d (auto, must stay 10)", f.regs[185], f.regs[108])
	}
	res := out["results"].(map[string]any)
	if res["v_high"].(map[string]any)["ok"] != true || res["zero_export_w"].(map[string]any)["ok"] != false {
		t.Errorf("results = %v", res)
	}
	if out["ok"] != false {
		t.Error("overall ok must be false when one field mismatched")
	}
	// ручной режим разрешает запись поля регулятора и сохраняется
	f.writes = nil
	if code, _ := save(t, s, `{"values":{"max_charge_a":30},"manual":{"max_charge":true}}`); code != 200 || f.regs[108] != 15 {
		t.Errorf("manual write: code %d, reg108=%d (want 15 = 30 A / 2 channels)", code, f.regs[108])
	}
	if !s.ovr.get("max_charge") {
		t.Error("manual flag not persisted")
	}
	// нижний порог выше верхнего — отказ без записи
	f.writes = nil
	if code, _ := save(t, s, `{"values":{"v_low":270},"manual":{}}`); code != 400 || len(f.writes) != 0 {
		t.Errorf("invalid pair: code %d, writes %v", code, f.writes)
	}
	if code, _ := save(t, s, `{"values":{"nope":1},"manual":{}}`); code != 400 {
		t.Errorf("unknown field must be 400, got %d", code)
	}
}

func TestInverterSaveRequiresPost(t *testing.T) {
	s := newInvServer(t, &fakeInverter{regs: liveRegs()})
	w := httptest.NewRecorder()
	s.inverterSave(w, httptest.NewRequest(http.MethodGet, "/inverter/save", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET must be 405, got %d", w.Code)
	}
}
