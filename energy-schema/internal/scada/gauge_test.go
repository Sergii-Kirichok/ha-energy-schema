package scada

import (
	"math"
	"testing"
)

// The teardrop tip must point radially OUTWARD — i.e. sit farther from the
// gauge center than the arc point, at distance r+d, for every angle. The round
// head's center stays on the arc (distance r).
func TestMarkerTipPointsOutward(t *testing.T) {
	cx, cy, r, sz := 200.0, 200.0, 80.0, 8.0
	d := sz * markerTipFactor
	for _, a := range []float64{0, 45, 90, 135, 180} {
		tx, ty := markerTip(cx, cy, r, a, sz)
		dist := math.Hypot(tx-cx, ty-cy)
		if dist <= r {
			t.Errorf("a=%.0f°: tip dist %.2f not outward (<= r %.0f)", a, dist, r)
		}
		if math.Abs(dist-(r+d)) > 1e-6 {
			t.Errorf("a=%.0f°: tip dist %.3f, want r+d %.3f", a, dist, r+d)
		}
	}
}

// Spot-check the tip direction at the cardinal angles — it points away from center.
func TestMarkerTipDirection(t *testing.T) {
	cx, cy, r, sz := 200.0, 200.0, 80.0, 8.0
	d := sz * markerTipFactor
	// 180° -> arc point at left, tip must move farther left (away from center).
	if tx, _ := markerTip(cx, cy, r, 180, sz); math.Abs(tx-(cx-r-d)) > 1e-6 {
		t.Errorf("180°: tip x %.3f, want %.3f", tx, cx-r-d)
	}
	// 90° -> arc point at top, tip must move farther up.
	if _, ty := markerTip(cx, cy, r, 90, sz); math.Abs(ty-(cy-r-d)) > 1e-6 {
		t.Errorf("90°: tip y %.3f, want %.3f", ty, cy-r-d)
	}
	// 0° -> arc point at right, tip must move farther right.
	if tx, _ := markerTip(cx, cy, r, 0, sz); math.Abs(tx-(cx+r+d)) > 1e-6 {
		t.Errorf("0°: tip x %.3f, want %.3f", tx, cx+r+d)
	}
}
