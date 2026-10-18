# Read-only: top-level cards of home-energy (type, title, entity ids, visibility) + dashboard access flags.
F=/config/.storage/lovelace.home_energy
sudo jq -r '.data.config.views[] | "== view: \(.title // .path)", ((.sections // [{cards: .cards}])[] | (.cards // [])[] |
  "\(.type) | \(.title // .heading // .name // "-") | vis=\(.visibility // [] | map(.condition) | join(",")) | \([.. | strings | select(test("^(input_boolean|input_number|input_button|input_text|input_select|switch|number|button|select)\\.[a-z0-9_]+$"))] | unique | join(" "))")' $F
echo "--- dashboards"; sudo jq -c '.data.items[] | {url_path, title, require_admin, show_in_sidebar}' /config/.storage/lovelace_dashboards
