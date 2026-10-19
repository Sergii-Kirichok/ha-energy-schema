# Imbalance journal: file lines, sensor state, add-on log lines; history size check for the backfill (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
for f in /share/energy_schema/imbalance.jsonl /mnt/data/supervisor/share/energy_schema/imbalance.jsonl; do
  sudo test -f "$f" && { echo "=== $f ($(sudo wc -l < "$f") lines)"; sudo cat "$f"; }
done
echo "=== sensor"; curl -s -H "Authorization: Bearer $TK" "$API/states/sensor.energy_schema_imbalance_last" | head -c 600; echo
E=$(date -u +%Y-%m-%dT%H:%M:%SZ)
for e in sensor.deye_sun_30k_battery_current sensor.energy_schema_bms_cell_max; do
  printf '%s history since 10-14: ' "$e"
  curl -s -o /dev/null -w '%{size_download} bytes %{time_total} s\n' -H "Authorization: Bearer $TK" "$API/history/period/2026-10-14T00:00:00Z?filter_entity_id=$e&end_time=$E&minimal_response"
done
SLUG=$(curl -s -H "Authorization: Bearer $TK" http://supervisor/addons | grep -oE '"slug":"[^"]*energy[^"]*"' | head -1 | cut -d'"' -f4)
echo "=== log ($SLUG)"; curl -s -H "Authorization: Bearer $TK" "http://supervisor/addons/$SLUG/logs" | grep -iE "imbalance|error|panic" | tail -15
