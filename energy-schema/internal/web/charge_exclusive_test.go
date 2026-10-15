package web

import "testing"

func TestExclusive(t *testing.T) {
	for _, c := range []struct {
		prevBal, bal, prevFull, full bool
		want                         string
	}{
		{false, true, true, true, chargeFullNowEntity}, // балансировку включили при полном → полный выкл
		{true, true, false, true, chargeBalanceEntity}, // полный включили при балансировке → балансировка выкл
		{true, true, true, true, ""},                   // без фронта (старт, ждём HA) — ничего
		{false, true, false, false, ""},
		{true, true, false, false, ""},
		{false, false, false, true, ""},
	} {
		if got := exclusive(c.prevBal, c.bal, c.prevFull, c.full); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}
