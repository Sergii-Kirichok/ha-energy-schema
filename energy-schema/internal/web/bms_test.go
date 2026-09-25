package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"energy-schema/internal/hass"
)

func TestCellVerdict(t *testing.T) {
	cases := []struct {
		max, min int
		level    string
	}{
		{3294, 3262, "warn"}, // Δ32 — балансируется
		{3280, 3262, "ok"},   // Δ18 — выровнены
		{3400, 3300, "bad"},  // Δ100 — разбаланс
		{3460, 3440, "warn"}, // верхняя ячейка высоко
		{3560, 3540, "bad"},  // у отсечки
	}
	for _, c := range cases {
		if lvl, _ := cellVerdict(c.max, c.min); lvl != c.level {
			t.Errorf("cellVerdict(%d,%d) = %s, want %s", c.max, c.min, lvl, c.level)
		}
	}
}

func TestPublishBMS(t *testing.T) {
	posted := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		posted[strings.TrimPrefix(r.URL.Path, "/api/states/")] = string(b)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	s := &Server{client: hass.NewClient(srv.URL+"/api", "T"), store: hass.NewStore()}

	regs := map[int]int{regSOH: 97, regCycles: 173, regCellMax: 3294, regCellMaxN: 7, regCellMin: 3262, regCellMinN: 12}
	line, err := s.publishBMS(regs)
	if err != nil {
		t.Fatalf("publishBMS: %v", err)
	}
	if len(posted) != 6 || !strings.Contains(posted["sensor.energy_schema_bms_cell_delta"], `"state":"32"`) ||
		!strings.Contains(posted["sensor.energy_schema_bms_cell_max"], `"state":"3.294"`) {
		t.Errorf("posted = %v", posted)
	}
	if s.store.Num(bmsSOHEntity) != 97 || !strings.Contains(line, "Δ32") {
		t.Errorf("virtual soh=%v line=%q", s.store.State(bmsSOHEntity), line)
	}
	// garbage block (BMS offline → zeros) must be rejected, nothing posted
	posted = map[string]string{}
	if _, err := s.publishBMS(map[int]int{regSOH: 0}); err == nil || len(posted) != 0 {
		t.Errorf("expected rejection, err=%v posted=%d", err, len(posted))
	}
}
