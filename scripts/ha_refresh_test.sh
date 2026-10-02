# Does homeassistant.update_entity force Solarman to re-read a settings entity? (no settings are changed)
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"; J="Content-Type: application/json"
E=number.deye_sun_30k_zero_export_power
lu() { curl -s -H "$H" $A/states/$E | grep -oE '"last_reported":"[^"]*"|"state":"[^"]*"' | tr '\n' ' '; }
echo "before: $(lu)"
curl -s -o /dev/null -w "update_entity http %{http_code}\n" -H "$H" -H "$J" -X POST $A/services/homeassistant/update_entity -d "{\"entity_id\":\"$E\"}"
sleep 3; echo "after 3s: $(lu)"
for e in number.deye_sun_30k_grid_voltage_protection_high number.deye_sun_30k_grid_voltage_protection_low number.deye_sun_30k_grid_frequency_protection_high number.deye_sun_30k_grid_frequency_protection_low number.deye_sun_30k_grid_reconnection_time number.deye_sun_30k_zero_export_power number.deye_sun_30k_battery_max_discharging_current select.deye_sun_30k_grid_voltage select.deye_sun_30k_grid_frequency time.deye_sun_30k_program_1_time switch.deye_sun_30k_battery_generator_charging; do
  printf '%s: ' $e; curl -s -H "$H" $A/states/$e | grep -oE '"state":"[^"]*"|"min":[0-9.]+|"max":[0-9.]+|"step":[0-9.]+|"unit_of_measurement":"[^"]*"' | tr '\n' ' '; echo; done
