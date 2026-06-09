package scada

// flow draws an energy path between points (flat x,y pairs) by state:
//   - "off": grey dashed line
//   - "bad": red dashed line with a ✕ at the midpoint
//   - otherwise: solid colored line with moving dots whose speed scales with
//     magKW (bigger flow → faster). reverse flips the dot direction.
func (s *Builder) flow(col, st string, magKW float64, reverse bool, pts ...float64) {
	if st == "off" {
		s.poly(cGry, 2, "7 7", pts...)
		return
	}
	if st == "bad" {
		s.poly(cRed, 2.5, "7 7", pts...)
		mx, my := pts[len(pts)/2/2*2], pts[len(pts)/2/2*2+1]
		s.t(mx, my-6, 17, cRed, "middle", "✕")
		return
	}
	s.poly(col, 3, "", pts...)
	pd := pathD(pts)
	if reverse {
		pd = pathD(revPts(pts))
	}
	dur := 2.6 - magKW*0.12
	if dur < 0.5 {
		dur = 0.5
	}
	if dur > 2.6 {
		dur = 2.6
	}
	for k := 0; k < 3; k++ {
		s.p(`<circle r="4.5" fill="%s"><animateMotion dur="%.2fs" repeatCount="indefinite" begin="-%.2fs" path="%s"/></circle>`, col, dur, float64(k)*dur/3, pd)
	}
}
