# Set add-on option charge_balance_stop_v (default 3.58) keeping all other saved options; restart; print only that option + state.
V=${V:-3.58}
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr "\0" "\n" | grep -m1 "^SUPERVISOR_TOKEN=" | cut -d= -f2-)
S=1089f1d1_energy_schema; API=http://supervisor/addons/$S
OPTS=$(curl -s -H "Authorization: Bearer $TK" $API/info | jq -c --argjson v "$V" '.data.options | select(type == "object") | .charge_balance_stop_v = $v')
[ -n "$OPTS" ] || { echo "no options read, abort"; exit 1; }
printf '{"options":%s}' "$OPTS" | curl -s -X POST -H "Authorization: Bearer $TK" -H "Content-Type: application/json" -d @- $API/options | jq -c '{result}'
curl -s -H "Authorization: Bearer $TK" $API/info | jq -c '{charge_balance_stop_v: .data.options.charge_balance_stop_v}'
curl -s -X POST -H "Authorization: Bearer $TK" $API/restart | jq -c '{restart: .result}'
sleep 20; curl -s -H "Authorization: Bearer $TK" $API/info | jq -c '{state: .data.state, version: .data.version}'
curl -s -H "Authorization: Bearer $TK" $API/logs | grep -E "balancing" | tail -3
