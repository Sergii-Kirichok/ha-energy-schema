# Set add-on option control_users=["stari40k"] keeping all other saved options; restart; print only control_users + state.
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr "\0" "\n" | grep -m1 "^SUPERVISOR_TOKEN=" | cut -d= -f2-)
S=1089f1d1_energy_schema; API=http://supervisor/addons/$S
OPTS=$(curl -s -H "Authorization: Bearer $TK" $API/info | jq -c '.data.options | select(type == "object") | .control_users = ["stari40k"]')
[ -n "$OPTS" ] || { echo "no options read, abort"; exit 1; }
printf '{"options":%s}' "$OPTS" | curl -s -X POST -H "Authorization: Bearer $TK" -H "Content-Type: application/json" -d @- $API/options | jq -c '{result}'
curl -s -H "Authorization: Bearer $TK" $API/info | jq -c '{control_users: .data.options.control_users}'
curl -s -X POST -H "Authorization: Bearer $TK" $API/restart | jq -c '{restart: .result}'
sleep 15; curl -s -H "Authorization: Bearer $TK" $API/info | jq -c '{state: .data.state, version: .data.version, ingress_panel: .data.ingress_panel}'
