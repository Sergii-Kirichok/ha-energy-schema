package scada

const (
	flowBaseSpeed  = 34.0  // px/сек при нулевой нагрузке (медленно)
	flowLoadSpeed  = 9.0   // +px/сек на каждый кВт (больше нагрузка → быстрее)
	flowMaxSpeed   = 120.0 // потолок скорости, px/сек
	flowDotSpacing = 110.0 // целевой интервал между кружочками, px
)

// flow draws an energy path between points (flat x,y pairs) by state:
//   - "off":  grey dashed line (deliberately not powered)
//   - "bad":  red dashed line with a ✕ at the midpoint (real break/fault)
//   - "lost": orange dashed line with a «?» at the midpoint (sensor link lost,
//     line probably still live)
//   - otherwise: solid colored line with moving dots at a UNIFORM linear speed
//     (px/sec, independent of line length); more load (magKW) → a bit faster.
//     Dot count scales with length so spacing stays constant. reverse flips dir.
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
	pd := pathD(pts)
	if reverse {
		pd = pathD(revPts(pts))
	}
	speed := flowBaseSpeed + magKW*flowLoadSpeed // px/сек, одинаково по всей линии
	if speed > flowMaxSpeed {
		speed = flowMaxSpeed
	}
	L := pathLen(pts)
	dur := L / speed // длиннее линия → дольше проход (но та же скорость)
	n := int(L/flowDotSpacing + 0.5)
	if n < 1 {
		n = 1
	}
	for k := 0; k < n; k++ {
		s.p(`<circle r="4.5" fill="%s"><animateMotion dur="%.2fs" repeatCount="indefinite" begin="-%.2fs" path="%s"/></circle>`, col, dur, float64(k)*dur/float64(n), pd)
	}
}
