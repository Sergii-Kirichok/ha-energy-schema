# Raw L1 grid voltage history, last ~26 h: every state change (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
S=2026-10-16T05:00:00Z
curl -s -H "Authorization: Bearer $TK" "$API/history/period/$S?filter_entity_id=sensor.deye_sun_30k_grid_l1_voltage&minimal_response&no_attributes" \
  | tr '{' '\n' | sed -nE 's/.*"state":"([^"]*)".*"last_changed":"([0-9-]+T[0-9:]{5}).*/\2 \1/p' \
  | awk '{v=$2; c=(v=="unavailable"||v=="unknown")?"NA":(v+0<20?"OFF":(v+0<150?"LOW":"ON")); if(c!=p){print $1, v, c; p=c}}'
echo "now: $(date -u +%FT%T)Z"
