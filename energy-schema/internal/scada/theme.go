// Package scada renders the live single-line diagram (SVG) of the energy
// system from a snapshot of Home Assistant entity states.
package scada

import "fmt"

// Palette — dark SCADA theme.
const (
	cTxt = "#e5e7eb"
	cSub = "#9ca3af"
	cBox = "#171a20"
	cBrd = "#2b2f38"
	cGrn = "#22c55e"
	cGry = "#6b7280"
	cRed = "#ef4444"
	cOrg = "#f59e0b"
	cAmb = "#f5b300"
	cBlu = "#60a5fa"
	cPur = "#a78bfa"
)

// band is a colored zone of a gauge/bar, filled up to threshold thr.
type band struct {
	thr float64
	col string
}

// kw formats watts as kilowatts ("X.XX кВт").
func kw(w float64) string { return fmt.Sprintf("%.2f кВт", w/1000) }
