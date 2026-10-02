# Function-16 write test on register 104 (zero-export background): 15 → 16 → 15 (150 → 160 → 150 W).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"; J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $A/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -H "$H" -H "$J" -X POST "$A/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":104,\"count\":1}" | grep -oE '"104":[0-9]+'; }
wr() { curl -s -o /dev/null -w "write $1 -> http %{http_code}\n" -H "$H" -H "$J" -X POST $A/services/solarman/write_multiple_holding_registers -d "{\"device\":\"$DEV\",\"register\":104,\"values\":[$1]}"; }
echo "before: $(rd)"; wr 16; sleep 2; echo "after write 16: $(rd)"; wr 15; sleep 2; echo "restored: $(rd)"
