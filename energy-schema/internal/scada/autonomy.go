package scada

import (
	"fmt"
	"math"
)

// condCloud estimates cloud coverage (%) from a HA condition string — daily
// forecasts of some providers (Met.no) omit cloud_coverage entirely.
func condCloud(c string) float64 {
	switch c {
	case "sunny", "clear-night":
		return 5
	case "partlycloudy":
		return 40
	case "windy", "windy-variant":
		return 30
	case "cloudy", "exceptional":
		return 85
	case "rainy", "pouring", "snowy", "snowy-rainy", "hail", "fog":
		return 90
	case "lightning", "lightning-rainy":
		return 80
	default:
		return 50
	}
}

// simulateAutonomy прогнозирует, на сколько часов хватит батареи, если внешний
// ввод пропадёт прямо сейчас (генератор не учитываем). Почасовая симуляция на
// 48ч вперёд: сегодня PV = текущая генерация до заката (батарея дозаряжается,
// но не выше usableMax), ночью PV = 0, завтра — оценка генерации по прогнозу
// облачности (clearDayKWh × фактор облачности, равномерно по светлому дню).
// Возвращает часы (48 = «горизонт прогноза перекрыт») и пояснение-разбивку.
func simulateAutonomy(st State, usable, usableMax, loadKW, pvNowKW, clearDayKWh float64) (float64, string) {
	if loadKW < 0.05 {
		return 48, "нет нагрузки"
	}
	hSet := st.HoursUntil("sun.sun", "next_setting")
	hRise := st.HoursUntil("sun.sun", "next_rising")
	dayNow := st.State("sun.sun") == "above_horizon" || pvNowKW > 0.1
	if hSet == 0 && hRise == 0 { // нет данных солнца — простая оценка без прогноза
		return usable / loadKW, fmt.Sprintf("без прогноза · нагрузка %.1f кВт", loadKW)
	}
	// облачность на завтра: дневной прогноз; если провайдер не отдал % — по condition
	cloud, cond, okF := st.ForecastInfo(1)
	if okF && cloud <= 0 {
		cloud = condCloud(cond)
	}
	if !okF {
		cloud = st.AttrNum("weather.forecast_home_assistant", "cloud_coverage")
	}
	tomorrowKWh := clearDayKWh * (1 - 0.7*cloud/100)
	// облачность на сегодня — «живая» (как в шапке солнца), а не дневной condition;
	// иначе сегодня и завтра выходят одинаковыми, хотя погода разная.
	cloudToday := st.AttrNum("weather.forecast_home_assistant", "cloud_coverage")
	if cloudToday <= 0 {
		if _, c0, ok0 := st.ForecastInfo(0); ok0 {
			cloudToday = condCloud(c0)
		}
	}
	todayKWh := clearDayKWh * (1 - 0.7*cloudToday/100)
	// длина светлого дня из пары восход/закат (работает и днём, и ночью)
	dayLen := math.Mod(hSet-hRise+24, 24)
	if dayLen < 2 || dayLen > 20 {
		dayLen = 14
	}
	pvTomorrow := tomorrowKWh / dayLen
	// сколько ещё дадут панели СЕГОДНЯ до заката: дневной потенциал (статистика ×
	// текущая облачность) × доля энергии светового дня, оставшаяся до заката.
	// Профиль мощности — полусинусоида: f = (1−cos(π·hSet/dayLen))/2 (≈1 утром,
	// ~0.5 в солнечный полдень, →0 к закату). Не зависит от уже выработанного.
	dayFrac := hSet / dayLen
	if dayFrac > 1 {
		dayFrac = 1
	}
	todayLeft := todayKWh * (1 - math.Cos(math.Pi*dayFrac)) / 2
	pvTodayAvg := pvNowKW // для симуляции: та же энергия ровным потоком до заката
	if hSet > 0.1 {
		pvTodayAvg = todayLeft / hSet
	}

	// разбивка для подписи под цифрой: прогноз генерации на остаток сегодня и завтра
	note := fmt.Sprintf("завтра ~%.0f кВт·ч (обл %.0f%%)", tomorrowKWh, cloud)
	if dayNow {
		note = fmt.Sprintf("сегодня ещё ~%.0f · ", todayLeft) + note
	} else {
		note = fmt.Sprintf("ночь, восход ~%.0fч · ", hRise) + note
	}

	e := usable
	const dt = 0.25
	for t := 0.0; t < 48; t += dt {
		pv := 0.0
		switch {
		case dayNow && t < hSet: // остаток сегодня — по прогнозу, не мгновенная мощность
			pv = pvTodayAvg
		case t >= hRise && t < hRise+dayLen: // завтрашний световой день
			pv = pvTomorrow
		case t >= hRise+24 && t < hRise+24+dayLen: // послезавтра — так же
			pv = pvTomorrow
		}
		e += (pv - loadKW) * dt
		if e > usableMax {
			e = usableMax
		}
		if e <= 0 {
			if !dayNow && t < hRise {
				return t, fmt.Sprintf("до восхода ~%.0fч НЕ дотянем · %s", hRise, note)
			}
			return t, note
		}
	}
	return 48, note
}
