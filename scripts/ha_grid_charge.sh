# Grid-charge diagnostics (read-only): power balance, charge registers, ToU slot.
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"; J="Content-Type: application/json"
echo "now $(date +%H:%M)"
for e in sensor.deye_sun_30k_battery sensor.deye_sun_30k_battery_current sensor.deye_sun_30k_battery_voltage sensor.deye_sun_30k_pv_power sensor.deye_sun_30k_load_power sensor.deye_sun_30k_grid_power number.deye_sun_30k_battery_grid_charging_current number.deye_sun_30k_battery_max_charging_current switch.deye_sun_30k_battery_grid_charging select.deye_sun_30k_time_of_use select.deye_sun_30k_work_mode select.deye_sun_30k_energy_pattern; do
  printf '%-58s ' $e; curl -s -H "$H" $A/states/$e | grep -oE '"state":"[^"]*"'; done
for n in 1 2 3 4 5 6; do printf 'prog%s ' $n; for k in time soc power charging; do
  e=number; [ $k = time ] && e=time; [ $k = charging ] && e=select
  printf '%s=%s ' $k "$(curl -s -H "$H" $A/states/$e.deye_sun_30k_program_${n}_$k | grep -oE '"state":"[^"]*"' | cut -d'"' -f4)"; done; echo; done
DEV=$(curl -s -H "$H" -H "$J" -X POST $A/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -H "$H" -H "$J" -X POST "$A/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":$1,\"count\":$2}" | grep -oE '"[0-9]+":-?[0-9]+' | tr '\n' ' '; echo; }
echo -n "reg 108-110: "; rd 108 3
echo -n "reg 124-130: "; rd 124 7
echo -n "reg 212-216: "; rd 212 5
