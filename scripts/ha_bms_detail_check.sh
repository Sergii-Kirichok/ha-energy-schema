# Verify BMS detail sensors and dashboard cards (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
for e in cell_max_loc cell_min_loc temp_max temp_min limits overcharge insulation weak_cell; do
  printf '%-14s ' $e; curl -s -H "Authorization: Bearer $TK" http://supervisor/core/api/states/sensor.energy_schema_bms_$e | grep -oE '"state":"[^"]*"|"where":"[^"]*"' | tr '\n' ' '; echo; done
echo "cards: weak=$(sudo grep -c energy_schema_bms_weak_cell /config/.storage/lovelace.home_energy) chart=$(sudo grep -c 'Ячейки · 24' /config/.storage/lovelace.home_energy)"
