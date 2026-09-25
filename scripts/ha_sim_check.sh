# Emulator entities in HA: heartbeat age + the three ATS (read-only).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
API=http://supervisor/core/api
H="Authorization: Bearer $TK"
hb=$(curl -s -H "$H" $API/states/sensor.sim_heartbeat | grep -oE '"state":"[0-9]+"' | grep -oE '[0-9]+')
echo "heartbeat age: $(( $(date +%s) - ${hb:-0} )) s"
for e in sim_contactor sim_contactor_link sim_avr_pos sim_avr_mode sim_avr_link sim_avr3_pos sim_avr3_mode sim_avr3_link; do
  printf '%-22s ' $e; curl -s -H "$H" $API/states/sensor.$e | grep -oE '"state":"[^"]*"|"friendly_name":"[^"]*"' | tr '\n' ' '; echo
done
