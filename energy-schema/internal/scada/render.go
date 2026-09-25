package scada

import (
	"fmt"
	"strings"
	"time"

	"energy-schema/internal/config"
)

// clockNow returns the current time (local). Overridden to a fixed value in
// tests so the golden render (timestamp label, generator times) is deterministic.
var clockNow = func() time.Time { return time.Now() }

// invPeakKW — пиковая мощность инвертора по выходу (10 с) сверх длительных
// 33 кВт; зона 33–45 кВт — «перегруз» на гейдже потребления (Дом).
const invPeakKW = 45.0

// pvInputMaxKW — максимальная мощность PV-входа по шильдику инвертора (39 кВт).
// Это потолок шкалы суммарной генерации в карточке Солнце.
const pvInputMaxKW = 39.0

// State is the read-only view of HA entity states the renderer needs.
// *hass.Store satisfies it.
type State interface {
	State(entity string) string
	Num(entity string) float64
	Int(entity string) int
	On(entity string) bool
	Available(entity string) bool
	// last-good snapshot + offline duration (for devices that dropped out)
	LastState(entity string) string
	LastNum(entity string) float64
	LastInt(entity string) int
	LostInfo(entity string) string
	// scalar attributes + time-until helpers (weather, sunset)
	Attr(entity, key string) string
	AttrNum(entity, key string) float64
	HoursUntil(entity, key string) float64
	// daily weather forecast (cloud % + condition, 0=today 1=tomorrow)
	ForecastInfo(daysAhead int) (float64, string, bool)
	// daytime cloud average (%) from the hourly forecast (0=today, 1=tomorrow)
	CloudForDay(daysAhead int) (float64, bool)
	// today's peak numeric value for an entity (for gauge max markers)
	DayMax(entity string) float64
	// energy (kWh) integrated for a *_power entity since local midnight
	DayEnergy(entity string) float64
	// rolling 24h peak/trough (for battery/home markers — independent of midnight)
	Max24h(entity string) float64
	Max12h(entity string) float64
	Min12h(entity string) (float64, bool)
	Avg24h(entity string) (float64, bool)
	// прогноз генерации (провайдер solar): суточные итоги + источник; почасовой профиль
	SolarTotals() (today, todayLeft, tomorrow float64, source string, ok bool)
	SolarProfile() (profile [72]float64, startDay, updated time.Time, ok bool)
	SolarCloudNow() (float64, bool) // облачность Open-Meteo на текущий час (%)
	// empirical generation baseline from long-term statistics
	PVClearDayKWh() float64 // best recent day (clear-day proxy), 0 if unknown
	PVRecent() (float64, int)
	// inverter→grid reconnection countdown: remaining s, total s, active, attempt #
	ReconnectInfo() (float64, float64, bool, int)
}

// phCol returns a phase color: red if off, orange if voltage out of [lo,hi],
// green otherwise.
func phCol(st State, onE, vE string, lo, hi float64) string {
	if !st.On(onE) {
		return cRed
	}
	v := st.Num(vE)
	if v < lo || v > hi { // выход за допустимые границы — оранжевым (не красным)
		return cOrg
	}
	return cGrn
}

// vHighRed — порог повышенного напряжения на ВХОДЕ ИНВЕРТОРА: выше него красным
// (инвертор ловит это как нестабильное и отваливается). Только для инвертора.
const vHighRed = 240.0

// invVCol colours the inverter's incoming grid voltage: red if absent (<1) or
// elevated (>240), orange if low (<205), normal otherwise. Применяется ТОЛЬКО к
// сети·вход инвертора — на входах/стабилизаторах напряжение красным не красим.
func invVCol(v float64) string {
	switch {
	case v < 1:
		return cRed
	case v > vHighRed:
		return cRed
	case v < 205:
		return cOrg
	default:
		return cTxt
	}
}

// rybLineState aggregates the three Рыбхоз phases: off / bad (partial) / on.
func rybLineState(st State) string {
	ons := 0
	for ph := 1; ph <= 3; ph++ {
		if st.On(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) {
			ons++
		}
	}
	if ons == 0 {
		return "off"
	}
	if ons < 3 {
		return "bad"
	}
	return "on"
}

// greenLineState aggregates the three Зелёный phases by presence + voltage.
func greenLineState(st State) string {
	ons, withV := 0, 0
	for ph := 1; ph <= 3; ph++ {
		if st.On(fmt.Sprintf("sensor.sim_green_l%d_on", ph)) {
			ons++
			if st.Num(fmt.Sprintf("sensor.sim_green_l%d_v", ph)) > 50 {
				withV++
			}
		}
	}
	if ons == 0 {
		return "off"
	}
	if withV == 0 || ons < 3 {
		return "bad"
	}
	return "on"
}

// rybPhase diagnoses ONE Рыбхоз phase line (independently of the others):
//
//	"on"   — фаза под напряжением (нормальный поток);
//	"lost" — датчик линии молчит, НО инвертор видит напряжение на этой фазе →
//	         линия жива, потеряна связь с датчиком/устройством (оранжевый «?»);
//	"bad"  — линии нет и инвертор фазу не подтверждает → реальный обрыв (красный ✕).
//
// Сверка по инвертору достоверна, только когда контактор кормит инвертор Рыбхозом.
func rybPhase(st State, ph int, contRyb bool) string {
	if st.On(fmt.Sprintf("sensor.sim_ryb_l%d_on", ph)) {
		return "on"
	}
	if contRyb && st.Num(fmt.Sprintf("sensor.deye_sun_30k_grid_l%d_voltage", ph)) > 150 {
		return "lost"
	}
	return "bad"
}

// stabOut is the state of stabilizer ph's output line: a real input break just
// de-energizes the output ("off"), it isn't a fault on the output side.
func stabOut(st State, ph int, contRyb bool) string {
	switch rybPhase(st, ph, contRyb) {
	case "bad":
		return "off"
	case "lost":
		return "lost"
	default:
		return "on"
	}
}

// Render builds the full SVG single-line diagram from the current state snapshot.
// cloudCond мапит облачность (%) в condition для иконки погоды (когда берём
// облачность из Open-Meteo, а не из Met.no).
func cloudCond(c float64) string {
	switch {
	case c < 20:
		return "sunny"
	case c < 60:
		return "partlycloudy"
	default:
		return "cloudy"
	}
}

// hoursToGen — часы до начала заметной генерации по прогнозному почасовому
// профилю (учитывает ориентацию/наклон полей и погоду; калибровка по истории за
// последние дни уточняет профиль). Возвращает 0, если профиля нет или старт
// генерации не найден в горизонте. Фолбэк без профиля — время до восхода.
func hoursToGen(st State) float64 {
	prof, sd, _, ok := st.SolarProfile()
	if !ok {
		return st.HoursUntil("sun.sun", "next_rising")
	}
	const genThresh = 0.5 // кВт·ч/ч — порог «генерация началась»
	now := clockNow()
	for i := 0; i < 72; i++ {
		if prof[i] >= genThresh {
			if d := sd.Add(time.Duration(i) * time.Hour).Sub(now).Hours(); d > 0 {
				return d
			}
		}
	}
	return 0
}

// frame — значения одного кадра рендера, общие для нескольких карточек.
// Считаются один раз в newFrame; секции flows/row1/row2/battery/sun/generator
// (render_*.go) читают их.
type frame struct {
	st  State
	cfg config.Config
	s   *Builder

	gridAvail, gridBonded, gridIn bool
	avrPos                        string
	avrStuck, genRun              bool
	rybSt, grnSt                  string
	exporting                     bool
	load, pvtot, bp               float64
	emuStale                      bool
	stOn                          map[string]string
	contOn, contRyb               bool
	clearDay                      float64 // считается в battery(), читается в sun()
}

func newFrame(st State, cfg config.Config, s *Builder) *frame {
	f := &frame{st: st, cfg: cfg, s: s}
	cont := st.State("sensor.sim_contactor")
	f.gridAvail = st.On("binary_sensor.deye_sun_30k_grid")
	f.gridBonded = strings.Contains(st.State("sensor.deye_sun_30k_device_relay"), "Grid")
	f.gridIn = f.gridBonded
	f.avrPos = st.State("sensor.sim_avr_pos")
	// АВР «залип»: переключён на Резерв, но инвертор всё ещё несёт нагрузку Дома —
	// значит реле не перекинулось (питание не ушло на резерв).
	avrLoad := st.Num("sensor.deye_sun_30k_load_l1_power") + st.Num("sensor.deye_sun_30k_load_l2_power") + st.Num("sensor.deye_sun_30k_load_l3_power")
	f.avrStuck = f.avrPos == "reserve" && avrLoad > 200
	f.genRun = st.State("sensor.sim_gen_state") == "running"
	f.rybSt = rybLineState(st)
	f.grnSt = greenLineState(st)
	f.exporting = st.State("sensor.sim_export") == "on" && f.grnSt == "on"
	f.load = st.Num("sensor.deye_sun_30k_load_power") / 1000
	f.pvtot = st.Num("sensor.deye_sun_30k_pv1_power") + st.Num("sensor.deye_sun_30k_pv2_power") + st.Num("sensor.deye_sun_30k_pv3_power")
	f.bp = st.Num("sensor.deye_sun_30k_battery_voltage") * st.Num("sensor.deye_sun_30k_battery_current")

	// «связь с устройствами»: в демо-эмуляторе sim_heartbeat обновляется каждый
	// цикл (unix-метка). Если она устарела (>30с) или эмулятор выключен — связи с
	// девайсами по RS-485 фактически нет, индикаторы краснеют. В реальной системе
	// heartbeat отсутствует (HA сам периодически опрашивает Modbus/RS-485): тогда
	// живость определяется доступностью самих сущностей — реальный офлайн девайса
	// делает сущность unavailable, и RS-485 так же краснеет.
	f.emuStale = st.Available("sensor.sim_heartbeat") && clockNow().Unix()-int64(st.Num("sensor.sim_heartbeat")) > 30

	f.stOn = map[string]string{"on": cGrn, "bad": cOrg, "lost": cOrg, "off": cGry}
	// Контактор — одно реле: ВЫКЛ → Ввод1 Рыбхоз (по автоматам, дефолт); ВКЛ → Ввод2 Зелёный.
	f.contOn = cont == "on"
	f.contRyb = !f.contOn // активный ввод = Рыбхоз, пока контактор выключен
	return f
}

// Render builds the full SVG single-line diagram from the current state snapshot.
func Render(st State, cfg config.Config) string {
	s := &Builder{}
	s.p(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 808" font-family="Arial,Helvetica,sans-serif">`)
	s.p(`<rect x="0" y="0" width="1440" height="830" fill="#0f1115"/>`)
	// вместо заголовка — текущая временная метка
	s.t(1428, 28, 18, cTxt, "end", clockNow().Format("2006-01-02 15:04:05"))

	f := newFrame(st, cfg, s)
	f.flows()
	f.row1()
	f.row2()
	f.avr3()
	f.battery()
	f.sun()
	f.generator()

	s.p(`</svg>`)
	return s.String()
}
