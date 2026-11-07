#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source "$script_dir/cloudflare-common.sh"
cf_initialize
[[ $# == 1 || ( $# == 2 && $2 == --apply ) ]] ||
  cf_fail 'Usage: export-audiences.sh cloudflare.nix [--apply]'
nix_file=$1
apply=false
[[ $# == 1 ]] || apply=true
[[ -f $nix_file && ! -L $nix_file ]] || cf_fail 'Nix source must be a regular file'
apps=$(cf_api GET "/accounts/$CF_ACCOUNT/access/apps?per_page=100")
jq -e 'type == "array" and all(.[]; .domain != "*.dbontr.me")' <<<"$apps" >/dev/null ||
  cf_fail 'Wildcard Access application remains'

lookup_audience() {
  local host=$1 entries app
  entries=$(jq -c --arg host "$host" '[.[] | select(.domain == $host and .type == "self_hosted")]' <<<"$apps")
  [[ $(jq -r 'length' <<<"$entries") == 1 ]] ||
    cf_fail "Missing or duplicate Access application for $host"
  app=$(jq -c '.[0]' <<<"$entries")
  jq -e '
    (.policies | type == "array" and length == 1) and
    (.policies[0].decision == "allow") and
    (.policies[0].include | type == "array" and length > 0 and
      all(.[]; type == "object" and (keys == ["email"])))
  ' <<<"$app" >/dev/null || cf_fail "Access policy not strict for $host"
  local aud
  aud=$(jq -r '.aud // empty' <<<"$app")
  [[ $aud =~ ^[0-9a-f]{64}$ ]] || cf_fail "Invalid audience ID at $host"
  printf '%s' "$aud"
}
monitor_aud=$(lookup_audience monitor.dbontr.me)

# Rewrite only the monitoring audience default. Do not touch the tunnel,
# firewall, or any unrelated authentication policy.
scratch=$(mktemp "$(dirname -- "$nix_file")/.cloudflare-aud.XXXXXXXX")
trap 'rm -f -- "$scratch"' EXIT
awk -v monitor="$monitor_aud" '
  /monitorAudTag[[:space:]]*=[[:space:]]*lib[.]mkOption/ { target="monitor" }
  target != "" && /default[[:space:]]*=/ {
    if ($0 !~ /default[[:space:]]*=[[:space:]]*(null|"[0-9a-f]+")[[:space:]]*;/) exit 1
    value = monitor
    sub(/default[[:space:]]*=[[:space:]]*(null|"[0-9a-f]+")[[:space:]]*;/,
        "default = \"" value "\";")
    count[target]++
    target=""
  }
  { print }
  END { if (count["monitor"] != 1) exit 1 }
' "$nix_file" > "$scratch" || cf_fail 'Cannot uniquely update Nix Access audience defaults'
if cmp -s "$nix_file" "$scratch"; then
  printf 'Cloudflare audience defaults already match\n'
elif [[ $apply == false ]]; then
  printf 'PLAN pin owner-only Cloudflare audiences in %s\n' "$nix_file"
else
  chmod --reference="$nix_file" "$scratch"
  chown --reference="$nix_file" "$scratch"
  mv -f -- "$scratch" "$nix_file"
  printf 'Pinned owner-only Cloudflare audiences\n'
fi
