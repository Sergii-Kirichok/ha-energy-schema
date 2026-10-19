package web

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"energy-schema/internal/hass"
)

var imbT0 = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

// smp — выборка на sec-й секунде от imbT0.
func smp(sec int, maxV, minV, a float64) imbSample {
	return imbSample{T: imbT0.Add(time.Duration(sec) * time.Second), Max: maxV, Min: minV, I: a, SOC: 95,
		Limit: math.NaN(), MaxLoc: "М12·№12", MinLoc: "М3·№7"}
}

func feedAll(tr *imbTracker, ss ...imbSample) []*imbRecord {
	var out []*imbRecord
	for _, s := range ss {
		if r := tr.feed(s); r != nil {
			out = append(out, r)
		}
	}
	return out
}

// с плато, нижняя отстаёт: 36 А = 0,6 А·ч в минуту
func fromFlat() []imbSample {
	return []imbSample{smp(0, 3.35, 3.33, -36), smp(60, 3.36, 3.34, -36), smp(120, 3.37, 3.35, -36),
		smp(180, 3.38, 3.36, -36), smp(240, 3.40, 3.37, -36), smp(300, 3.45, 3.376, -36),
		smp(360, 3.50, 3.39, -36), smp(420, 3.55, 3.40, -36)}
}

func TestImbFromFlatMinLagging(t *testing.T) {
	recs := feedAll(&imbTracker{source: "live"}, fromFlat()...)
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	r := recs[0]
	if r.Type != imbFromFlat || r.EndReason != "top" || r.StartMaxV != 3.35 || r.Source != "live" || r.Balancing {
		t.Errorf("record = %+v", r)
	}
	if len(r.Levels) != 4 || r.Levels[3].V != 3.55 || r.Levels[3].DeltaMV != 150 || r.Levels[0].DeltaMV != 30 ||
		r.Levels[3].MaxLoc != "М12·№12" || r.Levels[0].CurrentA != -36 {
		t.Errorf("levels = %+v", r.Levels)
	}
	want := []struct {
		v       float64
		ah      float64
		reached bool
	}{{3.375, 1.2, true}, {3.40, 1.8, true}, {3.45, 1.2, false}}
	if len(r.AhLag) != 3 {
		t.Fatalf("lags = %+v", r.AhLag)
	}
	for i, w := range want {
		if g := r.AhLag[i]; g.V != w.v || g.Ah != w.ah || g.Reached != w.reached {
			t.Errorf("lag %d = %+v, want %+v", i, g, w)
		}
	}
	if got := imbState(r); got != "3,55 В: 150 мВ · 1,8 А·ч · с плато" {
		t.Errorf("state = %q", got)
	}
}

func TestImbAfterPauseAndTopped(t *testing.T) {
	tr := &imbTracker{}
	s := []imbSample{smp(0, 3.42, 3.30, -10), smp(60, 3.45, 3.31, -10), smp(120, 3.50, 3.32, -10), smp(180, 3.55, 3.33, -10)}
	s[0].Phase = balCharge
	recs := feedAll(tr, s...)
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	r := recs[0]
	if r.Type != imbAfterRest || !r.Balancing || len(r.Levels) != 3 || r.Levels[0].V != 3.45 {
		t.Errorf("record = %+v", r)
	}
	if len(r.AhLag) != 1 || r.AhLag[0].V != 3.45 || r.AhLag[0].Reached || r.AhLag[0].Ah != 0.33 {
		t.Errorf("lags = %+v", r.AhLag)
	}
	if got := imbState(r); got != "3,55 В: 220 мВ · ≥0,3 А·ч · после паузы" {
		t.Errorf("state = %q", got)
	}
	// на верху заряд ещё идёт — нового подъёма нет; после остановки — снова
	if recs := feedAll(tr, smp(200, 3.55, 3.33, -3), smp(260, 3.50, 3.33, 0)); len(recs) != 0 || tr.r != nil {
		t.Fatalf("topped rise restarted: %v %+v", recs, tr.r)
	}
	if tr.feed(smp(900, 3.44, 3.34, -10)); tr.r == nil || tr.r.Type != imbAfterRest {
		t.Errorf("new rise after pause = %+v", tr.r)
	}
}

func TestImbStoppedBelowTop(t *testing.T) {
	tr := &imbTracker{}
	recs := feedAll(tr, smp(0, 3.38, 3.36, -20), smp(60, 3.40, 3.37, -20), smp(120, 3.47, 3.38, -20),
		smp(180, 3.46, 3.38, 0), smp(400, 3.44, 3.38, 0), smp(500, 3.43, 3.38, 0))
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	r := recs[0]
	if r.EndReason != "stopped" || !r.End.Equal(imbT0.Add(120*time.Second)) || len(r.Levels) != 2 || r.PeakMaxV != 3.47 {
		t.Errorf("record = %+v", r)
	}
	// 3,40: ток держится до следующей выборки — 60 → 180 с при 20 А = 0,67; дальше 0
	if len(r.AhLag) != 2 || r.AhLag[0].V != 3.40 || r.AhLag[0].Ah != 0.67 || r.AhLag[0].Reached {
		t.Errorf("lags = %+v", r.AhLag)
	}
	// не дошёл до 3,40 — записи нет
	if recs := feedAll(&imbTracker{}, smp(0, 3.35, 3.33, -20), smp(60, 3.39, 3.35, -20), smp(500, 3.38, 3.35, 0)); len(recs) != 0 {
		t.Errorf("below 3.40 must not record: %+v", recs[0])
	}
}

func TestImbShortGapAndBMSLimit(t *testing.T) {
	tr := &imbTracker{}
	end := smp(240, 3.42, 3.40, -36)
	end.Limit = 0
	// 90 с без заряда (150 → 240) — подъём продолжается
	if recs := feedAll(tr, smp(0, 3.38, 3.36, -36), smp(60, 3.40, 3.38, -36), smp(150, 3.41, 3.39, 0)); len(recs) != 0 || tr.r == nil {
		t.Fatalf("gap broke the rise: %v", recs)
	}
	r := tr.feed(end)
	if r == nil || r.EndReason != "bms_limit" || len(r.AhLag) != 1 {
		t.Fatalf("record = %+v", r)
	}
	// 3,40: 60 → 150 с при 36 А = 0,9 А·ч, пауза без тока не прибавляет; нижняя дошла на 240
	if g := r.AhLag[0]; g.V != 3.40 || !g.Reached || g.Ah != 0.9 {
		t.Errorf("lag = %+v", g)
	}
}

func TestImbReplayHistory(t *testing.T) {
	hist := make([][]hass.HistState, len(imbEntities))
	for k, s := range fromFlat() {
		at := s.T.Add(time.Duration(k%3) * 300 * time.Millisecond) // одна публикация — разброс в мс
		hist[0] = append(hist[0], hass.HistState{Time: at, State: ftoa(s.Max)})
		hist[1] = append(hist[1], hass.HistState{Time: at.Add(100 * time.Millisecond), State: ftoa(s.Min)})
	}
	hist[2] = []hass.HistState{{Time: imbT0, State: "-36"}, {Time: imbT0.Add(10 * time.Minute), State: "0"}}
	hist[4] = []hass.HistState{{Time: imbT0, State: "120 / 185 А"}}
	hist[5] = []hass.HistState{{Time: imbLocFixAt.Add(-time.Hour), State: "М12·№3"}, {Time: imbT0, State: "М12·№12"}}
	hist[7] = []hass.HistState{{Time: imbT0, State: "заряд 36 А · разбег 20 мВ · порций 0"}}
	recs := imbReplay(hist)
	if len(recs) != 1 || recs[0].Source != "history" || recs[0].Phase != balCharge || len(recs[0].Levels) != 4 {
		t.Fatalf("records = %+v", recs)
	}
	if g := recs[0].AhLag[1]; g.Ah != 1.8 || !g.Reached {
		t.Errorf("lag 3.40 = %+v", g)
	}
	if swapLoc("М12·№3") != "М3·№12" {
		t.Error("swapLoc")
	}
}

func ftoa(v float64) string { b, _ := json.Marshal(v); return string(b) }

func TestImbJournalRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "imbalance.jsonl")
	j := loadImbJournal(p)
	recs := feedAll(&imbTracker{source: "history"}, fromFlat()...)
	if err := j.add(recs[0], recs[0]); err != nil {
		t.Fatal(err)
	}
	got := loadImbJournal(p)
	if got.n != 2 || got.last == nil || got.last.AhLag[1].Ah != 1.8 || got.last.Source != "history" {
		t.Errorf("journal = %d %+v", got.n, got.last)
	}
}

func TestAddImbalanceRowOnce(t *testing.T) {
	var cfg any
	_ = json.Unmarshal([]byte(`{"views":[{"sections":[{"cards":[{"type":"entities","entities":[{"entity":"sensor.deye_sun_30k_battery_soh"}]},
	  {"type":"entities","entities":[{"entity":"sensor.energy_schema_bms_weak_cell"}]}]}]}]}`), &cfg)
	if ch, err := patchBMSCards(cfg); err != nil || !ch {
		t.Fatalf("changed=%v err=%v", ch, err)
	}
	if ch, _ := patchBMSCards(cfg); ch {
		t.Error("second run must be a no-op")
	}
	b, _ := json.Marshal(cfg)
	if strings.Count(string(b), imbEntity) != 1 || !strings.Contains(string(b), imbRowName) {
		t.Errorf("row: %s", b)
	}
}
