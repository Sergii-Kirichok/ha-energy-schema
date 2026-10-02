package web

import (
	"path/filepath"
	"testing"
)

// Сырые значения сняты с инвертора 02.10 (read_holding_registers).
func TestInvFieldDecodeLive(t *testing.T) {
	cases := []struct {
		key    string
		raw    int
		factor float64
		want   float64
	}{
		{"zero_export_w", 15, 2, 150},
		{"v_high", 2650, 2, 265},
		{"v_low", 1500, 2, 150},
		{"f_high", 5150, 2, 51.5},
		{"f_low", 4800, 2, 48},
		{"slot1_time", 100, 2, 60},    // 01:00
		{"slot6_time", 2100, 2, 1260}, // 21:00
		{"max_charge_a", 10, 2, 20},   // ×2 канала
		{"tou", 255, 2, 255},
	}
	for _, c := range cases {
		f := invField0(c.key)
		if got := f.decode(c.raw, c.factor); got != c.want {
			t.Errorf("%s decode(%d) = %v, want %v", c.key, c.raw, got, c.want)
		}
		if raw, err := f.encode(c.want, c.factor); err != nil || raw != c.raw {
			t.Errorf("%s encode(%v) = %d, %v; want %d", c.key, c.want, raw, err, c.raw)
		}
	}
}

func TestInvFieldEncodeRejects(t *testing.T) {
	bad := map[string]float64{"v_high": 300, "v_low": 50, "f_high": 70, "reconnect_s": 0,
		"gen_charge": 2, "work_mode": 3, "slot1_src": 4, "slot1_time": 1440, "shutdown_soc": 1, "tou": 256}
	for k, v := range bad {
		if _, err := invField0(k).encode(v, 2); err == nil {
			t.Errorf("%s=%v must be rejected", k, v)
		}
	}
}

func TestValidatePair(t *testing.T) {
	ok := map[string]float64{"v_low": 150, "v_high": 265, "f_low": 48, "f_high": 51.5,
		"slot1_time": 60, "slot2_time": 300, "slot3_time": 540, "slot4_time": 840, "slot5_time": 960, "slot6_time": 1260}
	if err := validatePair(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]float64{{"v_low": 265}, {"f_high": 48}, {"slot3_time": 300}} {
		m := map[string]float64{}
		for k, v := range ok {
			m[k] = v
		}
		for k, v := range bad {
			m[k] = v
		}
		if validatePair(m) == nil {
			t.Errorf("must reject %v", bad)
		}
	}
}

func TestOverridesPersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ovr.json")
	o := loadOverrides(p)
	if o.get("grid_charge") {
		t.Fatal("empty overrides must be false")
	}
	o.set(map[string]bool{"grid_charge": true})
	if !loadOverrides(p).get("grid_charge") {
		t.Error("override not persisted")
	}
	var nilO *overrides
	if nilO.get("x") {
		t.Error("nil overrides must read false")
	}
}

func TestInvFieldsUniqueRegs(t *testing.T) {
	seen := map[int]string{}
	for _, f := range invFields {
		if k, dup := seen[f.Reg]; dup {
			t.Errorf("register %d used by %s and %s", f.Reg, k, f.Key)
		}
		seen[f.Reg] = f.Key
		in := false
		for _, b := range invBlocks {
			in = in || (f.Reg >= b[0] && f.Reg < b[0]+b[1])
		}
		if !in {
			t.Errorf("%s reg %d is outside read blocks", f.Key, f.Reg)
		}
	}
}
