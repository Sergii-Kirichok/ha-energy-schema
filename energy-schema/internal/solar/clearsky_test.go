package solar

import (
	"math"
	"testing"
	"time"
)

var testLoc = Location{Lat: 50.45, Lon: 30.52, AltM: 150} // Kyiv

// На летнее солнцестояние высота солнца в истинный полдень = 90 − φ + 23.44.
func TestSolsticeNoonElevation(t *testing.T) {
	date := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	noon := SolarNoon(date, testLoc)
	el, _ := SunPos(noon, testLoc.Lat, testLoc.Lon)
	want := 90 - testLoc.Lat + 23.44
	if math.Abs(el-want) > 1.5 {
		t.Fatalf("высота солнца в полдень солнцестояния = %.2f°, ожидалось ~%.2f°", el, want)
	}
}

// Вертикальный массив на восток (Забор: 90°, азимут ~76°) летом должен пиковать
// УТРОМ и обрываться после полудня, а не давать симметричный «колокол».
func TestVerticalEastPeaksMorning(t *testing.T) {
	zabor := []Array{{Name: "Забор", KWp: 7.44, TiltDeg: 90, AzDeg: 76}}
	tz := time.FixedZone("EEST", 3*3600)
	date := time.Date(2026, 6, 21, 0, 0, 0, 0, tz)
	h := HourlyClearSky(date, testLoc, zabor, 33)
	peak := 0
	for i := 1; i < 24; i++ {
		if h[i] > h[peak] {
			peak = i
		}
	}
	if peak >= 13 {
		t.Fatalf("вертикальный восточный массив пикует в час %d, ожидалось утро (<13)", peak)
	}
	// после 16:00 прямого солнца на восточную плоскость практически нет
	if h[17] > 0.35*h[peak] {
		t.Fatalf("вертикальный восток в 17:00 = %.2f кВт·ч (пик %.2f) — солнце должно уже уйти за плоскость", h[17], h[peak])
	}
}

// Bifacial-прибавка увеличивает выработку и НЕ обрезается номиналом kWp.
func TestBifacialExceedsNameplate(t *testing.T) {
	// высокое переоблучение при ясном небе на хорошо ориентированной панели
	mono := Array{Name: "m", KWp: 6.45, TiltDeg: 30, AzDeg: 180}
	bif := mono
	bif.Bifacial = 0.15
	p := 1050.0 // POA выше 1000 — переоблучение реально бывает
	pm := ArrayPowerKW(mono, p, 10)
	pb := ArrayPowerKW(bif, p, 10)
	if pb <= pm {
		t.Fatalf("bifacial должен давать больше: mono=%.2f bif=%.2f", pm, pb)
	}
	if pb <= bif.KWp {
		t.Fatalf("выработка bifacial при переоблучении должна превышать номинал %.2f kWp, получено %.2f", bif.KWp, pb)
	}
}

// Южный наклонный массив симметричен относительно солнечного полудня.
func TestSouthArraySymmetric(t *testing.T) {
	south := []Array{{Name: "s", KWp: 6.0, TiltDeg: 35, AzDeg: 180}}
	tz := time.FixedZone("EEST", 3*3600)
	date := time.Date(2026, 6, 21, 0, 0, 0, 0, tz)
	h := HourlyClearSky(date, testLoc, south, 0)
	// солнечный полдень в Киеве летом ~13ч локального; сравним симметричные часы
	if math.Abs(h[10]-h[16]) > 0.25*h[13] {
		t.Fatalf("южный массив должен быть ~симметричен: h10=%.2f h16=%.2f h13=%.2f", h[10], h[16], h[13])
	}
}
