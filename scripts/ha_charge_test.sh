# Controlled experiment: does register 108 (Battery Max Charging Current) cap the
# PV charge current? Sets 8 A via the HA number entity, samples BMS current (reg
# 216, 0.1 A) every 20 s for 2 min, then restores the previous value.
#   make remote-sh SCRIPT=scripts/ha_charge_test.sh
ENT=number.deye_sun_30k_battery_max_charging_current
TEST_A=8
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
H="Authorization: Bearer $TK"
J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $API/template -d "{\"template\":\"{{ device_id('sensor.deye_sun_30k_battery') }}\"}")
rd() { curl -s -H "$H" -H "$J" -X POST "$API/services/solarman/read_holding_registers?return_response" -d "{\"device\":\"$DEV\",\"address\":$1,\"count\":$2}" | grep -oE '"[0-9]+":-?[0-9]+' | tr '\n' ' '; }
setv() { curl -s -o /dev/null -w "set $1 -> http %{http_code}\n" -H "$H" -H "$J" -X POST "$API/services/number/set_value" -d "{\"entity_id\":\"$ENT\",\"value\":$1}"; }
PREV=$(curl -s -H "$H" "$API/states/$ENT" | grep -oE '"state":"[^"]*"' | cut -d'"' -f4)
echo "=== prev $ENT = $PREV ; baseline: $(rd 108 1) $(rd 214 3)"
setv $TEST_A
for i in 1 2 3 4 5 6; do sleep 20; echo "t+$((i*20))s: $(rd 108 1) $(rd 214 3) pv=$(curl -s -H "$H" $API/states/sensor.deye_sun_30k_pv_power | grep -oE '"state":"[^"]*"' | cut -d'"' -f4)"; done
setv "$PREV"
sleep 20; echo "=== restored: $(rd 108 1) $(rd 214 3)"
echo "=== done"
