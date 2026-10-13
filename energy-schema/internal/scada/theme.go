// Package scada renders the live single-line diagram (SVG) of the energy
// system from a snapshot of Home Assistant entity states.
package scada

import "fmt"

// Palette — dark SCADA theme.
const (
	cTxt  = "#e5e7eb"
	cSub  = "#9ca3af"
	cBox  = "#171a20"
	cBrd  = "#2b2f38"
	cGrn  = "#22c55e"
	cGry  = "#6b7280"
	cRed  = "#ef4444"
	cRed2 = "#b91c1c" // тёмно-красный — зона перегруза (33–45 кВт, пик 10 с)
	cOrg  = "#f59e0b"
	cAmb  = "#f5b300"
	cBlu  = "#60a5fa"
	cPur  = "#a78bfa"
	cCyn  = "#22d3ee" // голубой — маркер минимума (заряд/трофа), в пару к красному пику
)

// band is a colored zone of a gauge/bar, filled up to threshold thr.
type band struct {
	thr float64
	col string
}

// kw formats watts as kilowatts ("X.XX кВт").
func kw(w float64) string { return fmt.Sprintf("%.2f кВт", w/1000) }

// dimCol — цвет зоны «за пределом уровня»: 30 % цвета на фоне дорожки
// (#23272f), явным hex — 8-значные #RRGGBBAA старые ТВ-браузеры не понимают.
func dimCol(hex string) string {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return "#23272f"
	}
	mix := func(c, bg int) int { return (c*3 + bg*7) / 10 }
	return fmt.Sprintf("#%02x%02x%02x", mix(r, 0x23), mix(g, 0x27), mix(b, 0x2f))
}
