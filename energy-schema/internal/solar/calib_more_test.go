package solar

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestGeoHash(t *testing.T) {
	a := []Array{{Name: "s", KWp: 5.123, TiltDeg: 30.4, AzDeg: 180, Bifacial: 0.12}, {KWp: 2, TiltDeg: 90, AzDeg: 90}}
	if got, want := GeoHash(a), "5.12/30/180/0.12;2.00/90/90/0.00;"; got != want {
		t.Fatalf("GeoHash = %q, want %q", got, want)
	}
	if GeoHash(nil) != "" {
		t.Error("no arrays → empty hash")
	}
	b := []Array{{Name: "other name", KWp: 5.12, TiltDeg: 30, AzDeg: 180, Bifacial: 0.12}, a[1]}
	if GeoHash(a) != GeoHash(b) {
		t.Error("name and sub-rounding noise must not change the hash")
	}
	b[0].AzDeg = 181
	if GeoHash(a) == GeoHash(b) {
		t.Error("azimuth change must change the hash")
	}
}

func TestKpBinAndClamp(t *testing.T) {
	for _, c := range []struct {
		kp  float64
		bin int
	}{{0, 0}, {0.149, 0}, {0.15, 1}, {0.35, 2}, {0.55, 3}, {0.75, 4}, {0.919, 4}, {0.92, 5}, {1.1, 5}} {
		if got := kpBin(c.kp); got != c.bin {
			t.Errorf("kpBin(%v) = %d, want %d", c.kp, got, c.bin)
		}
	}
	if clampf(-1, 0, 1) != 0 || clampf(2, 0, 1) != 1 || clampf(0.5, 0, 1) != 0.5 {
		t.Error("clampf")
	}
	if omAzimuth(-10) != 170 || omAzimuth(0) != -180 || omAzimuth(270) != 90 {
		t.Errorf("omAzimuth: %v %v %v", omAzimuth(-10), omAzimuth(0), omAzimuth(270))
	}
}

func TestStatus(t *testing.T) {
	c := NewCalibrator(t.TempDir()+"/c.json", "g")
	if w, r, d, m := c.Status(); w != 0 || r != 1 || d != 0 || m != 0 {
		t.Fatalf("cold Status = %v %v %v %v", w, r, d, m)
	}
	// 9 days: MAE over the LAST 7 only (first two have a huge error)
	for i := 0; i < 9; i++ {
		e := 1.0
		if i < 2 {
			e = 100
		}
		c.d.Days = append(c.d.Days, dayRec{Day: strconv.Itoa(i), PredCal: 10, Act: 10 - e})
	}
	c.d.Level = cell{SumF: 100, SumA: 1000, W: 30} // ratio clamps at 3 → Level clamps at 1.3
	w, r, d, m := c.Status()
	if w != 30 || r != 1.3 || d != 9 || !near(m, 1) {
		t.Fatalf("Status = %v %v %v %v, want 30 1.3 9 1", w, r, d, m)
	}
	c.d.Level = cell{SumF: 100, SumA: 10, W: 30}
	if _, r, _, _ := c.Status(); r != 0.5 {
		t.Errorf("low level ratio = %v, want clamp 0.5", r)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calib.json")
	c := NewCalibrator(path, "geoA")
	c.d.KBin["open-meteo"] = [6]cell{3: {SumF: 10, SumA: 8, W: 2}}
	c.d.Level = cell{SumF: 50, SumA: 45, W: 3}
	c.d.Days = []dayRec{{Day: "2026-06-09", PredRaw: 30, PredCal: 28, Act: 27}}
	c.d.LastHour = 1781000000
	c.Freeze(1781003600, 2.5, 3, "met.no")
	before := c.Apply("open-meteo", 5, 7)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file left behind: %v", err)
	}

	l := NewCalibrator(path, "geoA")
	if got := l.Apply("open-meteo", 5, 7); !near(got, before) {
		t.Errorf("Apply after reload = %v, want %v", got, before)
	}
	if l.d.LastHour != 1781000000 || len(l.d.Days) != 1 || l.d.Days[0] != c.d.Days[0] || l.d.Level != c.d.Level {
		t.Errorf("reloaded state differs: %+v", l.d)
	}
	if p := l.d.Pending["1781003600"]; p.Raw != 2.5 || p.Eclr != 3 || p.Src != "met.no" {
		t.Errorf("pending = %+v", p)
	}

	// geometry changed → clean start under the new hash
	g := NewCalibrator(path, "geoB")
	if len(g.d.KBin) != 0 || g.d.Level.W != 0 || len(g.d.Days) != 0 || g.d.GeoHash != "geoB" {
		t.Errorf("geometry change must reset: %+v", g.d)
	}
}

func TestNewCalibratorBadFiles(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.json")
	os.WriteFile(corrupt, []byte("{not json"), 0644)
	nilMaps := filepath.Join(dir, "nil.json")
	os.WriteFile(nilMaps, []byte(`{"geo":"g","level":{"sf":1,"sa":1,"w":1}}`), 0644)

	for name, path := range map[string]string{"corrupt": corrupt, "dir": dir, "nilmaps": nilMaps} {
		c := NewCalibrator(path, "g")
		if c.d.KBin == nil || c.d.Pending == nil || c.d.GeoHash != "g" {
			t.Fatalf("%s: maps/geo not initialised: %+v", name, c.d)
		}
		c.Freeze(1, 1, 1, "x") // must not panic on a nil map
		if got := c.Apply("x", 4, 5); math.IsNaN(got) || got <= 0 {
			t.Errorf("%s: Apply = %v", name, got)
		}
	}
	if c := NewCalibrator(nilMaps, "g"); c.d.Level.W != 1 {
		t.Error("valid file with omitted maps must keep its other fields")
	}
}

func TestSaveError(t *testing.T) {
	c := NewCalibrator(filepath.Join(t.TempDir(), "no", "such", "dir", "c.json"), "g")
	if err := c.Save(); err == nil {
		t.Fatal("Save into a missing directory must fail")
	}
}

// eClr ≤ 0 → no weather bin and no ceiling; negative raw clamps to 0; output
// never exceeds 1.3× ceiling even with a strong learned correction.
func TestApplyEdges(t *testing.T) {
	c := NewCalibrator(t.TempDir()+"/c.json", "g")
	if got := c.Apply("om", 3, 0); got != 3 {
		t.Errorf("eClr=0 → %v, want raw 3", got)
	}
	if got := c.Apply("om", -2, 5); got != 0 {
		t.Errorf("negative raw → %v, want 0", got)
	}
	c.d.KBin["om"] = [6]cell{5: {SumF: 10, SumA: 30, W: 1000}} // ×3 → clamp 2.2
	if got := c.Apply("om", 10, 10); !near(got, 13) {
		t.Errorf("strong correction = %v, want capped 1.3×10", got)
	}
	if got := c.Apply("om", 1, 10); !near(got, 1) { // kp 0.1 → empty bin 0
		t.Errorf("other bin = %v, want 1", got)
	}
}

// Low coverage and night hours are not learned; pending >48 h are purged;
// the Days history is capped at 60.
func TestObserveDueGatesAndHousekeeping(t *testing.T) {
	c := NewCalibrator(t.TempDir()+"/c.json", "g")
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.d.LastHour = base.Add(-time.Hour).Unix() // cold start would begin at now-2h and skip 12:00
	c.Freeze(base.Unix(), 10, 11, "om")
	c.Freeze(base.Add(time.Hour).Unix(), 10, 0.1, "om") // night: Eclr < eclrMin
	c.ObserveDue(base.Add(2*time.Hour+10*time.Minute),
		func(int64) (float64, int) { return 1, covMin - 1 },
		func(int64) (float64, bool) { return 0, false })
	if b := c.d.KBin["om"]; b != ([6]cell{}) {
		t.Errorf("low coverage must not train: %+v", b)
	}
	if len(c.d.Pending) != 0 {
		t.Errorf("observed hours must leave pending: %v", c.d.Pending)
	}

	c.Freeze(base.Add(-72*time.Hour).Unix(), 1, 1, "om") // stale
	c.Freeze(base.Add(72*time.Hour).Unix(), 1, 1, "om")  // future
	c.ObserveDue(base.Add(3*time.Hour), func(int64) (float64, int) { return 0, 12 }, func(int64) (float64, bool) { return 0, false })
	if _, ok := c.d.Pending[strconv.FormatInt(base.Add(-72*time.Hour).Unix(), 10)]; ok || len(c.d.Pending) != 1 {
		t.Errorf("stale pending not purged / future lost: %v", c.d.Pending)
	}

	c2 := NewCalibrator(t.TempDir()+"/c.json", "g")
	for day := 0; day < 70; day++ {
		h := base.AddDate(0, 0, day)
		c2.Freeze(h.Unix(), 10, 11, "om")
		c2.ObserveDue(h.Add(70*time.Minute), func(int64) (float64, int) { return 8, 12 }, func(int64) (float64, bool) { return 50, true })
	}
	if n := len(c2.d.Days); n != 60 {
		t.Fatalf("Days = %d, want capped at 60", n)
	}
	if last := c2.d.Days[59].Day; last != "2026-03-10" { // day 69 is still open
		t.Errorf("last closed day = %s", last)
	}
	if w, _, _, mae := c2.Status(); w <= 0 || math.IsNaN(mae) || mae < 0 {
		t.Errorf("Status after training: w=%v mae=%v", w, mae)
	}
}
