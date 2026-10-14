package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"energy-schema/internal/hass"
)

// «Сохранить» с оборота карточки: проверка до записи, числа в хелперы и сразу в
// применённые настройки регулятора, тумблеры — turn_on/turn_off.
func TestQuickSave(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.URL.Path+" "+string(b))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	s := &Server{client: hass.NewClient(srv.URL+"/api", "T"), store: hass.NewStore(),
		applied: loadApplied(filepath.Join(t.TempDir(), "a.json"))}
	s.store.ReplaceStates(map[string]string{"input_number.energy_schema_charge_max_a": "30",
		"input_number.energy_schema_charge_grid_a": "4", "input_number.energy_schema_charge_target_soc": "80",
		"input_number.energy_schema_charge_full_days": "16"})
	s.applied.apply(s.store.Num)
	for _, bad := range []string{"max_a=99", "auto=2", "nope=1", "max_a=abc", "max_a"} {
		calls = nil
		if _, err := s.quickSave(bad); err == nil || len(calls) != 0 {
			t.Errorf("%q: err=%v calls=%v", bad, err, calls)
		}
	}
	calls = nil
	if _, err := s.quickSave("max_a=4,auto=0,target_soc=85"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || !strings.Contains(calls[0], `"value":4`) || !strings.Contains(calls[1], "/input_boolean/turn_off") {
		t.Errorf("calls = %v", calls)
	}
	if got := s.applied.num("input_number.energy_schema_charge_max_a", s.store.Num); got != 4 {
		t.Errorf("max_a must be applied at once, got %v", got)
	}
	if got := s.applied.num("input_number.energy_schema_charge_target_soc", s.store.Num); got != 85 {
		t.Errorf("target must be applied at once, got %v", got)
	}
}

// Черновик (ползунки HA) не действует, пока не применён.
func TestChargeAppliedDraft(t *testing.T) {
	st := hass.NewStore()
	st.ReplaceStates(map[string]string{"input_number.energy_schema_charge_max_a": "30"})
	a := loadApplied(filepath.Join(t.TempDir(), "a.json"))
	if !a.empty() || a.num("input_number.energy_schema_charge_max_a", st.Num) != 30 {
		t.Fatal("before first apply the draft is used")
	}
	a.apply(st.Num)
	st.ReplaceStates(map[string]string{"input_number.energy_schema_charge_max_a": "5"})
	if a.num("input_number.energy_schema_charge_max_a", st.Num) != 30 || len(a.pending(st.Num)) != 1 {
		t.Error("draft change must not apply by itself")
	}
	if ch := a.apply(st.Num); len(ch) != 1 || a.num("input_number.energy_schema_charge_max_a", st.Num) != 5 {
		t.Errorf("apply: %v", ch)
	}
}
