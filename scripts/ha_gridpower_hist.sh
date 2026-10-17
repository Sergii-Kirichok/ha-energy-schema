# Grid power sensor: distinct states and first/last change times over the last 14 h (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
S=$(date -u -d @$(( $(date +%s) - 50400 )) +%Y-%m-%dT%H:%M:%SZ); E=$(date -u +%Y-%m-%dT%H:%M:%SZ)
curl -s -H "Authorization: Bearer $TK" "$API/history/period/$S?end_time=$E&filter_entity_id=sensor.deye_sun_30k_grid_power&minimal_response&no_attributes" \
  | tr '{' '\n' | sed -nE 's/.*"state":"([^"]*)".*"last_changed":"([0-9-]+T[0-9:]{5}).*/\2 \1/p' \
  | awk '{c=($2=="unavailable"||$2=="unknown")?$2:(($2+0==0)?"zero":"num"); if(c!=p){print $1, $2, c; p=c}}' | head -40
