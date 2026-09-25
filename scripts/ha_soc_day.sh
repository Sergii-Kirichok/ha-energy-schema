# Today's SOC and battery current timeline (read-only), one line per ~30 min.
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
S=$(date -u +%Y-%m-%dT06:00:00)
for e in sensor.deye_sun_30k_battery sensor.deye_sun_30k_battery_current sensor.deye_sun_30k_pv_power; do
  echo "=== $e"
  curl -s -H "Authorization: Bearer $TK" "$API/history/period/$S?filter_entity_id=$e&minimal_response&no_attributes" \
    | tr '{' '\n' | sed -nE 's/.*"state":"([^"]*)".*"last_changed":"[0-9-]+T([0-9]{2}:[0-9]{2}).*/\2 \1/p' \
    | awk '{k=substr($1,1,4) (substr($1,4,1)<3?"0":"3"); if(k!=p){print; p=k}}' | tr '\n' ' '; echo
done
