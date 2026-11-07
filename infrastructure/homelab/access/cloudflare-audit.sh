#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source "$script_dir/cloudflare-common.sh"
cf_initialize

printf 'Cloudflare read-only Access and DNS audit\n'
apps=$(cf_api GET "/accounts/$CF_ACCOUNT/access/apps?per_page=100")
jq -r '
  .[] | select(.type == "self_hosted" or .type == "ssh" or .type == "mcp") | . as $app |
  "ACCESS id=\(.id // "") name=\(.name // "") domain=\(.domain // "") aud=\(.aud // "") policy_count=\((.policies // []) | length)",
  (.policies // [] | .[] |
    "POLICY app=\($app.id // "") decision=\(.decision // "") selectors=\(([(.include // []),(.require // []),(.exclude // [])] | flatten | map(keys) | flatten | unique) | join(","))")
' <<<"$apps"
zones=$(cf_api GET '/zones?name=dbontr.me')
while IFS= read -r zone; do
  zone_id=$(jq -r '.id' <<<"$zone")
  jq -r '"ZONE \(.id) status=\(.status // "")"' <<<"$zone"
  for host in dbontr.me recipes.dbontr.me media.dbontr.me \
              monitor.dbontr.me mcp.dbontr.me; do
    records=$(cf_api GET "/zones/$zone_id/dns_records?name=$host")
    jq -r --arg host "$host" '
      .[] | "DNS \($host) \(.type // "") proxied=\(.proxied // false) target=\((.content // "")[0:90])"
    ' <<<"$records"
  done
done < <(jq -c '.[] | select(.name == "dbontr.me")' <<<"$zones")
