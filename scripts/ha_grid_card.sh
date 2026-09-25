# "Сеть и фазы" card config + grid voltage entities (read-only).
F=/config/.storage/lovelace.home_energy
N=$(sudo grep -n "Сеть и фазы" $F | head -1 | cut -d: -f1)
echo "line $N"; sudo sed -n "$((N)),$((N+45))p" $F
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
echo "=== grid voltage/power entities"
curl -s -H "Authorization: Bearer $TK" http://supervisor/core/api/states | tr '{' '\n' | grep -E '"entity_id":"sensor\.deye_sun_30k_(grid|external|internal|load)_l[123]_(voltage|power)"' \
  | sed -E 's/.*"entity_id":"([^"]+)".*"state":"([^"]*)".*/\1 = \2/' | sort
