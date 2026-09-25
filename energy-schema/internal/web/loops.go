package web

import (
	"log"
	"strconv"
	"strings"
	"time"

	"energy-schema/internal/hass"
)

// BMS SOH: интеграция Solarman считает sensor.*_battery_soh по накопленному
// заряду с номиналом 48 В — для HV-батареи (630 В) это даёт ~92 % вместо 97 %
// от BMS. Настоящий SOH отдаёт регистр 10006 (Deye battery read-only block,
// «Battery SOH», 1 %). Читаем его через сервис solarman.read_holding_registers
// и публикуем как виртуальную сущность; рендер предпочитает её.
const (
	bmsDeviceEntity = "sensor.deye_sun_30k_battery" // любая сущность устройства Solarman
	bmsSOHRegister  = 10006
	bmsSOHEntity    = "sensor.energy_schema_bms_soh"
)

func (s *Server) loopBMS() {
	last := -1
	for {
		regs, err := s.client.ReadHoldingRegisters(bmsDeviceEntity, bmsSOHRegister, 1)
		if err != nil {
			log.Println("bms soh:", err)
		} else if v := regs[bmsSOHRegister]; v >= 1 && v <= 100 {
			s.store.SetVirtual(bmsSOHEntity, strconv.Itoa(v))
			if v != last {
				log.Printf("bms soh: %d%% (reg %d)", v, bmsSOHRegister)
				last = v
			}
		} else {
			log.Printf("bms soh: reg %d out of range: %v", bmsSOHRegister, regs)
		}
		time.Sleep(10 * time.Minute)
	}
}

// weatherEntity is the HA weather entity used for the autonomy forecast.
const weatherEntity = "weather.forecast_home_assistant"

// productionEntity is the cumulative lifetime PV energy sensor; its long-term
// statistics give us real daily generation (the recorder keeps only ~2 days of
// raw history, but statistics persist for a year).
const productionEntity = "sensor.deye_sun_30k_total_production"

// loopForecast refreshes the daily weather forecast every 30 minutes.
func (s *Server) loopForecast() {
	for {
		if days, err := s.client.DailyForecast(weatherEntity); err != nil {
			log.Println("forecast:", err)
		} else {
			s.store.SetForecast(days)
		}
		if hrs, err := s.client.HourlyForecast(weatherEntity); err != nil {
			log.Println("hourly forecast:", err)
		} else {
			s.store.SetHourly(hrs)
		}
		time.Sleep(30 * time.Minute)
	}
}

// loopPVHistory refreshes the empirical generation baseline from long-term
// statistics every 3 hours: best recent day (clear-day proxy) and the average.
// Forecasting tomorrow's yield off real recent days beats a fixed nameplate
// guess — in winter the "clear day" is far below a summer one.
func (s *Server) loopPVHistory() {
	for {
		if daily, err := s.client.DailyProduction(productionEntity, 10); err != nil {
			log.Println("pv history:", err)
		} else if len(daily) > 0 {
			best, sum := 0.0, 0.0
			for _, v := range daily {
				if v > best {
					best = v
				}
				sum += v
			}
			s.store.SetPVStats(best, sum/float64(len(daily)), len(daily))
			log.Printf("pv history: %d days, best %.0f kWh, avg %.0f kWh", len(daily), best, sum/float64(len(daily)))
		}
		time.Sleep(3 * time.Hour)
	}
}

// rollSeedEntities have their rolling 24h min/max seeded from history at
// startup so the home/battery markers don't reset to "now" on every restart.
var rollSeedEntities = []string{
	"sensor.deye_sun_30k_load_power",
	"sensor.deye_sun_30k_battery",
	"sensor.deye_sun_30k_pv_power", // часовой факт генерации для калибровки прогноза
}

// rollFile persists the rolling 24h buffers across restarts (the HA recorder
// drops peaks our 5s poll catches). /data is the add-on's persistent volume.
const rollFile = "/data/roll.json"

// rollPersistEntities are the entities whose 24h min/avg/max must survive a
// restart (battery SOC peak, home load min/avg/max).
var rollPersistEntities = []string{
	"sensor.deye_sun_30k_battery",
	"sensor.deye_sun_30k_load_power",
	"sensor.deye_sun_30k_pv_power",
}

// loopPersist saves the rolling buffers (and calibration) to disk every minute.
func (s *Server) loopPersist() {
	for {
		time.Sleep(60 * time.Second)
		if err := s.store.SaveRoll(rollFile, rollPersistEntities); err != nil {
			log.Println("save roll:", err)
		}
		if s.solar != nil && s.solar.Cal != nil {
			if err := s.solar.Cal.Save(); err != nil {
				log.Println("save calib:", err)
			}
		}
	}
}

// pvDayMaxEntities have today's peak seeded from history (sun "Max today").
var pvDayMaxEntities = []string{
	"sensor.deye_sun_30k_pv_power",
	"sensor.deye_sun_30k_pv1_power",
	"sensor.deye_sun_30k_pv2_power",
	"sensor.deye_sun_30k_pv3_power",
}

// seedRolls pre-fills the rolling 24h windows from recorder history so a freshly
// restarted add-on already reflects the true last-24h min/max, not just values
// seen since boot.
func (s *Server) seedRolls() {
	since := time.Now().Add(-24 * time.Hour)
	for _, e := range rollSeedEntities {
		pts, err := s.client.History(e, since)
		if err != nil {
			log.Println("seed history:", e, err)
			continue
		}
		for _, p := range pts {
			s.store.SeedRoll(e, p.Time, p.Value)
		}
		log.Printf("seed %s: %d points (24h)", e, len(pts))
	}
	// sun "Max today" peaks — seed from today's history so they survive a restart
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, e := range pvDayMaxEntities {
		pts, err := s.client.History(e, midnight)
		if err != nil {
			log.Println("seed daymax:", e, err)
			continue
		}
		kwh := 0.0 // интеграл мощности по трапециям — энергия с начала дня
		for i, p := range pts {
			s.store.SeedDayMax(e, p.Time, p.Value)
			if i > 0 {
				if d := p.Time.Sub(pts[i-1].Time).Hours(); d > 0 && d < 0.5 {
					kwh += (pts[i-1].Value + p.Value) / 2 / 1000 * d
				}
			}
		}
		s.store.SetDayEnergy(e, kwh)
		log.Printf("seed %s: %d points, %.1f kWh today", e, len(pts), kwh)
	}
}

// trackReconnect derives the inverter→grid reconnection state from the device:
// grid present (qualified) but the inverter relay not yet bonded to it = waiting.
// The device exposes only the delay setpoint, not a live countdown, so the Store
// times it — restarting on each re-entry (a failed attempt drops the relay again).
func (s *Server) trackReconnect() {
	// «сеть присутствует» — по НАПРЯЖЕНИЮ фаз (оно появляется сразу при возврате
	// сети), а не по binary_sensor.grid (тот включается лишь в момент подключения,
	// поэтому окно ожидания «напряжение есть, но не подключился» им не поймать).
	gridPresent := s.store.Num("sensor.deye_sun_30k_grid_l1_voltage") > 150 ||
		s.store.Num("sensor.deye_sun_30k_grid_l2_voltage") > 150 ||
		s.store.Num("sensor.deye_sun_30k_grid_l3_voltage") > 150
	bonded := strings.Contains(s.store.State("sensor.deye_sun_30k_device_relay"), "Grid")
	total := s.store.Num("number.deye_sun_30k_grid_reconnection_time")
	s.store.UpdateReconnect(gridPresent && !bonded, gridPresent, total)
}

// loop refreshes the state snapshot and the on-disk SVG on a fixed cadence.
func (s *Server) loop() {
	for {
		if m, err := s.client.FetchStates(); err != nil {
			log.Println("fetch:", err)
		} else {
			s.store.Replace(m)
			s.trackReconnect()
		}
		s.writeFiles()
		time.Sleep(pollInterval)
	}
}

// loopAnim re-renders the on-disk SVG ~once a second so the marching flow arrows
// move smoothly on the TV (host re-fetches /local/energy_schema.svg; rsvg can't
// play SMIL). Data itself refreshes on the slower poll loop.
func (s *Server) loopAnim() {
	for {
		time.Sleep(time.Second)
		s.writeFiles()
	}
}

// loopSolarForecast refreshes the generation forecast snapshot every 30 minutes
// (Open-Meteo recomputes ~every 15 min; more often is pointless).
func (s *Server) loopSolarForecast() {
	for {
		snap := s.solar.Build(time.Now(), s.store)
		s.store.SetSolarForecast(hass.SolarForecastSnapshot{
			StartDay:     snap.StartDay,
			HourlyKWh:    snap.HourlyKWh,
			TodayKWh:     snap.Today,
			TodayLeftKWh: snap.TodayLeft,
			TomorrowKWh:  snap.Tomorrow,
			Source:       snap.Source,
			CloudNow:     snap.CloudNow,
			UpdatedAt:    time.Now(),
		})
		log.Printf("solar: прогноз (%s) сегодня %.1f · остаток %.1f · завтра %.1f кВт·ч",
			snap.Source, snap.Today, snap.TodayLeft, snap.Tomorrow)
		time.Sleep(30 * time.Minute)
	}
}

// loopCalib learns the forecast calibration from finished hours every 5 minutes
// (actual hourly generation from pv_power roll vs the frozen forecast, gated by
// coverage and full-battery SOC).
func (s *Server) loopCalib() {
	const pv = "sensor.deye_sun_30k_pv_power"
	const soc = "sensor.deye_sun_30k_battery"
	for {
		time.Sleep(5 * time.Minute)
		if s.solar == nil || s.solar.Cal == nil {
			continue
		}
		s.solar.Cal.ObserveDue(time.Now(),
			func(h int64) (float64, int) { return s.store.HourlyEnergy(pv, h) },
			func(h int64) (float64, bool) { return s.store.HourlyMax(soc, h) })
		w, rl, days, mae := s.solar.Cal.Status()
		log.Printf("calib: уровень W=%.1f R=%.2f · дней=%d · MAE7=%.1f кВт·ч", w, rl, days, mae)
	}
}
