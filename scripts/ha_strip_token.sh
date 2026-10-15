# Remove the obsolete ha_token/ha_url keys from the add-on's saved options and
# restart it. Runs on HAOS: make -s remote-sh SCRIPT=scripts/ha_strip_token.sh
# Prints key names and results only, never option values.
: "${SLUG:=energy_schema}"
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
export SUPERVISOR_TOKEN="$TK"
H="Authorization: Bearer $TK"
OPTS=$(curl -s -H "$H" "http://supervisor/addons/$SLUG/info" | jq -c '.data.options')
[ -n "$OPTS" ] && [ "$OPTS" != "null" ] || { echo "no options read"; exit 1; }
echo "present: $(printf '%s' "$OPTS" | jq -r '[keys[] | select(. == "ha_token" or . == "ha_url")] | join(",")')"
printf '%s' "$OPTS" | jq -c '{options: del(.ha_token, .ha_url)}' \
  | curl -s -X POST -H "$H" -H "Content-Type: application/json" --data-binary @- \
      "http://supervisor/addons/$SLUG/options" | jq -c '{result}'
echo "left: $(curl -s -H "$H" "http://supervisor/addons/$SLUG/info" | jq -r '[.data.options | keys[] | select(. == "ha_token" or . == "ha_url")] | join(",")')"
ha apps restart "$SLUG" >/dev/null 2>&1 && echo "restarted"
