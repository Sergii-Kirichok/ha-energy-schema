# Per-phase grid outage sensors (24 h) and their dashboard rows (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
for ph in 1 2 3; do
  e=sensor.energy_schema_grid_outage_l$ph
  echo "== $e"; curl -s -H "Authorization: Bearer $TK" "$API/states/$e" | grep -oE '"state":"[^"]*"|"(off|low)_[a-z_]+":"?[^",}]*' | tr '\n' ' '; echo
done
echo "dash rows: $(sudo grep -c energy_schema_grid_outage_l /config/.storage/lovelace.home_energy)"
