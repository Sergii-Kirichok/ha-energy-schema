# Verify the flip card is served and the param route works (read-only: delta 0 is rejected).
A=http://1089f1d1-energy-schema:8099
echo "=== svg markers"; curl -s $A/schematic.svg | grep -oE 'id="card-batt"|class="face f-(front|back)"|data-flip="batt"|data-set="[a-z_]+:[^"]+"' | sort | uniq -c
echo "=== html markers"; curl -s $A/ | grep -oE 'function (flips|setp|wire)|\.flipped>\.f-back' | sort -u
echo "=== param route (delta 0 must be 400)"; curl -s -o /dev/null -w 'http %{http_code}\n' -X POST "$A/control?act=param&val=max_a:0"
curl -s -X POST "$A/control?act=param&val=max_a:0"; echo
echo "=== GET must be 405"; curl -s -o /dev/null -w 'http %{http_code}\n' "$A/control?act=param&val=max_a:+5"
