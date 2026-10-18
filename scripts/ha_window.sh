# All state changes of a few entities in the last 25 min (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
S=$(date -u -d @$(( $(date +%s) - 1500 )) +%Y-%m-%dT%H:%M:%SZ); E=$(date -u +%Y-%m-%dT%H:%M:%SZ)
for e in sensor.deye_sun_30k_battery_current sensor.deye_sun_30k_grid_l1_voltage sensor.deye_sun_30k_grid_power sensor.deye_sun_30k_load_power sensor.energy_schema_bms_limits; do
  echo "=== $e"
  curl -s -H "Authorization: Bearer $TK" "$API/history/period/$S?end_time=$E&filter_entity_id=$e&minimal_response&no_attributes" \
    | tr '{' '\n' | sed -nE 's/.*"state":"([^"]*)".*"last_changed":"[0-9-]+T([0-9:]{8}).*/\2 \1/p' | tr '\n' '|'; echo
done
