: "${SLUG:=energy_schema}"
# Pick up the Supervisor token from a running add-on process (not in our env).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
export SUPERVISOR_TOKEN="$TK"

ha store reload >/dev/null 2>&1
echo "=== update $SLUG ==="
ha apps update "$SLUG" 2>&1 | tail -3
ha apps restart "$SLUG" >/dev/null 2>&1
sleep 12
echo "=== logs ==="
ha apps logs "$SLUG" 2>&1 | tail -5
echo "=== svg bytes ==="
sudo wc -c /config/www/energy_schema.svg 2>&1
echo "=== done ==="
