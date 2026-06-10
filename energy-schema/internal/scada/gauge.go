package scada

import (
	"fmt"
	"math"
)

const (
	markerTipFactor  = 1.0 // вынос острия (× sz) наружу: кончик ~у осевой линии полосы
	markerHeadFactor = 0.6 // радиус головы капли (× sz)
	markerInset      = 7.0 // полуширина полосы гейджа: центр головы — на ВНУТРЕННЕЙ дуге
)

// markerTip returns the teardrop tip point for the marker at angle a — it sits
// outward from the (inset) head center, at radius (r-markerInset)+d.
func markerTip(cx, cy, r, a, sz float64) (float64, float64) {
	rr := r - markerInset
	mx, my := pt(cx, cy, rr, a)
	d := sz * markerTipFactor
	return mx - (cx-mx)/rr*d, my - (cy-my)/rr*d
}

// marker draws a white teardrop at angle a: a round head whose center sits on
// the INNER edge of the band (closer to the gauge center), tapering to a sharp
// point aimed OUTWARD. Built from an explicit radial vector → correct at any angle.
func (s *Builder) marker(cx, cy, r, a, sz float64) {
	rr := r - markerInset // центр головы — на внутренней дуге
	mx, my := pt(cx, cy, rr, a)
	ux, uy := (cx-mx)/rr, (cy-my)/rr      // единичный вектор к центру
	hr := sz * markerHeadFactor           // радиус головы
	d := sz * markerTipFactor             // вынос острия наружу
	alpha := math.Acos(hr / d)            // полуугол касательных острия к голове
	pd := math.Atan2(-uy, -ux)            // направление острия — НАРУЖУ (от центра)
	tx, ty := markerTip(cx, cy, r, a, sz) // вершина (остриё) — наружу
	s.p(`<path d="M %.1f,%.1f`, tx, ty)
	const n = 16
	for i := 0; i <= n; i++ { // дуга головы со стороны центра (минуя клин острия)
		ang := pd + alpha + (2*math.Pi-2*alpha)*float64(i)/float64(n)
		s.p(` L %.1f,%.1f`, mx+hr*math.Cos(ang), my+hr*math.Sin(ang))
	}
	s.p(` Z" fill="#ffffff" stroke="#0f1115" stroke-width="1"/>`)
}

// markerMax draws a colored teardrop on the OUTER edge of the band, pointing
// INWARD — marks today's peak value on a gauge.
func (s *Builder) markerMax(cx, cy, r, a, sz float64, col string) {
	rr := r + markerInset // центр головы — на ВНЕШНЕЙ дуге
	mx, my := pt(cx, cy, rr, a)
	ux, uy := (cx-mx)/rr, (cy-my)/rr // единичный вектор к центру
	hr := sz * markerHeadFactor
	d := sz * markerTipFactor
	alpha := math.Acos(hr / d)
	pd := math.Atan2(uy, ux)   // направление острия — ВНУТРЬ (к центру)
	tx, ty := mx+ux*d, my+uy*d // вершина (остриё) — внутрь
	s.p(`<path d="M %.1f,%.1f`, tx, ty)
	const n = 16
	for i := 0; i <= n; i++ { // дуга головы со внешней стороны (минуя клин острия)
		ang := pd + alpha + (2*math.Pi-2*alpha)*float64(i)/float64(n)
		s.p(` L %.1f,%.1f`, mx+hr*math.Cos(ang), my+hr*math.Sin(ang))
	}
	s.p(` Z" fill="%s" stroke="#0f1115" stroke-width="1"/>`, col)
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

// ringTimer draws a circular countdown gauge: a faint full ring, a coloured arc
// for the fraction remaining (clockwise from 12 o'clock), the value in the
// centre and a caption above it.
func (s *Builder) ringTimer(cx, cy, r, frac float64, col, label, center string) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	s.p(`<circle cx="%g" cy="%g" r="%g" fill="none" stroke="#23272f" stroke-width="6"/>`, cx, cy, r)
	if frac >= 0.999 {
		s.p(`<circle cx="%g" cy="%g" r="%g" fill="none" stroke="%s" stroke-width="6"/>`, cx, cy, r, col)
	} else if frac > 0.001 {
		x0, y0 := pt(cx, cy, r, 90)          // 12 часов
		x1, y1 := pt(cx, cy, r, 90-frac*360) // по часовой
		large := 0
		if frac > 0.5 {
			large = 1
		}
		s.p(`<path fill="none" stroke="%s" stroke-width="6" stroke-linecap="round" d="M %.1f %.1f A %g %g 0 %d 1 %.1f %.1f"/>`, col, x0, y0, r, r, large, x1, y1)
	}
	s.t(cx, cy+6, 17, col, "middle", center)
	s.t(cx, cy-r-8, 11, cSub, "middle", label)
}

// barTicks places small value labels (no unit) at the given scale values under
// a horizontal bar, so colour-zone boundaries are readable.
func (s *Builder) barTicks(x, y, w, max float64, vals []float64) {
	for _, v := range vals {
		s.t(x+w*v/max, y, 9, cSub, "middle", fmt.Sprintf("%.0f", v))
	}
}

// gaugeTick draws a short radial tick just outside the band at value v's angle
// and the label further out along the same radius — marks colour-zone
// boundaries clearly separated from the arc.
func (s *Builder) gaugeTick(cx, cy, r, v, max float64, txt string) {
	a := gAng(v, max)
	x1, y1 := pt(cx, cy, r+7, a)
	x2, y2 := pt(cx, cy, r+13, a)
	s.poly(cSub, 1.5, "", x1, y1, x2, y2)
	lx, ly := pt(cx, cy, r+23, a)
	s.t(lx, ly+3, 10, cSub, "middle", txt)
}

// barMax draws a colored teardrop ABOVE a horizontal bar at value v's position,
// tip pointing down onto the scale — marks a peak (mirror of the value needle).
func (s *Builder) barMax(x, y, w, v, max float64, col string) {
	mv := v
	if mv > max {
		mv = max
	}
	if mv < 0 {
		mv = 0
	}
	mx := x + w*mv/max
	s.p(`<path d="M %.1f,%g C %.1f,%g %.1f,%g %.1f,%g C %.1f,%g %.1f,%g %.1f,%g Z" fill="%s" stroke="#0f1115" stroke-width="1"/>`,
		mx, y, mx+7, y-9, mx+7, y-18, mx, y-18, mx-7, y-18, mx-7, y-9, mx, y, col)
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
