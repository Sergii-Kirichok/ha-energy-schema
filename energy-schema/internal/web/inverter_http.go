package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

//go:embed inverter.html
var inverterHTML []byte

const overridesFile = "/data/overrides.json"

// readInverter читает все поля страницы; factor — число каналов АКБ (рег. 110).
func (s *Server) readInverter() (map[string]float64, float64, error) {
	raw := map[int]int{}
	for _, b := range invBlocks {
		r, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, b[0], b[1])
		if err != nil {
			return nil, 0, err
		}
		for k, v := range r {
			raw[k] = v
		}
	}
	r, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, parallelReg, 1)
	if err != nil {
		return nil, 0, err
	}
	factor := 1 + float64(r[parallelReg]&1)
	out := map[string]float64{}
	for _, f := range invFields {
		if v, ok := raw[f.Reg]; ok {
			out[f.Key] = f.decode(v, factor)
		}
	}
	return out, factor, nil
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
		"values": vals, "manual": s.ovr.snapshot(), "channels": factor, "can": s.userAllowed(r),
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
	cur, factor, err := s.readInverter()
	if err != nil {
		http.Error(w, "нет связи с инвертором: "+err.Error(), http.StatusBadGateway)
		return
	}
	manual := s.ovr.snapshot()
	for k, v := range req.Manual {
		manual[k] = v
	}
	next := map[string]float64{}
	for k, v := range cur {
		next[k] = v
	}
	type wr struct {
		f   invField
		raw int
	}
	var writes []wr
	for k, v := range req.Values {
		f := invField0(k)
		if f == nil {
			http.Error(w, "неизвестное поле "+k, http.StatusBadRequest)
			return
		}
		if f.Auto != "" && !manual[f.Auto] {
			continue // поле регулятора в «авто» — пишет регулятор
		}
		raw, err := f.encode(v, factor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		next[k] = v
		writes = append(writes, wr{*f, raw})
	}
	if err := validatePair(next); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.ovr.set(req.Manual)
	res := map[string]invResult{}
	for _, x := range writes {
		if err := s.client.WriteHoldingRegister(bmsDeviceEntity, x.f.Reg, x.raw); err != nil {
			res[x.f.Key] = invResult{Want: req.Values[x.f.Key], Error: err.Error()}
		}
	}
	time.Sleep(time.Second) // инвертору нужно время применить значение
	after, _, err := s.readInverter()
	if err != nil {
		http.Error(w, "записано, но перечитать не удалось: "+err.Error(), http.StatusBadGateway)
		return
	}
	ok := true
	for _, x := range writes {
		k := x.f.Key
		r := res[k]
		r.Want, r.Read = req.Values[k], after[k]
		r.OK = r.Error == "" && fmt.Sprint(x.f.decode(x.raw, factor)) == fmt.Sprint(after[k])
		ok = ok && r.OK
		res[k] = r
		log.Printf("inverter: %s reg %d ← %d, read %v (%v)", k, x.f.Reg, x.raw, after[k], map[bool]string{true: "ok", false: "MISMATCH"}[r.OK])
	}
	writeJSON(w, map[string]any{"ok": ok, "results": res, "values": after, "manual": s.ovr.snapshot(), "ts": time.Now().Format("15:04:05")})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
