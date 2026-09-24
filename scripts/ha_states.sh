# Dump HA entity states matching a pattern plus the recent history of one
# entity. Runs on HAOS: make remote-sh SCRIPT=scripts/ha_states.sh
#   PAT  — regex over entity ids (default: Deye battery-related)
#   HIST — entity whose 7-day history to print
: "${PAT:=batt|soh|soc|capac|cycle|bms}"
: "${HIST:=sensor.deye_sun_30k_battery_soh}"
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
H="Authorization: Bearer $TK"
echo "=== $HIST (full state + attributes)"
curl -s -H "$H" "$API/states/$HIST"; echo
echo "=== entities matching /$PAT/ (id = state)"
curl -s -H "$H" "$API/states" | tr '{' '\n' | grep -i 'deye' | grep -iE "$PAT" \
  | sed -E 's/.*"entity_id":"([^"]+)".*"state":"([^"]*)".*/\1 = \2/' | sort
echo "=== $HIST history 7d (time state)"
S=$(date -u -d '7 days ago' +%Y-%m-%dT%H:%M:%S 2>/dev/null)
curl -s -H "$H" "$API/history/period/$S?filter_entity_id=$HIST&minimal_response&significant_changes_only" \
  | tr '{' '\n' | sed -nE 's/.*"state":"([^"]*)".*"last_changed":"([^"]*)".*/\2 \1/p' | tail -40
echo "=== done"
