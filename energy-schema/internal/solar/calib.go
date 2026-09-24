package solar

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"sync"
	"time"
)

// Самокалибровка прогноза: учим мультипликативные поправки «сырой прогноз →
// факт» с ПРИВЯЗКОЙ К ПОГОДЕ. Два слоя:
//   KBin  — поправка по корзинам индекса ясности Kp = сырой/ясно-небесный
//           (отдельно на источник: open-meteo / met.no). Это и есть учёт погоды:
//           дождливый час сравнивается с дождливым прогнозом, ясный с ясным.
//   Level — общий суточный уровень (грязь/снег/деградация), учится на УЖЕ
//           скорректированном KBin прогнозе → не дублирует масштаб (правка ревью).
// Холодный старт: пусто → поправки ≈ 1 (байесовское сжатие к 1 по весу W).

type cell struct {
	SumF float64 `json:"sf"`
	SumA float64 `json:"sa"`
	W    float64 `json:"w"`
}

func (cl cell) ratio(w0 float64) float64 {
	if cl.W <= 0 || cl.SumF <= 0 {
		return 1
	}
	r := clampf(cl.SumA/cl.SumF, 1.0/3, 3)
	return math.Exp(cl.W / (cl.W + w0) * math.Log(r)) // сжатие к 1 при малом W
}

type hourPred struct {
	Raw  float64 `json:"r"` // замороженный сырой прогноз часа, кВт·ч
	Eclr float64 `json:"e"` // ясно-небесный потолок часа, кВт·ч
	Src  string  `json:"s"`
}

type dayRec struct {
	Day     string  `json:"d"`
	PredRaw float64 `json:"pr"`
	PredCal float64 `json:"pc"`
	Act     float64 `json:"a"`
}

type calibData struct {
	KBin       map[string][6]cell  `json:"kbin"`
	Level      cell                `json:"level"`
	Pending    map[string]hourPred `json:"pending"`
	Days       []dayRec            `json:"days"`
	DayYMD     string              `json:"day_ymd"`
	DayPredRaw float64             `json:"dpr"`
	DayPredCal float64             `json:"dpc"`
	DayAct     float64             `json:"da"`
	LastHour   int64               `json:"last_hour"`
	GeoHash    string              `json:"geo"`
	V          int                 `json:"v"`
}

// Calibrator is concurrency-safe; the provider calls Apply/Freeze, the server
// calls ObserveDue/Save on timers.
type Calibrator struct {
	mu   sync.Mutex
	d    calibData
	path string
}

const (
	kbinW0    = 5.0
	levelW0   = 3.0
	decayKBin = 0.97716 // 0.5^(1/30) — halflife 30 дней (смещение погодной модели стабильно)
	decayLvl  = 0.87055 // 0.5^(1/5)  — halflife 5 дней (грязь/снег приходят-уходят)
	eclrMin   = 0.5     // кВт·ч — ниже = ночь/сумерки, не учим
	covMin    = 10      // минимум 5-мин корзин из 12 (рестарт не учим как ноль)
	socFull   = 98.0    // % — полная батарея могла резать PV → час пропускаем
)

func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func kpBin(kp float64) int {
	switch {
	case kp < 0.15:
		return 0
	case kp < 0.35:
		return 1
	case kp < 0.55:
		return 2
	case kp < 0.75:
		return 3
	case kp < 0.92:
		return 4
	default:
		return 5
	}
}

// GeoHash — отпечаток геометрии массивов; при изменении калибровка сбрасывается
// (выученное относилось к старой конфигурации).
func GeoHash(arrays []Array) string {
	s := ""
	for _, a := range arrays {
		s += fmt.Sprintf("%.2f/%.0f/%.0f/%.2f;", a.KWp, a.TiltDeg, a.AzDeg, a.Bifacial)
	}
	return s
}

// NewCalibrator loads /data/calib.json; if the geometry hash differs, starts clean.
func NewCalibrator(path, geoHash string) *Calibrator {
	c := &Calibrator{path: path, d: calibData{KBin: map[string][6]cell{}, Pending: map[string]hourPred{}, GeoHash: geoHash, V: 1}}
	b, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// первый запуск — холодный старт, это норма
	case err != nil:
		log.Printf("calib: read %s: %v (cold start)", path, err)
	default:
		var d calibData
		if err := json.Unmarshal(b, &d); err != nil {
			log.Printf("calib: %s corrupt: %v (reset)", path, err)
		} else if d.GeoHash != geoHash {
			log.Printf("calib: geometry changed (%s -> %s), reset", d.GeoHash, geoHash)
		} else {
			if d.KBin == nil {
				d.KBin = map[string][6]cell{}
			}
			if d.Pending == nil {
				d.Pending = map[string]hourPred{}
			}
			c.d = d
		}
	}
	c.d.GeoHash = geoHash
	return c
}

// Apply returns the calibrated hourly kWh for one hour (raw forecast eRaw, clear-
// sky ceiling eClr, source src). Cold start → ≈ eRaw.
func (c *Calibrator) Apply(src string, eRaw, eClr float64) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	rl := clampf(c.d.Level.ratio(levelW0), 0.5, 1.3)
	rk := 1.0
	if eClr > 0 {
		kp := clampf(eRaw/eClr, 0, 1.1)
		b := c.d.KBin[src]
		rk = clampf(b[kpBin(kp)].ratio(kbinW0), 0.5, 2.2)
	}
	v := eRaw * rk * rl
	if eClr > 0 && v > 1.3*eClr {
		v = 1.3 * eClr
	}
	if v < 0 {
		v = 0
	}
	return v
}

// Freeze records the raw forecast for a future hour (overwritten each refresh →
// we learn on the LAST forecast issued before the hour began).
func (c *Calibrator) Freeze(hourStart int64, eRaw, eClr float64, src string) {
	c.mu.Lock()
	c.d.Pending[strconv.FormatInt(hourStart, 10)] = hourPred{Raw: eRaw, Eclr: eClr, Src: src}
	c.mu.Unlock()
}

// ObserveDue learns from hours that have finished. energy(hourUnix)->(kWh,
// coverage 0..12); socMax(hourUnix)->(max SOC %, ok). now is local time.
func (c *Calibrator) ObserveDue(now time.Time, energy func(int64) (float64, int), socMax func(int64) (float64, bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := now.Add(-65 * time.Minute).Unix() // час закончился ≥5 мин назад
	if c.d.LastHour == 0 {
		c.d.LastHour = now.Add(-2 * time.Hour).Truncate(time.Hour).Unix()
	}
	for h := c.d.LastHour + 3600; h <= cutoff; h += 3600 {
		ht := time.Unix(h, 0).In(now.Location())
		if ymd := ht.Format("2006-01-02"); ymd != c.d.DayYMD {
			c.rollDay() // закрыть предыдущий день (Level + затухание)
			c.d.DayYMD = ymd
		}
		c.d.LastHour = h
		key := strconv.FormatInt(h, 10)
		p, ok := c.d.Pending[key]
		delete(c.d.Pending, key) // не копим утечку независимо от исхода
		if !ok || p.Eclr < eclrMin {
			continue // нет прогноза / ночь
		}
		eAct, cov := energy(h)
		if cov < covMin {
			continue // рестарт/пропуски — не учим
		}
		if sm, sok := socMax(h); sok && sm >= socFull {
			continue // батарея была полной → PV могла резаться, факт занижен
		}
		if eAct > 1.3*p.Eclr {
			eAct = 1.3 * p.Eclr // winsorize выбросов
		}
		kp := clampf(p.Raw/p.Eclr, 0, 1.1)
		bi := kpBin(kp)
		b := c.d.KBin[p.Src]
		b[bi].SumF += p.Raw
		b[bi].SumA += eAct
		b[bi].W += 1
		c.d.KBin[p.Src] = b
		// аккумулятор дня для Level — на KBin-СКОРРЕКТИРОВАННОМ прогнозе (без двойного учёта)
		rk := clampf(b[bi].ratio(kbinW0), 0.5, 2.2)
		c.d.DayPredRaw += p.Raw
		c.d.DayPredCal += p.Raw * rk
		c.d.DayAct += eAct
	}
	// чистка старых pending (>48ч) — на случай рестартов с пропусками
	old := now.Add(-48 * time.Hour).Unix()
	for k := range c.d.Pending {
		if v, _ := strconv.ParseInt(k, 10, 64); v < old {
			delete(c.d.Pending, k)
		}
	}
}

// rollDay closes the accumulated day: decays + updates Level on the KBin-corrected
// day forecast, decays KBin, appends a Days record. Called once per day boundary.
func (c *Calibrator) rollDay() {
	if c.d.DayYMD != "" && c.d.DayPredCal > 0.5 { // был содержательный день
		c.d.Level.SumF = c.d.Level.SumF*decayLvl + c.d.DayPredCal
		c.d.Level.SumA = c.d.Level.SumA*decayLvl + c.d.DayAct
		c.d.Level.W = c.d.Level.W*decayLvl + 1
		for s, b := range c.d.KBin {
			for i := range b {
				b[i].SumF *= decayKBin
				b[i].SumA *= decayKBin
				b[i].W *= decayKBin
			}
			c.d.KBin[s] = b
		}
		c.d.Days = append(c.d.Days, dayRec{Day: c.d.DayYMD, PredRaw: c.d.DayPredRaw, PredCal: c.d.DayPredCal, Act: c.d.DayAct})
		if len(c.d.Days) > 60 {
			c.d.Days = c.d.Days[len(c.d.Days)-60:]
		}
	}
	c.d.DayPredRaw, c.d.DayPredCal, c.d.DayAct = 0, 0, 0
}

// Status returns level weight (effective days), level correction, day count and
// 7-day MAE (calibrated vs actual) — for logging/observability.
func (c *Calibrator) Status() (levelW, rLevel float64, days int, mae7 float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rLevel = clampf(c.d.Level.ratio(levelW0), 0.5, 1.3)
	levelW = c.d.Level.W
	days = len(c.d.Days)
	cnt := 0
	sum := 0.0
	for i := len(c.d.Days) - 1; i >= 0 && cnt < 7; i-- {
		sum += math.Abs(c.d.Days[i].PredCal - c.d.Days[i].Act)
		cnt++
	}
	if cnt > 0 {
		mae7 = sum / float64(cnt)
	}
	return
}

// Save atomically persists the model.
func (c *Calibrator) Save() error {
	c.mu.Lock()
	b, err := json.Marshal(c.d)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.path+".tmp", b, 0644); err != nil {
		return err
	}
	return os.Rename(c.path+".tmp", c.path)
}
