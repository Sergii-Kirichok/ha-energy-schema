package scada

import (
	"math"
	"testing"
)

// The head center sits on the inner arc (r-markerInset); the tip points radially
// OUTWARD from it, so the tip is at distance (r-markerInset)+d for every angle.
func TestMarkerTipPointsOutward(t *testing.T) {
	cx, cy, r, sz := 200.0, 200.0, 80.0, 8.0
	rr := r - markerInset
	d := sz * markerTipFactor
	for _, a := range []float64{0, 45, 90, 135, 180} {
		tx, ty := markerTip(cx, cy, r, a, sz)
		dist := math.Hypot(tx-cx, ty-cy)
		if dist <= rr {
			t.Errorf("a=%.0f°: tip dist %.2f not outward (<= head radius %.2f)", a, dist, rr)
		}
		if math.Abs(dist-(rr+d)) > 1e-6 {
			t.Errorf("a=%.0f°: tip dist %.3f, want (r-inset)+d %.3f", a, dist, rr+d)
		}
	}
}

// Spot-check tip direction at the cardinal angles — outward from the inset head.
func TestMarkerTipDirection(t *testing.T) {
	cx, cy, r, sz := 200.0, 200.0, 80.0, 8.0
	rr := r - markerInset
	d := sz * markerTipFactor
	// 180° -> head at left (cx-rr), tip farther left.
	if tx, _ := markerTip(cx, cy, r, 180, sz); math.Abs(tx-(cx-rr-d)) > 1e-6 {
		t.Errorf("180°: tip x %.3f, want %.3f", tx, cx-rr-d)
	}
	// 90° -> head at top (cy-rr), tip farther up.
	if _, ty := markerTip(cx, cy, r, 90, sz); math.Abs(ty-(cy-rr-d)) > 1e-6 {
		t.Errorf("90°: tip y %.3f, want %.3f", ty, cy-rr-d)
	}
	// 0° -> head at right (cx+rr), tip farther right.
	if tx, _ := markerTip(cx, cy, r, 0, sz); math.Abs(tx-(cx+rr+d)) > 1e-6 {
		t.Errorf("0°: tip x %.3f, want %.3f", tx, cx+rr+d)
	}
}
