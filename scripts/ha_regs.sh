# Read raw inverter holding registers through the HA Solarman integration
# (service solarman.read_holding_registers, read-only). Runs on HAOS:
#   make remote-sh SCRIPT=scripts/ha_regs.sh            # default: BMS SOH block
#   BLOCKS="10040:17 210:16" make remote-sh SCRIPT=...   # custom "addr:count" blocks
# Known Deye HV (SG01HP3) battery registers (MODBUS RTU V104 doc):
#   10005 BMS SOC 1%   10006 BMS SOH 1%   10007 remaining Ah
#   10046 cycles       10050 pack SOC     10051 pack SOH    10052/10055 max/min cell mV
#   214 BMS SOC        215 voltage 0.1V   216 current 0.1A  217 temp (x-1000)/10
: "${ENT:=sensor.deye_sun_30k_battery}"
: "${BLOCKS:=10000:13 10040:17}"
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
H="Authorization: Bearer $TK"
J="Content-Type: application/json"
DEV=$(curl -s -H "$H" -H "$J" -X POST $API/template -d "{\"template\":\"{{ device_id('$ENT') }}\"}")
echo "=== device_id=$DEV"
for b in $BLOCKS; do
  a=${b%%:*}; n=${b##*:}
  echo "=== holding $a (0x$(printf %X "$a")) count $n"
  curl -s -w ' [http %{http_code}]' -H "$H" -H "$J" -X POST "$API/services/solarman/read_holding_registers?return_response" \
    -d "{\"device\":\"$DEV\",\"address\":$a,\"count\":$n}"; echo
done
echo "=== done"
