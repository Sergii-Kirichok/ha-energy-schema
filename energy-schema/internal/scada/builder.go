package scada

import (
	"fmt"
	"html"
	"strings"
)

// Builder accumulates SVG markup with small drawing primitives.
// phase is the current animation time (seconds) used to position the marching
// flow arrows — baked into static coordinates so they move even when the SVG is
// rasterised (rsvg/ТВ doesn't play SMIL).
type Builder struct {
	b     strings.Builder
	phase float64
}

// String returns the accumulated SVG.
func (s *Builder) String() string { return s.b.String() }

// p writes a formatted fragment.
func (s *Builder) p(f string, a ...interface{}) { s.b.WriteString(fmt.Sprintf(f, a...)) }

// box draws a rounded card rectangle.
func (s *Builder) box(x, y, w, h float64) {
	s.p(`<rect x="%g" y="%g" width="%g" height="%g" rx="12" fill="%s" stroke="%s" stroke-width="1.5"/>`, x, y, w, h, cBox, cBrd)
}

// t draws an anchored text node (content is HTML-escaped).
func (s *Builder) t(x, y, sz float64, col, anchor, str string) {
	s.p(`<text x="%g" y="%g" font-size="%g" fill="%s" text-anchor="%s">%s</text>`, x, y, sz, col, anchor, html.EscapeString(str))
}

// dot draws a filled circle.
func (s *Builder) dot(x, y, r float64, col string) {
	s.p(`<circle cx="%g" cy="%g" r="%g" fill="%s"/>`, x, y, r, col)
}

// poly draws a (optionally dashed) polyline through the given x,y pairs.
func (s *Builder) poly(col string, wdt float64, dash string, pts ...float64) {
	d := ""
	if dash != "" {
		d = ` stroke-dasharray="` + dash + `"`
	}
	s.p(`<polyline fill="none" stroke="%s" stroke-width="%g" stroke-linejoin="round"%s points="`, col, wdt, d)
	for i := 0; i < len(pts); i += 2 {
		s.p("%g,%g ", pts[i], pts[i+1])
	}
	s.p(`"/>`)
}
