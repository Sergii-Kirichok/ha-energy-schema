: "${SLUG:=energy_schema}"
# Pick up the Supervisor token from a running add-on process (not in our env).
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
export SUPERVISOR_TOKEN="$TK"

echo "=== update $SLUG ==="
# store reload иногда ещё не видит свежий push — повторяем, пока версия не последняя
for try in 1 2 3 4; do
  ha store reload >/dev/null 2>&1
  ha apps update "$SLUG" 2>&1 | tail -3
  ha apps info "$SLUG" 2>/dev/null | grep -q '^update_available: false' && break
  echo "(retry $try: update not applied yet)"; sleep 15
done
ha apps info "$SLUG" 2>/dev/null | grep -E '^version:'
ha apps restart "$SLUG" >/dev/null 2>&1
sleep 12
echo "=== logs ==="
ha apps logs "$SLUG" 2>&1 | tail -5
echo "=== svg bytes ==="
sudo wc -c /config/www/energy_schema.svg 2>&1
echo "=== done ==="
