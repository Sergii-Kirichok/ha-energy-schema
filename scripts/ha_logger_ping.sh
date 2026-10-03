for h in 192.168.0.18 192.168.0.203; do printf "%s ping: " $h; ping -c 2 -W 2 $h >/dev/null 2>&1 && echo ok || echo FAIL; printf "%s tcp 8899: " $h; (nc -z -w 3 $h 8899 && echo open) 2>/dev/null || echo closed/timeout; done
sudo grep -oE "\"domain\": ?\"[a-z_]+\"|\"host\": ?\"[^\"]*\"" /config/.storage/core.config_entries | grep -B1 "192.168.0.(18|203)\"" 
