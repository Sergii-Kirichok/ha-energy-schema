# Verify phase rows: card entities + published combined sensors (read-only).
F=/config/.storage/lovelace.home_energy
echo "card rows: $(sudo grep -c 'energy_schema_grid_l[123]' $F), old power rows: $(sudo grep -c 'grid_l[123]_power' $F)"
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
for ph in 1 2 3; do printf 'L%s = ' $ph; curl -s -H "Authorization: Bearer $TK" http://supervisor/core/api/states/sensor.energy_schema_grid_l$ph | grep -oE '"state":"[^"]*"'; done
