# Inverter page smoke test on HAOS (read-only: GET page and state, POST-guard).
A=http://1089f1d1-energy-schema:8099
echo "page: $(curl -s -o /dev/null -w '%{http_code} %{size_download}B' $A/inverter)"
curl -s $A/inverter/state | head -c 900; echo
echo "save via GET: $(curl -s -o /dev/null -w '%{http_code}' $A/inverter/save)"
echo "nav on schematic: $(curl -s $A/schematic.svg | grep -c 'data-nav="inverter"')"
