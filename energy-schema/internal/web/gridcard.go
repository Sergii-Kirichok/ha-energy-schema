package web

// Карточка «Сеть и фазы» — markdown вместо entities: у entities-строк цвет
// иконки не задать (card-mod не установлен), а markdown HA пропускает
// <font color> и <ha-icon>. Иконка фазы — по текущему напряжению (цвета как в
// scada/theme.go): 207–253 В зелёная, выше — красная, 20–207 — оранжевая,
// <20 В или нет данных — красная mdi:transmission-tower-off. Имя фазы берётся
// из friendly_name сенсора (итог отключений за 24 ч), значение — его state.

// gridCardMark finds an already converted card (also with an older template).
const gridCardMark = "'sensor.energy_schema_grid_l' ~ n"

const gridCardMD = `<table width="100%">
<tr><td width="40"><ha-icon icon="mdi:transmission-tower"></ha-icon></td><td>Мощность сети</td><td align="right">{{ states('sensor.deye_sun_30k_grid_power') }} W</td></tr>
<tr><td width="40"><ha-icon icon="mdi:sine-wave"></ha-icon></td><td>Частота</td><td align="right">{{ states('sensor.deye_sun_30k_grid_frequency') }} Hz</td></tr>
{%- for n in [1, 2, 3] %}
{%- set g = 'sensor.energy_schema_grid_l' ~ n %}
{%- set v = states('sensor.deye_sun_30k_grid_l' ~ n ~ '_voltage') | float(0) %}
{%- set c = '#ef4444' if v < 20 or v > 253 else ('#f59e0b' if v < 207 else '#22c55e') %}
<tr><td width="40"><font color="{{ c }}"><ha-icon icon="mdi:transmission-tower{{ '-off' if v < 20 else '' }}"></ha-icon></font></td><td>{{ state_attr(g, 'friendly_name') or 'L' ~ n }}</td><td align="right">{{ states(g) }}</td></tr>
{%- endfor %}
</table>`

// patchGridCard turns the entities card with the L1..L3 rows into the markdown
// card in place (title, visibility and position kept; old rows incl. 0.6.56
// outage rows dropped) and refreshes the template of an already converted card.
func patchGridCard(node any) (bool, error) {
	if md := findMarkdownWith(node, gridCardMark); md != nil {
		if md["content"] == gridCardMD {
			return false, nil
		}
		md["content"] = gridCardMD
		return true, nil
	}
	card := findCardWith(node, gridRowEntity(1))
	if card == nil {
		return false, nil
	}
	for _, k := range []string{"entities", "state_color", "show_header_toggle"} {
		delete(card, k)
	}
	card["type"], card["content"] = "markdown", gridCardMD
	return true, nil
}
