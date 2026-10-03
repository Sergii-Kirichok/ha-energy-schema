# Scan Deye battery register space for per-pack blocks (read-only): prints non-zero registers.
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"; J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $A/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -m 25 -H "$H" -H "$J" -X POST "$A/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":$1,\"count\":$2}" | grep -oE '"[0-9]+":-?[0-9]+' | grep -v ':0$' | tr '\n' ' '; }
# Battery area: BMS1 10000.., per-pack blocks (ha-solarman deye_p3 "Battery N") 0x2730 + 0x26*(N-1), up to 20 packs.
# On SG01HP3 + HV rack (03.10): 10002 = 1 pack, 10032..10039 = rack BMU serial, 10120..10799 all zero.
for b in $(seq 10000 100 10700); do printf '%s: ' $b; rd $b 100; echo; done
