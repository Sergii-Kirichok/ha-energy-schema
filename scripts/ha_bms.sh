# Show the BMS sensors the add-on publishes into HA (read-only).
#   make remote-sh SCRIPT=scripts/ha_bms.sh
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
for e in soh cell_max cell_min cell_delta balance cycles; do
  curl -s -H "Authorization: Bearer $TK" "$API/states/sensor.energy_schema_bms_$e" \
    | sed -E 's/.*"state":"([^"]*)".*"friendly_name":"([^"]*)".*"last_updated":"([^"]*)".*/\2 = \1  (\3)/'
  echo
done
