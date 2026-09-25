# Charge-current control surface (read-only): raw battery registers under load,
# Time-of-Use program entities, BMS limits. make remote-sh SCRIPT=scripts/ha_charge.sh
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
H="Authorization: Bearer $TK"
J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $API/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -H "$H" -H "$J" -X POST "$API/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":$1,\"count\":$2}"; echo; }
echo "=== live: pv/load/grid/batt"
for e in sensor.deye_sun_30k_pv_power sensor.deye_sun_30k_load_power sensor.deye_sun_30k_grid_power sensor.deye_sun_30k_battery_power sensor.deye_sun_30k_battery_current sensor.deye_sun_30k_battery_voltage sensor.deye_sun_30k_battery; do
  printf '%s = ' "$e"; curl -s -H "$H" "$API/states/$e" | grep -oE '"state":"[^"]*"' | head -1; done
echo "=== raw 108,109 (max charge/discharge A), 212/213 (BMS limits), 214-217 (soc,V,I,T), 10003-10006"
rd 108 2; rd 212 6; rd 10003 4
echo "=== raw 141..170 (ToU: sell mode, program times/power/soc)"; rd 141 30
echo "=== HA entities: program / time of use / sell / max power"
curl -s -H "$H" $API/states | tr '{' '\n' | grep -iE '"entity_id":"[^"]*deye[^"]*(program|time_of_use|sell|max_sell|solar_sell|export|charge)[^"]*"' \
  | sed -E 's/.*"entity_id":"([^"]+)".*"state":"([^"]*)".*/\1 = \2/' | sort
echo "=== done"
