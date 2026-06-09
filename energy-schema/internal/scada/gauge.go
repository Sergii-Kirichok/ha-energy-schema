package scada

import "math"

// marker draws the white teardrop needle of a gauge at angle a on radius r.
// The pointer (local +Y / the tip) is rotated to face the gauge center.
func (s *Builder) marker(cx, cy, r, a, sz float64) {
	mx, my := pt(cx, cy, r, a)
	rot := math.Atan2(mx-cx, cy-my) * 180 / math.Pi // местная +Y (остриё) → к центру
	R := sz * 0.95                                  // радиус головы
	T := sz * 1.8                                   // длина к острию
	s.p(`<g transform="translate(%.1f,%.1f) rotate(%.1f)"><path d="M 0,%.1f C %.1f,%.1f %.1f,%.1f 0,%.1f C %.1f,%.1f %.1f,%.1f 0,%.1f Z" fill="#ffffff" stroke="#0f1115" stroke-width="1"/></g>`,
		mx, my, rot, -R, R*1.33, -R, R*0.55, T, 0.0, T, -R*0.55, T, -R*1.33, -R)
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
