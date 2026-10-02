# Map inverter-page fields to Solarman profile entries and live entities (read-only).
P=/config/custom_components/solarman/inverter_definitions/deye_p3.yaml
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
A=http://supervisor/core/api; H="Authorization: Bearer $TK"
echo "=== profile: grid protection / voltage / frequency / reconnect entries"
sudo grep -n -i -B1 -A10 'name: "Grid \(Over\|Under\|High\|Low\|Voltage High\|Voltage Low\|Frequency High\|Frequency Low\|Reconnect\)\|name: ".*\(Protect\|Reconnect\|Over Voltage\|Under Voltage\|Over Frequency\|Under Frequency\).*"' $P | grep -E 'name:|platform|registers:|scale|rule|min:|max:|uom' | head -60
echo "=== profile: work mode / energy pattern / time of use / program charging lookup"
for n in "Work Mode" "Energy Pattern" "Time of Use" "Program 1 Charging" "Program 1 Time" "Program 1 SOC" "Program 1 Power" "Zero Export Power" "Battery Generator Charging\"" "Battery Grid Charging\"" "Battery Max Discharging Current" "Battery Shutdown SOC" "Battery Low SOC" "Battery Restart SOC" "Grid Max Export Power"; do
  echo "--- $n"; sudo grep -n -A14 "name: \"$n" $P | grep -E 'platform|registers:|scale|rule|key:|value:|min:|max:|mask|bit' | head -14; done
echo "=== HA services of solarman"
curl -s -H "$H" $A/services | tr '{' '\n' | grep -o '"solarman":{[^}]*\|"write_holding_register[a-z_]*"\|"read_[a-z_]*"' | head
echo "=== entity states & options"
for e in select.deye_sun_30k_work_mode select.deye_sun_30k_energy_pattern select.deye_sun_30k_time_of_use select.deye_sun_30k_program_1_charging; do
  printf '%s: ' $e; curl -s -H "$H" $A/states/$e | grep -oE '"state":"[^"]*"|"options":\[[^]]*\]' | tr '\n' ' '; echo; done
curl -s -H "$H" $A/states | tr '{' '\n' | grep -oE '"entity_id":"[a-z]+\.deye_sun_30k_[a-z0-9_]*(grid|volt|freq|protect|reconnect)[a-z0-9_]*"' | sort -u | head -40
