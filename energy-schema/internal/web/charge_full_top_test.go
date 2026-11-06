package web

import "testing"

// Полный заряд кончает BMS (снятый лимит у верха), а не SOC 100 %; страховка — ячейка.
func TestFullTop(t *testing.T) {
	for _, c := range []struct {
		name, cell, limit string
		soc               float64
		want              bool
	}{
		{"SOC 100 on flat, BMS still allows", "3.400", "78", 100, false},
		{"BMS dropped limit near top", "3.600", "0", 100, true},
		{"BMS ban on flat part is not top", "3.340", "0", 80, false},
		{"BMS silent, cell at safety stop", "3.620", "78", 99, true},
		{"no BMS limits sensor, below stop", "3.500", "", 100, false},
	} {
		s, _ := balSetup(t, map[string]string{chargeBalanceEntity: "off", bmsCellMaxEntity: c.cell}, nil, c.limit)
		if got := s.fullTop(c.soc); got != c.want {
			t.Errorf("%s: fullTop = %v, want %v", c.name, got, c.want)
		}
	}
}
