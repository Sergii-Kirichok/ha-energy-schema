# Charge controller state: helpers, setpoint sensors, inverter registers 108/110/128 (read-only).
#   make remote-sh SCRIPT=scripts/ha_charge_state.sh
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
H="Authorization: Bearer $TK"
J="Content-Type: application/json"
for e in input_boolean.energy_schema_charge_auto input_number.energy_schema_charge_max_a input_number.energy_schema_charge_grid_a input_number.energy_schema_charge_taper_soc input_number.energy_schema_charge_target_soc input_number.energy_schema_charge_full_days sensor.energy_schema_charge_setpoint sensor.energy_schema_charge_next_full number.deye_sun_30k_battery_max_charging_current number.deye_sun_30k_battery_grid_charging_current sensor.deye_sun_30k_battery sensor.deye_sun_30k_battery_current; do
  printf '%-52s ' "$e"; curl -s -H "$H" "$API/states/$e" | grep -oE '"state":"[^"]*"|"mode":"[^"]*"|"friendly_name":"[^"]*"' | tr '\n' ' '; echo
done
DEV=$(curl -s -H "$H" -H "$J" -X POST $API/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
echo "=== regs 108..110, 128"; curl -s -H "$H" -H "$J" -X POST "$API/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":108,\"count\":3}"; echo; curl -s -H "$H" -H "$J" -X POST "$API/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":128,\"count\":1}"; echo
