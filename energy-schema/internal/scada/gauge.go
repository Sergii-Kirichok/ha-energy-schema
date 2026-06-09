package scada

import "math"

// marker draws a white teardrop needle at angle a on radius r: a round head
// riding on the arc that tapers to a sharp point aimed at the gauge center.
// Built from an explicit inward vector, so it orients correctly at any angle.
// markerTip returns the teardrop tip point for the marker at angle a — it sits
// d inside the arc point, i.e. distance r-d from the gauge center (always inward).
func markerTip(cx, cy, r, a, sz float64) (float64, float64) {
	mx, my := pt(cx, cy, r, a)
	d := sz * markerTipFactor
	return mx + (cx-mx)/r*d, my + (cy-my)/r*d
}

const markerTipFactor = 2.3 // вынос острия (× sz) от центра головы

func (s *Builder) marker(cx, cy, r, a, sz float64) {
	mx, my := pt(cx, cy, r, a)
	ux, uy := (cx-mx)/r, (cy-my)/r        // единичный вектор к центру (остриё туда)
	hr := sz                              // радиус головы
	d := sz * markerTipFactor             // вынос острия от центра головы
	alpha := math.Acos(hr / d)            // полуугол касательных острия к голове
	a0 := math.Atan2(uy, ux)              // направление к центру
	tx, ty := markerTip(cx, cy, r, a, sz) // вершина (остриё) — к центру
	s.p(`<path d="M %.1f,%.1f`, tx, ty)
	const n = 16
	for i := 0; i <= n; i++ { // дуга головы по внешней стороне (минуя клин острия)
		ang := a0 + alpha + (2*math.Pi-2*alpha)*float64(i)/float64(n)
		s.p(` L %.1f,%.1f`, mx+hr*math.Cos(ang), my+hr*math.Sin(ang))
	}
	s.p(` Z" fill="#ffffff" stroke="#0f1115" stroke-width="1"/>`)
}

// gauge draws a 180° semicircular gauge with colored bands, a needle, a value
// label and an optional caption.
func (s *Builder) gauge(cx, cy, r, val, max float64, bands []band, valTxt, label string) {
	s.arc(cx, cy, r, 180, 0, "#23272f", 14)
	prev := 0.0
	cur := cSub
	for _, b := range bands {
		s.arc(cx, cy, r, gAng(prev, max), gAng(b.thr, max), b.col, 14)
		if val >= prev && val < b.thr {
			cur = b.col
		}
		prev = b.thr
	}
	s.marker(cx, cy, r, gAng(val, max), r*0.12)
	s.t(cx, cy, r*0.30, cur, "middle", valTxt)
	if label != "" {
		s.t(cx, cy+20, 12, cSub, "middle", label)
	}
}

// bar draws a horizontal scale bar with color zones, a teardrop marker and a
// centered value text.
func (s *Builder) bar(x, y, w, h, val, max float64, bands []band, valTxt string) {
	prev := 0.0
	for _, b := range bands {
		x1 := x + w*prev/max
		x2 := x + w*math.Min(b.thr, max)/max
		s.p(`<rect x="%.1f" y="%g" width="%.1f" height="%g" fill="%s" opacity="0.85"/>`, x1, y, x2-x1, h, b.col)
		prev = b.thr
	}
	s.p(`<rect x="%g" y="%g" width="%g" height="%g" rx="6" fill="none" stroke="%s" stroke-width="1"/>`, x, y, w, h, cBrd)
	mv := val
	if mv > max {
		mv = max
	}
	mx := x + w*mv/max
	s.p(`<path d="M %.1f,%g C %.1f,%g %.1f,%g %.1f,%g C %.1f,%g %.1f,%g %.1f,%g Z" fill="#ffffff" stroke="#0f1115" stroke-width="1"/>`,
		mx, y+h, mx+7, y+h+9, mx+7, y+h+18, mx, y+h+18, mx-7, y+h+18, mx-7, y+h+9, mx, y+h)
	s.t(x+w/2, y+h/2+10, 28, "#ffffff", "middle", valTxt)
}
