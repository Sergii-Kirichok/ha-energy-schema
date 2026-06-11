package scada

import (
	"math"
	"time"
)

const (
	flowBaseSpeed  = 29.07 // px/сек при нулевой нагрузке (медленно; −5% и ещё −10%)
	flowLoadSpeed  = 7.695 // +px/сек на каждый кВт (больше нагрузка → быстрее)
	flowMaxSpeed   = 102.6 // потолок скорости, px/сек
	flowDotSpacing = 95.0  // целевой интервал между стрелками, px
)

// animClock returns the current animation time in seconds. Overridden to a
// constant in tests so the golden render is deterministic.
var animClock = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }

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
	// направление потока: к получателю (или обратно при reverse — отдача)
	seq := pts
	if reverse {
		seq = revPts(pts)
	}
	speed := flowBaseSpeed + magKW*flowLoadSpeed // px/сек, одинаково по всей линии
	if speed > flowMaxSpeed {
		speed = flowMaxSpeed
	}
	L := pathLen(seq)
	n := int(L/flowDotSpacing + 0.5)
	if n < 1 {
		n = 1
	}
	// стрелки маршируют вдоль линии: смещение запекается из текущего времени
	// (rsvg/ТВ не играет SMIL, поэтому позиции статичны и двигаются между кадрами).
	off := math.Mod(s.phase*speed/L, 1.0)
	for k := 0; k < n; k++ {
		f := math.Mod(off+float64(k)/float64(n), 1.0)
		px, py, dx, dy := pointDir(seq, f*L)
		s.arrow(px, py, dx, dy, col)
	}
}
