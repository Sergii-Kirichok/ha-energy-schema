package scada

import (
	"math"
	"testing"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestGAng(t *testing.T) {
	cases := []struct{ v, max, want float64 }{
		{0, 100, 180},   // empty -> left
		{100, 100, 0},   // full -> right
		{50, 100, 90},   // half -> top
		{150, 100, 0},   // clamp over max
		{-10, 100, 180}, // clamp under zero
	}
	for _, c := range cases {
		if got := gAng(c.v, c.max); !almost(got, c.want) {
			t.Errorf("gAng(%v,%v) = %v, want %v", c.v, c.max, got, c.want)
		}
	}
}

func TestPt(t *testing.T) {
	// 0° -> +X; 90° -> -Y (SVG y-down); 180° -> -X.
	if x, y := pt(0, 0, 1, 0); !almost(x, 1) || !almost(y, 0) {
		t.Errorf("pt 0deg = %v,%v", x, y)
	}
	if x, y := pt(0, 0, 1, 90); !almost(x, 0) || !almost(y, -1) {
		t.Errorf("pt 90deg = %v,%v", x, y)
	}
	if x, y := pt(10, 10, 2, 180); !almost(x, 8) || !almost(y, 10) {
		t.Errorf("pt 180deg = %v,%v", x, y)
	}
}

func TestPathD(t *testing.T) {
	if got := pathD([]float64{1, 2, 3, 4}); got != "M 1,2 3,4" {
		t.Errorf("pathD = %q", got)
	}
}

func TestRevPts(t *testing.T) {
	got := revPts([]float64{1, 2, 3, 4, 5, 6})
	want := []float64{5, 6, 3, 4, 1, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("revPts = %v, want %v", got, want)
		}
	}
}

func TestKW(t *testing.T) {
	if got := kw(2250); got != "2.25 кВт" {
		t.Errorf("kw(2250) = %q", got)
	}
	if got := kw(0); got != "0.00 кВт" {
		t.Errorf("kw(0) = %q", got)
	}
}
