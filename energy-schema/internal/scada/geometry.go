package scada

import (
	"fmt"
	"math"
)

// pt returns the cartesian point at radius r and angle deg (degrees, CCW,
// SVG y-down) around (cx,cy).
func pt(cx, cy, r, deg float64) (float64, float64) {
	rad := deg * math.Pi / 180
	return cx + r*math.Cos(rad), cy - r*math.Sin(rad)
}

// pathD builds an SVG path "M x,y x,y ..." from flat x,y pairs.
func pathD(pts []float64) string {
	d := "M"
	for i := 0; i < len(pts); i += 2 {
		d += fmt.Sprintf(" %g,%g", pts[i], pts[i+1])
	}
	return d
}

// pathLen returns the total polyline length through the flat x,y pairs.
func pathLen(pts []float64) float64 {
	total := 0.0
	for i := 2; i < len(pts); i += 2 {
		total += math.Hypot(pts[i]-pts[i-2], pts[i+1]-pts[i-1])
	}
	return total
}

// pointAt returns the point at distance dist along the polyline (flat x,y pairs).
func pointAt(pts []float64, dist float64) (float64, float64) {
	if len(pts) < 4 {
		return pts[0], pts[1]
	}
	for i := 2; i < len(pts); i += 2 {
		seg := math.Hypot(pts[i]-pts[i-2], pts[i+1]-pts[i-1])
		if dist <= seg || i == len(pts)-2 {
			t := 0.0
			if seg > 0 {
				t = dist / seg
			}
			if t > 1 {
				t = 1
			}
			return pts[i-2] + (pts[i]-pts[i-2])*t, pts[i-1] + (pts[i+1]-pts[i-1])*t
		}
		dist -= seg
	}
	return pts[len(pts)-2], pts[len(pts)-1]
}

// pointDir returns the point at distance dist along the polyline plus the unit
// tangent direction there (for orienting flow arrows).
func pointDir(pts []float64, dist float64) (x, y, dx, dy float64) {
	x, y = pointAt(pts, dist)
	x2, y2 := pointAt(pts, dist+1.0)
	dx, dy = x2-x, y2-y
	if l := math.Hypot(dx, dy); l > 0 {
		dx, dy = dx/l, dy/l
	} else {
		dx, dy = 1, 0
	}
	return
}

// revPts returns the x,y pairs in reverse order (for reversed flow animation).
func revPts(pts []float64) []float64 {
	r := make([]float64, len(pts))
	n := len(pts)
	for i := 0; i < n; i += 2 {
		r[i] = pts[n-2-i]
		r[i+1] = pts[n-1-i]
	}
	return r
}

// gAng maps a value in [0,max] to a gauge angle in [180,0] degrees.
func gAng(v, max float64) float64 {
	if v > max {
		v = max
	}
	if v < 0 {
		v = 0
	}
	return 180 - v/max*180
}

// arc draws a stroked circular arc from angle a1 to a2.
func (s *Builder) arc(cx, cy, r, a1, a2 float64, col string, wdt float64) {
	x1, y1 := pt(cx, cy, r, a1)
	x2, y2 := pt(cx, cy, r, a2)
	large := 0
	if math.Abs(a1-a2) > 180 {
		large = 1
	}
	s.p(`<path fill="none" stroke="%s" stroke-width="%g" stroke-linecap="butt" d="M %.1f %.1f A %g %g 0 %d 1 %.1f %.1f"/>`, col, wdt, x1, y1, r, r, large, x2, y2)
}
