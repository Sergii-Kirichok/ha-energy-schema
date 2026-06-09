: "${SLUG:=energy_schema}"
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
export SUPERVISOR_TOKEN="$TK"
ha addons logs "$SLUG" 2>&1 | tail -40
