# Read-only: the «Сеть и фазы» card (any card mentioning energy_schema_grid_l1), phase sensors,
# and the markdown template rendered by HA (POST /api/template).
F=/config/.storage/lovelace.home_energy
sudo jq -c '[.. | objects | select(has("type") and ((.entities // .content // "") | tostring | test("energy_schema_grid_l"))) | {type, rows: (.entities // [] | length), has_content: has("content")}]' $F
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
for ph in 1 2 3; do printf 'L%s = ' $ph; curl -s -H "Authorization: Bearer $TK" http://supervisor/core/api/states/sensor.energy_schema_grid_l$ph | grep -oE '"(state|friendly_name)":"[^"]*"' | tr '\n' ' '; echo; done
echo "--- rendered"
sudo jq -r 'first(.. | objects | select(.type == "markdown" and ((.content // "") | test("energy_schema_grid_l"))) | .content)' $F \
  | jq -Rs '{template: .}' \
  | curl -s -X POST -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' -d @- http://supervisor/core/api/template
echo
