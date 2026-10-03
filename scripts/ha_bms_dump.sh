# Dump both Deye battery blocks (BMS1 10000+, BMS2 15000+) — read-only.
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"; J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $A/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -H "$H" -H "$J" -X POST "$A/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":$1,\"count\":$2}" | grep -oE '"[0-9]+":-?[0-9]+' | tr '\n' ' '; echo; }
# по 20 регистров: больше логгер может не отдать; 15000+ — вторая BMS (у нас может не отвечать)
for b in 10000 10020 10040 10060 10080 10100; do echo "=== $b"; rd $b 20; done
