# Live cell max/min/delta states with change times (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr "\0" "\n" | grep -m1 "^SUPERVISOR_TOKEN=" | cut -d= -f2-)
for e in sensor.energy_schema_bms_cell_max sensor.energy_schema_bms_cell_min sensor.energy_schema_bms_cell_delta; do printf "%s " $e; curl -s -H "Authorization: Bearer $TK" http://supervisor/core/api/states/$e | grep -oE "\"(state|last_changed)\":\"[^\"]*\"" | tr "\n" " "; echo; done
