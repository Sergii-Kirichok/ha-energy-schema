package web

import (
	"math"
	"time"
)

// Журнал разбаланса «заряд через колено»: подъём — непрерывный заряд (ток
// < −0,5 А, паузы до 5 мин не рвут его), за который верхняя ячейка лезет
// вверх. На каждом подъёме фиксируем первые пересечения верхней ячейкой
// уровней 3,40…3,55 В (время, разбег, ток, где крайние ячейки) и «отставание
// в А·ч»: сколько заряда влили от пересечения уровня верхней ячейкой до
// пересечения того же уровня нижней. Объективная запись для Deye.
var (
	imbLevels    = []float64{3.40, 3.45, 3.50, 3.55}
	imbLagLevels = []float64{3.375, 3.40, 3.45}
)

const (
	imbChargeA   = -0.5 // заряд: ток ниже (HA: «−» — заряд)
	imbStopAfter = 5 * time.Minute
	imbTopV      = 3.55
	imbRecordV   = 3.40 // подъёмы, где верхняя ячейка не дошла сюда, не пишем
	imbMaxDt     = 10 * time.Minute
	imbAfterRest = "after_pause"
	imbFromFlat  = "from_flat"
)

// imbSample — одна выборка: напряжения в В, ток в А (− заряд), лимит заряда
// BMS в А (NaN — неизвестен), фаза балансировки ("" — выкл).
type imbSample struct {
	T                time.Time
	Max, Min, I, SOC float64
	Limit            float64
	MaxLoc, MinLoc   string
	Phase            string
}

func (s imbSample) valid() bool {
	return !math.IsNaN(s.Max) && !math.IsNaN(s.Min) && !math.IsNaN(s.I) && s.Max > 2 && s.Min > 2
}

func (s imbSample) deltaMV() int { return int(math.Round((s.Max - s.Min) * 1000)) }

type imbLevel struct {
	V        float64   `json:"v"`
	Time     time.Time `json:"time"`
	DeltaMV  int       `json:"delta_mv"`
	CurrentA float64   `json:"current_a"`
	MaxLoc   string    `json:"max_loc"`
	MinLoc   string    `json:"min_loc"`
}

type imbLag struct {
	V       float64    `json:"v"`
	Ah      float64    `json:"ah"`
	Reached bool       `json:"reached"` // false — нижняя не дошла до конца подъёма, Ah — «не меньше»
	MaxAt   time.Time  `json:"max_at"`
	MinAt   *time.Time `json:"min_at,omitempty"`
}

type imbRecord struct {
	Start       time.Time  `json:"start"`
	End         time.Time  `json:"end"`
	Source      string     `json:"source"` // live / history
	Type        string     `json:"type"`   // after_pause / from_flat
	StartSOC    float64    `json:"start_soc"`
	StartMaxV   float64    `json:"start_max_v"`
	Balancing   bool       `json:"balancing"`
	Phase       string     `json:"phase,omitempty"`
	PeakMaxV    float64    `json:"peak_max_v"`
	PeakDeltaMV int        `json:"peak_delta_mv"`
	EndReason   string     `json:"end_reason"` // top / bms_limit / stopped
	Levels      []imbLevel `json:"levels"`
	AhLag       []imbLag   `json:"ah_lag"`
}

// imbTracker — чистая логика подъёмов; кормится выборками по времени.
type imbTracker struct {
	source  string
	r       *imbRecord // текущий подъём
	last    imbSample
	has     bool
	lastChg time.Time // последняя выборка с зарядом в подъёме
	topped  bool      // подъём кончился верхом: новый — только после остановки заряда
}

// feed учитывает выборку; возвращает завершённую запись (или nil).
func (t *imbTracker) feed(s imbSample) *imbRecord {
	if !s.valid() {
		return nil
	}
	prev, had := t.last, t.has
	t.last, t.has = s, true
	charging := s.I < imbChargeA
	if t.r == nil {
		if !charging {
			t.topped = false
			return nil
		}
		if t.topped {
			return nil
		}
		t.start(s)
	} else {
		// ток держится до следующей выборки; длинные дыры в данных не интегрируем
		// ponytail: ступенчатый интеграл, на 5-с выборке и истории HA хватает
		if dt := s.T.Sub(prev.T); had && dt > 0 && dt <= imbMaxDt {
			q := -prev.I * dt.Hours()
			for i := range t.r.AhLag {
				if !t.r.AhLag[i].Reached {
					t.r.AhLag[i].Ah += q
				}
			}
		}
		if charging {
			t.lastChg = s.T
		} else if s.T.Sub(t.lastChg) > imbStopAfter {
			return t.finish(t.lastChg, "stopped")
		}
	}
	t.update(s)
	switch {
	case s.Max >= imbTopV:
		t.topped = true
		return t.finish(s.T, "top")
	case s.Limit == 0:
		t.topped = true
		return t.finish(s.T, "bms_limit")
	}
	return nil
}

func (t *imbTracker) start(s imbSample) {
	typ := imbFromFlat
	if s.Max >= imbRecordV {
		typ = imbAfterRest
	}
	soc := s.SOC
	if math.IsNaN(soc) { // NaN не ложится в JSON; 0 — SOC неизвестен
		soc = 0
	}
	t.r = &imbRecord{Start: s.T, Source: t.source, Type: typ, StartSOC: soc, StartMaxV: s.Max,
		Balancing: s.Phase != "", Phase: s.Phase, Levels: []imbLevel{}, AhLag: []imbLag{}}
	t.lastChg = s.T
}

func (t *imbTracker) update(s imbSample) {
	r := t.r
	r.PeakMaxV = math.Max(r.PeakMaxV, s.Max)
	r.PeakDeltaMV = max(r.PeakDeltaMV, s.deltaMV())
	// уровень, выше которого ячейка была уже на старте, — не пересечение
	for _, l := range imbLevels {
		if r.StartMaxV < l && s.Max >= l && !r.hasLevel(l) {
			r.Levels = append(r.Levels, imbLevel{V: l, Time: s.T, DeltaMV: s.deltaMV(), CurrentA: math.Round(s.I*10) / 10,
				MaxLoc: s.MaxLoc, MinLoc: s.MinLoc})
		}
	}
	for _, l := range imbLagLevels {
		if r.StartMaxV < l && s.Max >= l && r.lag(l) == nil {
			r.AhLag = append(r.AhLag, imbLag{V: l, MaxAt: s.T})
		}
	}
	for i := range r.AhLag {
		if g := &r.AhLag[i]; !g.Reached && s.Min >= g.V {
			at := s.T
			g.Reached, g.MinAt = true, &at
		}
	}
}

func (r *imbRecord) hasLevel(v float64) bool {
	for _, l := range r.Levels {
		if l.V == v {
			return true
		}
	}
	return false
}

func (r *imbRecord) lag(v float64) *imbLag {
	for i := range r.AhLag {
		if r.AhLag[i].V == v {
			return &r.AhLag[i]
		}
	}
	return nil
}

// finish закрывает подъём; пишем только дошедшие до 3,40 В и с чем-то измеренным
// (иначе возобновление заряда уже на верху даёт пустые записи).
func (t *imbTracker) finish(end time.Time, reason string) *imbRecord {
	r := t.r
	t.r = nil
	if r.PeakMaxV < imbRecordV || len(r.Levels) == 0 && len(r.AhLag) == 0 {
		return nil
	}
	r.End, r.EndReason = end, reason
	for i := range r.AhLag {
		r.AhLag[i].Ah = math.Round(r.AhLag[i].Ah*100) / 100
	}
	return r
}
