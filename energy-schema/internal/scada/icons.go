package scada

// icon draws a glyph (~13px) centered at (ix,iy) in color col.
func (s *Builder) icon(kind string, ix, iy float64, col string) {
	switch kind {
	case "fish":
		s.p(`<g fill="%s"><ellipse cx="%g" cy="%g" rx="11" ry="6.5"/><polygon points="%g,%g %g,%g %g,%g"/></g><circle cx="%g" cy="%g" r="1.5" fill="#0f1115"/>`, col, ix-2, iy, ix+8, iy, ix+17, iy-6, ix+17, iy+6, ix-7, iy-1.5)
	case "sine":
		s.p(`<path d="M %g %g c 3,-13 8,-13 11,0 c 3,13 8,13 11,0" fill="none" stroke="%s" stroke-width="2.5"/>`, ix-13, iy, col)
	case "inv":
		s.p(`<rect x="%g" y="%g" width="26" height="22" rx="3" fill="none" stroke="%s" stroke-width="2"/><line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="2"/><path d="M %g %g c 2,-5 5,-5 6,0 c 1,5 4,5 6,0" fill="none" stroke="%s" stroke-width="1.8"/>`, ix-13, iy-11, col, ix, iy-9, ix, iy+9, col, ix+2, iy, col)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.8"/>`, ix-9, iy-3, ix-3, iy-3, col)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.8"/>`, ix-9, iy+3, ix-3, iy+3, col)
	case "gen":
		s.p(`<circle cx="%g" cy="%g" r="12" fill="none" stroke="%s" stroke-width="2"/><text x="%g" y="%g" font-size="14" font-weight="bold" fill="%s" text-anchor="middle">G</text>`, ix, iy, col, ix, iy+5, col)
	case "genrun":
		s.p(`<circle cx="%g" cy="%g" r="12" fill="none" stroke="%s" stroke-width="2"/><text x="%g" y="%g" font-size="14" font-weight="bold" fill="%s" text-anchor="middle">G</text>`, ix, iy, col, ix, iy+5, col)
		for i := 0; i < 3; i++ {
			cx := ix - 4 + float64(i)*5
			s.p(`<circle cx="%g" cy="%g" r="3" fill="#9ca3af"><animate attributeName="cy" values="%g;%g" dur="1.6s" repeatCount="indefinite" begin="%.1fs"/><animate attributeName="opacity" values="0.7;0" dur="1.6s" repeatCount="indefinite" begin="%.1fs"/></circle>`, cx, iy-14, iy-14, iy-26, float64(i)*0.5, float64(i)*0.5)
		}
	case "home":
		s.p(`<path d="M %g %g L %g %g L %g %g L %g %g L %g %g Z" fill="none" stroke="%s" stroke-width="2"/>`, ix-12, iy+10, ix-12, iy-2, ix, iy-12, ix+12, iy-2, ix+12, iy+10, col)
	case "batt":
		s.p(`<rect x="%g" y="%g" width="22" height="16" rx="2" fill="none" stroke="%s" stroke-width="2"/><rect x="%g" y="%g" width="3" height="8" fill="%s"/>`, ix-12, iy-8, col, ix+10, iy-4, col)
	case "sun":
		s.p(`<circle cx="%g" cy="%g" r="7" fill="none" stroke="%s" stroke-width="2"/>`, ix, iy, col)
		for a := 0; a < 8; a++ {
			x1, y1 := pt(ix, iy, 10, float64(a)*45)
			x2, y2 := pt(ix, iy, 14, float64(a)*45)
			s.p(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2"/>`, x1, y1, x2, y2, col)
		}
	case "leaf":
		s.p(`<path d="M %g %g q 14,-16 0,-22 q -14,6 0,22 Z" fill="none" stroke="%s" stroke-width="2"/>`, ix, iy+10, col)
	case "sw":
		// transfer switch: 2 входа сверху, 1 выход снизу, нож на выбранный вход
		s.p(`<circle cx="%g" cy="%g" r="2.6" fill="%s"/><circle cx="%g" cy="%g" r="2.6" fill="%s"/><circle cx="%g" cy="%g" r="2.6" fill="%s"/>`, ix-9, iy-8, col, ix+9, iy-8, col, ix, iy+10, col)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.8"/><line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.8"/>`, ix-9, iy-8, ix-9, iy-13, col, ix+9, iy-8, ix+9, iy-13, col)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="2.6"/>`, ix, iy+10, ix-9, iy-6, col)
	case "regen":
		// двунаправленный знак (импорт/отдача = регенерация)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="2"/><polygon points="%g,%g %g,%g %g,%g" fill="%s"/>`, ix-6, iy+9, ix-6, iy-6, col, ix-6, iy-11, ix-10, iy-4, ix-2, iy-4, col)
		s.p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="2"/><polygon points="%g,%g %g,%g %g,%g" fill="%s"/>`, ix+6, iy-9, ix+6, iy+6, col, ix+6, iy+11, ix+10, iy+4, ix+2, iy+4, col)
	}
}

// head draws a card header: icon + title, with an optional status dot.
func (s *Builder) head(x, y, w float64, kind, ttl, statusCol string) {
	s.icon(kind, x+22, y+22, cTxt)
	s.t(x+44, y+27, 14, cTxt, "start", ttl)
	if statusCol != "" {
		s.dot(x+w-16, y+18, 6, statusCol)
	}
}

// wiconKind maps a HA weather condition to a glyph kind.
func wiconKind(c string) string {
	switch c {
	case "sunny", "clear-night":
		return "sun"
	case "partlycloudy":
		return "partly"
	case "rainy", "pouring", "snowy-rainy":
		return "rain"
	case "lightning", "lightning-rainy":
		return "storm"
	case "snowy", "hail":
		return "snow"
	case "windy", "windy-variant":
		return "wind"
	case "fog":
		return "fog"
	default: // cloudy, exceptional, unknown
		return "cloud"
	}
}

// wicon draws a small weather glyph for a HA condition, centered at (ix,iy).
func (s *Builder) wicon(cond string, ix, iy float64) {
	sun := func(cx, cy, r float64) {
		s.p(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s"/>`, cx, cy, r, cAmb)
		for a := 0; a < 8; a++ {
			x1, y1 := pt(cx, cy, r+2.5, float64(a)*45)
			x2, y2 := pt(cx, cy, r+5, float64(a)*45)
			s.p(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1.8"/>`, x1, y1, x2, y2, cAmb)
		}
	}
	cloud := func(cx, cy float64) {
		s.p(`<circle cx="%.1f" cy="%.1f" r="5" fill="%s"/><circle cx="%.1f" cy="%.1f" r="7" fill="%s"/><circle cx="%.1f" cy="%.1f" r="5.5" fill="%s"/>`,
			cx-7, cy+1, cSub, cx, cy-3, cSub, cx+7, cy+1, cSub)
	}
	switch wiconKind(cond) {
	case "sun":
		sun(ix, iy, 6)
	case "partly":
		sun(ix-4, iy-4, 4)
		cloud(ix+3, iy+3)
	case "rain":
		cloud(ix, iy-2)
		for i := 0; i < 3; i++ {
			x := ix - 7 + float64(i)*7
			s.p(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2" stroke-linecap="round"/>`, x, iy+6, x-2, iy+12, cBlu)
		}
	case "storm":
		cloud(ix, iy-2)
		s.p(`<polygon points="%.1f,%.1f %.1f,%.1f %.1f,%.1f %.1f,%.1f %.1f,%.1f" fill="%s"/>`,
			ix+2, iy+5, ix-3, iy+11, ix, iy+11, ix-2, iy+16, ix+5, iy+8, cAmb)
	case "snow":
		cloud(ix, iy-2)
		for i := 0; i < 3; i++ {
			x := ix - 7 + float64(i)*7
			s.p(`<circle cx="%.1f" cy="%.1f" r="1.7" fill="#e5e7eb"/>`, x, iy+11)
		}
	case "wind":
		for i := 0; i < 3; i++ {
			y := iy - 4 + float64(i)*5
			s.p(`<path d="M %.1f %.1f h 11 a 3.2 3.2 0 1 1 -3 3.2" fill="none" stroke="%s" stroke-width="2" stroke-linecap="round"/>`, ix-8, y, cSub)
		}
	case "fog":
		for i := 0; i < 4; i++ {
			y := iy - 6 + float64(i)*4
			s.p(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2" stroke-linecap="round"/>`, ix-9, y, ix+9, y, cSub)
		}
	default:
		cloud(ix, iy)
	}
}
