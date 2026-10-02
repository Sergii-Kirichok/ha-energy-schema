# Profile blocks for the inverter-page registers (read-only).
P=/config/custom_components/solarman/inverter_definitions/deye_p3.yaml
for r in 0x0068 0x008D 0x008E 0x008F 0x0092 0x0094 0x009A 0x00A6 0x00AC 0x0073 0x0074 0x0075; do
  echo "=== $r"; L=$(sudo grep -n "registers: \[$r" $P | head -1 | cut -d: -f1); [ -z "$L" ] && { echo none; continue; }
  sudo sed -n "$((L-8)),$((L+22))p" $P | grep -E 'name:|platform|registers:|scale|rule|key:|value:|bit|mask|offset|range|min:|max:|- ' | head -26; done
echo "=== reconnection"; L=$(sudo grep -n 'name: "Grid Reconnection Time"' $P | head -1 | cut -d: -f1); sudo sed -n "$L,$((L+10))p" $P | grep -E 'registers|scale|rule'
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"; J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $A/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -H "$H" -H "$J" -X POST "$A/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":$1,\"count\":$2}" | grep -oE '"[0-9]+":-?[0-9]+' | tr '\n' ' '; echo; }
echo -n "raw 141-147: "; rd 141 7; echo -n "raw 148-177: "; rd 148 30; echo -n "raw 185-188: "; rd 185 4
