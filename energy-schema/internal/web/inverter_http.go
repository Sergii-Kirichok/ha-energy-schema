package web

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"time"
)

//go:embed inverter.html
var inverterHTML []byte

const overridesFile = "/data/overrides.json"

// readRaw читает все блоки регистров страницы; factor — число каналов АКБ (рег. 110).
func (s *Server) readRaw() (map[int]int, float64, error) {
	raw := map[int]int{}
	for _, b := range append(invBlocks, [2]int{parallelReg, 1}) {
		r, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, b[0], b[1])
		if err != nil {
			return nil, 0, err
		}
		for k, v := range r {
			raw[k] = v
		}
	}
	return raw, 1 + float64(raw[parallelReg]&1), nil
}

func decodeAll(raw map[int]int, factor float64) map[string]float64 {
	out := map[string]float64{}
	for _, f := range invFields {
		if v, ok := raw[f.Reg]; ok {
			out[f.Key] = f.decode(v, factor)
		}
	}
	return out
}

// readInverter — значения полей страницы.
func (s *Server) readInverter() (map[string]float64, float64, error) {
	raw, factor, err := s.readRaw()
	if err != nil {
		return nil, 0, err
	}
	return decodeAll(raw, factor), factor, nil
}

// touBlock — слоты расписания (время, мощность, напряжение, SOC, источник)
// пишутся одним запросом целиком, как это делает штатная программа Deye.
var touBlock = [2]int{148, 30}

// invVerifyDelays — паузы перед повторными чтениями при сверке (≈10 с всего).
var invVerifyDelays = []time.Duration{1500 * time.Millisecond, 2500 * time.Millisecond, 3 * time.Second, 3 * time.Second}

// writeRuns группирует изменённые регистры в непрерывные куски (каждый — один
// запрос функцией 16). Если затронут любой слот, блок расписания пишется
// целиком: неизменённые регистры берутся из текущих значений base.
func writeRuns(changed, base map[int]int) [][]int {
	regs := map[int]int{}
	for r, v := range changed {
		regs[r] = v
	}
	for r := range changed {
		if r >= touBlock[0] && r < touBlock[0]+touBlock[1] {
			for a := touBlock[0]; a < touBlock[0]+touBlock[1]; a++ {
				if _, ok := regs[a]; !ok {
					regs[a] = base[a]
				}
			}
			break
		}
	}
	addrs := make([]int, 0, len(regs))
	for a := range regs {
		addrs = append(addrs, a)
	}
	sort.Ints(addrs)
	var runs [][]int // [start, v0, v1, ...]
	for _, a := range addrs {
		if n := len(runs); n > 0 && runs[n-1][0]+len(runs[n-1])-1 == a {
			runs[n-1] = append(runs[n-1], regs[a])
			continue
		}
		runs = append(runs, []int{a, regs[a]})
	}
	return runs
}

// guard — те же правила, что у /control: POST с той же страницы и права управления.
func (s *Server) guard(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" && sfs != "same-origin" {
		http.Error(w, "cross-site request rejected", http.StatusForbidden)
		return false
	}
	if !s.userAllowed(r) {
		http.Error(w, "только просмотр — нет прав управления", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) handleInverterPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(inverterHTML)
}

func (s *Server) inverterState(w http.ResponseWriter, r *http.Request) {
	vals, factor, err := s.readInverter()
	if err != nil {
		http.Error(w, "нет связи с инвертором: "+err.Error(), http.StatusBadGateway)
		return
	}
	num := s.store.Num
	writeJSON(w, map[string]any{
		"values": vals, "manual": s.ovr.snapshot(), "regulator": s.store.On(chargeAutoHelper), "channels": factor, "can": s.userAllowed(r),
		"live": map[string]any{
			"v":    []float64{num("sensor.deye_sun_30k_grid_l1_voltage"), num("sensor.deye_sun_30k_grid_l2_voltage"), num("sensor.deye_sun_30k_grid_l3_voltage")},
			"f":    num("sensor.deye_sun_30k_grid_frequency"),
			"temp": num("sensor.deye_sun_30k_temperature"),
		},
		"auto": map[string]float64{"max_charge_a": num("sensor.energy_schema_charge_setpoint")},
		"ts":   time.Now().Format("15:04:05"),
	})
}

type invSaveReq struct {
	Values map[string]float64 `json:"values"`
	Manual map[string]bool    `json:"manual"`
}

type invResult struct {
	OK    bool    `json:"ok"`
	Want  float64 `json:"want"`
	Read  float64 `json:"read"`
	Error string  `json:"error,omitempty"`
}

// inverterSave: валидирует всё, пишет только отличающиеся регистры, затем
// перечитывает и сверяет каждое поле.
func (s *Server) inverterSave(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var req invSaveReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	baseRaw, factor, err := s.readRaw()
	if err != nil {
		http.Error(w, "нет связи с инвертором: "+err.Error(), http.StatusBadGateway)
		return
	}
	regulator := s.store.On(chargeAutoHelper)
	if regulator {
		req.Manual = nil // при включённом «Авто-регулятор» ручных полей регулятора нет
	}
	manual := s.ovr.snapshot()
	for k, v := range req.Manual {
		manual[k] = v
	}
	next := map[string]float64{}
	for k, v := range decodeAll(baseRaw, factor) {
		next[k] = v
	}
	var writes []invWrite
	for k, v := range req.Values {
		f := invField0(k)
		if f == nil {
			http.Error(w, "неизвестное поле "+k, http.StatusBadRequest)
			return
		}
		if f.Auto != "" && (regulator || !manual[f.Auto]) {
			continue // поле регулятора в «авто» — пишет регулятор
		}
		raw, err := f.encode(v, factor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		next[k] = v
		writes = append(writes, invWrite{*f, raw})
	}
	if err := validatePair(next); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.ovr.set(req.Manual)
	changed := map[int]int{}
	for _, x := range writes {
		changed[x.f.Reg] = x.raw
	}
	var werr string
	for _, run := range writeRuns(changed, baseRaw) {
		if err := s.client.WriteHoldingRegisters(bmsDeviceEntity, run[0], run[1:]); err != nil {
			werr = err.Error()
		}
		log.Printf("inverter: write %d..%d = %v", run[0], run[0]+len(run)-2, run[1:])
	}
	// инвертор применяет запись с задержкой (до нескольких секунд): перечитываем,
	// пока всё не совпадёт или не выйдет время
	var after map[int]int
	for _, d := range invVerifyDelays {
		time.Sleep(d)
		if after, _, err = s.readRaw(); err != nil {
			continue
		}
		if allMatch(writes, after) {
			break
		}
	}
	if after == nil {
		http.Error(w, "записано, но перечитать не удалось: "+err.Error(), http.StatusBadGateway)
		return
	}
	res, ok := map[string]invResult{}, true
	for _, x := range writes {
		k, got := x.f.Key, after[x.f.Reg]
		r := invResult{Want: req.Values[k], Read: x.f.decode(got, factor), OK: got == x.raw, Error: werr}
		ok = ok && r.OK
		res[k] = r
		log.Printf("inverter: %s reg %d ← %d, read %d (%v)", k, x.f.Reg, x.raw, got, map[bool]string{true: "ok", false: "MISMATCH"}[r.OK])
	}
	writeJSON(w, map[string]any{"ok": ok, "results": res, "values": decodeAll(after, factor), "manual": s.ovr.snapshot(), "regulator": regulator, "ts": time.Now().Format("15:04:05")})
}

func allMatch(writes []invWrite, raw map[int]int) bool {
	for _, x := range writes {
		if raw[x.f.Reg] != x.raw {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// invWrite — поле и сырое значение регистра к записи.
type invWrite struct {
	f   invField
	raw int
}
