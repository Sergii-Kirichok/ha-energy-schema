// Command icongen renders the add-on icon.png (128×128) and logo.png (250×100):
// a dark rounded square with a red/orange/green 180° gauge arc and a lightning
// bolt. Run from energy-schema/: go run ./tools/icongen
// Not part of the add-on image (the Dockerfile builds only ./cmd/energy-schema).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

var (
	cBG     = color.RGBA{0x0f, 0x11, 0x15, 0xff}
	cRed    = color.RGBA{0xef, 0x44, 0x44, 0xff}
	cOrange = color.RGBA{0xf5, 0x9e, 0x0b, 0xff}
	cGreen  = color.RGBA{0x22, 0xc5, 0x5e, 0xff}
	cBolt   = color.RGBA{0xf5, 0xb3, 0x00, 0xff}
	bolt    = [][2]float64{{68, 52}, {52, 78}, {64, 78}, {58, 98}, {78, 68}, {66, 68}}
)

// sample returns the colour at point (x,y) of the 128×128 viewBox.
func sample(x, y float64) color.RGBA {
	if inPoly(x, y, bolt) {
		return cBolt
	}
	const cx, cy, r, sw = 64.0, 86.0, 42.0, 12.0
	if d := math.Hypot(x-cx, y-cy); y <= cy && d >= r-sw/2 && d <= r+sw/2 {
		t := 1 - math.Atan2(cy-y, x-cx)/math.Pi // 0 at 180° (left), 1 at 0° (right)
		switch {
		case t < 0.20:
			return cRed
		case t < 0.32:
			return cOrange
		default:
			return cGreen
		}
	}
	if inRoundRect(x, y, 128, 28) {
		return cBG
	}
	return color.RGBA{}
}

// inRoundRect: (x,y) inside a size×size square at the origin with corner radius rad.
func inRoundRect(x, y, size, rad float64) bool {
	if x < 0 || y < 0 || x > size || y > size {
		return false
	}
	qx := math.Max(math.Max(rad-x, x-(size-rad)), 0)
	qy := math.Max(math.Max(rad-y, y-(size-rad)), 0)
	return qx*qx+qy*qy <= rad*rad
}

// inPoly is the even-odd point-in-polygon test.
func inPoly(x, y float64, p [][2]float64) bool {
	in := false
	for i, j := 0, len(p)-1; i < len(p); j, i = i, i+1 {
		xi, yi, xj, yj := p[i][0], p[i][1], p[j][0], p[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// render draws the mark scaled by s (pixels per viewBox unit) with its origin
// at pixel (ox,oy), 4×4 supersampled; alpha is averaged premultiplied.
func render(w, h int, s, ox, oy float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	const n = 4
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			var r, g, b, a float64
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/n - ox) / s
					y := (float64(py) + (float64(sy)+0.5)/n - oy) / s
					c := sample(x, y)
					f := float64(c.A) / 255
					r, g, b, a = r+float64(c.R)*f, g+float64(c.G)*f, b+float64(c.B)*f, a+float64(c.A)
				}
			}
			k := float64(n * n)
			img.SetRGBA(px, py, color.RGBA{uint8(r/k + 0.5), uint8(g/k + 0.5), uint8(b/k + 0.5), uint8(a/k + 0.5)})
		}
	}
	return img
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	write("icon.png", render(128, 128, 1, 0, 0))
	write("logo.png", render(250, 100, 100.0/128, 75, 0))
}
