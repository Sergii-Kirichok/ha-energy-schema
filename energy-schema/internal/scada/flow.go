package scada

const (
	flowDotSpacing = 95.0 // целевой интервал между стрелками-направлениями, px
)

// arrow draws a small filled triangle (flow direction marker) centred at
// (px,py), pointing along the unit direction (dx,dy).
func (s *Builder) arrow(px, py, dx, dy float64, col string) {
	const ahead, back, half = 5.5, 3.5, 4.0
	nx, ny := -dy, dx // перпендикуляр
	s.p(`<polygon points="%.1f,%.1f %.1f,%.1f %.1f,%.1f" fill="%s"/>`,
		px+dx*ahead, py+dy*ahead,
		px-dx*back+nx*half, py-dy*back+ny*half,
		px-dx*back-nx*half, py-dy*back-ny*half, col)
}

// flow draws an energy path between points (flat x,y pairs) by state:
//   - "off":  grey dashed line (deliberately not powered)
//   - "bad":  red dashed line with a ✕ at the midpoint (real break/fault)
//   - "lost": orange dashed line with a «?» at the midpoint (sensor link lost,
//     line probably still live)
//   - otherwise: solid colored line with STATIC direction arrows (triangles)
//     showing which way current flows; reverse flips the direction. magKW is
//     kept for signature compatibility (no longer affects animation).
func (s *Builder) flow(col, st string, magKW float64, reverse bool, pts ...float64) {
	if st == "off" {
		s.poly(cGry, 2, "7 7", pts...)
		return
	}
	if st == "bad" {
		s.poly(cRed, 2.5, "7 7", pts...)
		// ✕ ровно на середине линии (по длине пути), тёмная подложка маскирует пунктир
		mx, my := pointAt(pts, pathLen(pts)/2)
		const xr = 7.0
		s.p(`<circle cx="%.1f" cy="%.1f" r="11" fill="#0f1115"/>`, mx, my)
		s.p(`<path d="M %.1f,%.1f L %.1f,%.1f M %.1f,%.1f L %.1f,%.1f" stroke="%s" stroke-width="3" stroke-linecap="round"/>`,
			mx-xr, my-xr, mx+xr, my+xr, mx-xr, my+xr, mx+xr, my-xr, cRed)
		return
	}
	if st == "lost" {
		s.poly(cOrg, 2.5, "6 5", pts...)
		mx, my := pointAt(pts, pathLen(pts)/2)
		s.p(`<circle cx="%.1f" cy="%.1f" r="10" fill="#0f1115" stroke="%s" stroke-width="1.5"/>`, mx, my, cOrg)
		s.t(mx, my+5, 15, cOrg, "middle", "?")
		return
	}
	s.poly(col, 3, "", pts...)
	// направление потока: к получателю (или обратно при reverse — отдача).
	// Стрелки СТАТИЧНЫЕ — показывают только направление тока, без движения.
	seq := pts
	if reverse {
		seq = revPts(pts)
	}
	L := pathLen(seq)
	n := int(L/flowDotSpacing + 0.5)
	if n < 1 {
		n = 1
	}
	for k := 0; k < n; k++ {
		f := (float64(k) + 0.5) / float64(n)
		px, py, dx, dy := pointDir(seq, f*L)
		s.arrow(px, py, dx, dy, col)
	}
}
