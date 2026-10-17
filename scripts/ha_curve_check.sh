# Count stepline curves in the dashboard (read-only).
echo "stepline: $(sudo grep -c "\"curve\": \"stepline\"" /config/.storage/lovelace.home_energy)"
