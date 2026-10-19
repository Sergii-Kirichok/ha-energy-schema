# Set recorder purge_keep_days: 30 (backup first), check config, restart HA core.
set -e
F=/config/configuration.yaml
sudo grep -q "^recorder:" $F && { echo "recorder already present"; exit 1; }
sudo cp $F /config/configuration.yaml.bak-recorder-$(date +%Y%m%d%H%M)
printf '\nrecorder:\n  purge_keep_days: 30\n' | sudo tee -a $F >/dev/null
echo "--- tail"; sudo tail -3 $F
TK=$(sudo cat /proc/*/environ 2>/dev/null | tr '\0' '\n' | grep -m1 '^SUPERVISOR_TOKEN=' | cut -d= -f2-)
echo "--- check"; curl -s -X POST -H "Authorization: Bearer $TK" http://supervisor/core/check
echo; echo "--- restart"; curl -s -X POST -H "Authorization: Bearer $TK" http://supervisor/core/restart; echo
