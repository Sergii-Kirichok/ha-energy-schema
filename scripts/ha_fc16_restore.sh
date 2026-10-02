# Restore register 104 to 15 (150 W) with function 16, verifying with delays.
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"; J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $A/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -H "$H" -H "$J" -X POST "$A/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":104,\"count\":1}" | grep -oE '"104":[0-9]+'; }
echo "now: $(rd)"
for i in 1 2 3; do v=$(rd); [ "$v" = '"104":15' ] && break
  curl -s -o /dev/null -w "write 15 -> http %{http_code}\n" -H "$H" -H "$J" -X POST $A/services/solarman/write_multiple_holding_registers -d "{\"device\":\"$DEV\",\"register\":104,\"values\":[15]}"
  for t in 1 3 6; do sleep $t; echo "  +${t}s: $(rd)"; done; done
echo "final: $(rd)"
