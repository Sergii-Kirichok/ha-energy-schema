package web

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
)

// Страница настроек инвертора: поле ↔ регистр Deye (MODBUS RTU V104 + профиль
// Solarman deye_p3, адреса сверены с живыми значениями 02.10). Запись —
// solarman.write_holding_register, сверка — чтение тех же регистров обратно.
type invField struct {
	Key      string
	Reg      int
	Kind     string  // num | bool | enum | hhmm | bits
	Scale    float64 // value = raw × Scale (num); 0 = × число каналов АКБ (рег. 110)
	Min, Max float64
	Auto     string // ключ ручного переопределения регулятора ("" — обычное поле)
}

var invFields = func() []invField {
	f := []invField{
		{Key: "zero_export_w", Reg: 104, Kind: "num", Scale: 10, Min: 0, Max: 500},
		{Key: "max_charge_a", Reg: 108, Kind: "num", Min: 0, Max: 185, Auto: "max_charge"},
		{Key: "max_discharge_a", Reg: 109, Kind: "num", Scale: 1, Min: 0, Max: 185},
		{Key: "shutdown_soc", Reg: 115, Kind: "num", Scale: 1, Min: 5, Max: 80},
		{Key: "restart_soc", Reg: 116, Kind: "num", Scale: 1, Min: 5, Max: 90},
		{Key: "low_soc", Reg: 117, Kind: "num", Scale: 1, Min: 5, Max: 90},
		{Key: "grid_charge_a", Reg: 128, Kind: "num", Min: 0, Max: 185, Auto: "grid_a"},
		{Key: "gen_charge", Reg: 129, Kind: "bool"},
		{Key: "grid_charge", Reg: 130, Kind: "bool", Auto: "grid_charge"},
		{Key: "energy_pattern", Reg: 141, Kind: "enum", Max: 1},
		{Key: "work_mode", Reg: 142, Kind: "enum", Max: 2},
		{Key: "tou", Reg: 146, Kind: "bits", Max: 255}, // бит 0 — вкл., биты 1–7 — Пн…Вс
		{Key: "reconnect_s", Reg: 180, Kind: "num", Scale: 1, Min: 1, Max: 300},
		{Key: "v_high", Reg: 185, Kind: "num", Scale: 0.1, Min: 200, Max: 290},
		{Key: "v_low", Reg: 186, Kind: "num", Scale: 0.1, Min: 100, Max: 230},
		{Key: "f_high", Reg: 187, Kind: "num", Scale: 0.01, Min: 50.1, Max: 65},
		{Key: "f_low", Reg: 188, Kind: "num", Scale: 0.01, Min: 45, Max: 49.9},
	}
	for i := 0; i < 6; i++ {
		n := i + 1
		f = append(f,
			invField{Key: fmt.Sprintf("slot%d_time", n), Reg: 148 + i, Kind: "hhmm"}, // минуты суток ↔ HHMM
			invField{Key: fmt.Sprintf("slot%d_soc", n), Reg: 166 + i, Kind: "num", Scale: 1, Min: 5, Max: 100},
			invField{Key: fmt.Sprintf("slot%d_src", n), Reg: 172 + i, Kind: "enum", Max: 3}) // —/Сеть/Ген/Оба
	}
	return f
}()

// invBlocks — чтение всех полей четырьмя запросами.
var invBlocks = [][2]int{{104, 27}, {141, 6}, {148, 30}, {180, 9}}

func invField0(key string) *invField {
	for i := range invFields {
		if invFields[i].Key == key {
			return &invFields[i]
		}
	}
	return nil
}

func (f invField) scale(factor float64) float64 {
	if f.Scale == 0 {
		return math.Max(1, factor)
	}
	return f.Scale
}

// decode — значение для страницы из сырого регистра.
func (f invField) decode(raw int, factor float64) float64 {
	switch f.Kind {
	case "num":
		return math.Round(float64(raw)*f.scale(factor)*100) / 100
	case "hhmm":
		return float64(raw/100*60 + raw%100)
	}
	return float64(raw)
}

// encode — сырое значение регистра; ошибка, если значение вне допустимого.
func (f invField) encode(v, factor float64) (int, error) {
	switch f.Kind {
	case "bool":
		if v != 0 && v != 1 {
			return 0, fmt.Errorf("%s: ожидается 0/1", f.Key)
		}
		return int(v), nil
	case "hhmm":
		if v < 0 || v >= 1440 || v != math.Trunc(v) {
			return 0, fmt.Errorf("%s: время вне суток", f.Key)
		}
		m := int(v)
		return m/60*100 + m%60, nil
	case "enum", "bits":
		if v < 0 || v > f.Max || v != math.Trunc(v) {
			return 0, fmt.Errorf("%s: недопустимое значение %v", f.Key, v)
		}
		return int(v), nil
	}
	if v < f.Min || v > f.Max {
		return 0, fmt.Errorf("%s: %v вне диапазона %v…%v", f.Key, v, f.Min, f.Max)
	}
	return int(math.Round(v / f.scale(factor))), nil
}

// validatePair — связанные пороги: нижний строго ниже верхнего, слоты по возрастанию.
func validatePair(v map[string]float64) error {
	for _, p := range [][2]string{{"v_low", "v_high"}, {"f_low", "f_high"}} {
		if v[p[0]] >= v[p[1]] {
			return fmt.Errorf("%s должен быть меньше %s", p[0], p[1])
		}
	}
	for n := 2; n <= 6; n++ {
		if v[fmt.Sprintf("slot%d_time", n)] <= v[fmt.Sprintf("slot%d_time", n-1)] {
			return fmt.Errorf("время слота %d должно быть позже слота %d", n, n-1)
		}
	}
	return nil
}

// overrides — поля регулятора, переведённые со страницы в «вручную».
type overrides struct {
	mu   sync.Mutex
	path string
	m    map[string]bool
}

func loadOverrides(path string) *overrides {
	o := &overrides{path: path, m: map[string]bool{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &o.m)
	}
	return o
}

func (o *overrides) get(k string) bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.m[k]
}

func (o *overrides) snapshot() map[string]bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	c := map[string]bool{}
	for k, v := range o.m {
		c[k] = v
	}
	return c
}

func (o *overrides) set(m map[string]bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for k, v := range m {
		o.m[k] = v
	}
	b, _ := json.Marshal(o.m)
	_ = os.WriteFile(o.path, b, 0o644)
}
