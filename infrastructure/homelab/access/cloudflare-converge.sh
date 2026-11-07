#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source "$script_dir/cloudflare-common.sh"
cf_initialize
[[ $# == 0 || ( $# == 1 && $1 == --apply ) ]] ||
  cf_fail 'Usage: cloudflare-converge.sh [--apply]'
apply=false
[[ $# == 0 ]] || apply=true
apps_path="/accounts/$CF_ACCOUNT/access/apps"
apps=$(cf_api GET "$apps_path?per_page=100")
jq -e 'type == "array"' <<<"$apps" >/dev/null ||
  cf_fail 'Unexpected application listing'

match_app() {
  local domain=$1
  jq -c --arg domain "$domain" '
    [.[] | select(.domain == $domain and .type == "self_hosted")] |
    if length > 1 then error("Duplicate Access application") else (.[0] // null) end
  ' <<<"$apps"
}

validate_owner_shape() {
  jq -e '
    (.policies | type == "array" and length == 1) and
    (.policies[0].decision == "allow") and
    (.policies[0].include | type == "array" and length > 0 and
      all(.[]; type == "object" and (keys == ["email"]) and
        (.email | type == "object" and (.email | type == "string" and length > 0))))
  ' <<<"$1" >/dev/null || cf_fail 'Access policy is not a strict owner email allowlist'
}

validate_policy() {
  local candidate=$1 owner=$2
  validate_owner_shape "$candidate"
  printf '%s\n%s\n' "$candidate" "$owner" |
    jq -se '
      .[0].policies[0].include == .[1].include and
      (.[0].policies[0].require // []) == .[1].require
    ' >/dev/null || cf_fail 'Owner Access policy does not match the apex'
}

apex=$(match_app dbontr.me)
[[ $apex != null ]] || cf_fail 'Protected apex Access application is missing'
validate_owner_shape "$apex"
owner=$(jq -c '{
  name: "Owner only", decision: "allow",
  include: .policies[0].include,
  exclude: (.policies[0].exclude // []),
  require: (.policies[0].require // [])
}' <<<"$apex")
[[ $(match_app '*.dbontr.me') == null ]] ||
  cf_fail 'Wildcard Access application must not cover private and guest services'

gateway=$(match_app mcp.dbontr.me)
[[ $gateway != null ]] || cf_fail 'The protected MCP gateway is missing'
validate_policy "$gateway" "$owner"
printf 'VERIFIED mcp.dbontr.me aud=%s\n' "$(jq -r '.aud // empty' <<<"$gateway")"

monitor=$(match_app monitor.dbontr.me)
if [[ $monitor != null ]]; then
  validate_policy "$monitor" "$owner"
  printf 'VERIFIED monitor.dbontr.me aud=%s\n' "$(jq -r '.aud // empty' <<<"$monitor")"
elif [[ $apply == false ]]; then
  printf 'PLAN create owner-only Access app monitor.dbontr.me\n'
else
  body=$(printf '%s\n' "$owner" |
    jq -c '{type:"self_hosted",domain:"monitor.dbontr.me",name:"DBONTR Monitoring",session_duration:"24h",policies:[.]}')
  monitor=$(cf_api POST "$apps_path" "$body")
  validate_policy "$monitor" "$owner"
  printf 'CREATED monitor.dbontr.me\n'
fi

if [[ $apply == true ]]; then
  apps=$(cf_api GET "$apps_path?per_page=100")
  [[ $(match_app '*.dbontr.me') == null ]] ||
    cf_fail 'Wildcard Access application remains'
  for host in mcp.dbontr.me monitor.dbontr.me; do
    current=$(match_app "$host")
    [[ $current != null ]] || cf_fail "Missing protected application at $host"
    validate_policy "$current" "$owner"
  done
  zones=$(cf_api GET '/zones?name=dbontr.me')
  zone=$(jq -c '[.[] | select(.name == "dbontr.me")] | if length == 1 then .[0] else error("Zone not uniquely accessible") end' <<<"$zones")
  zone_id=$(jq -r '.id' <<<"$zone")
  tunnel=132a5be7-22b0-4697-a46a-4683bcee9a25.cfargotunnel.com
  for host in monitor.dbontr.me; do
    path="/zones/$zone_id/dns_records"
    existing=$(cf_api GET "$path?name=$host")
    if [[ $(jq -r 'length' <<<"$existing") != 0 ]]; then
      jq -e --arg tunnel "$tunnel" '
        length == 1 and .[0].type == "CNAME" and
        .[0].content == $tunnel and .[0].proxied == true
      ' <<<"$existing" >/dev/null || cf_fail "Conflicting DNS record for $host"
      printf 'DNS VERIFIED %s\n' "$host"
    else
      body=$(jq -nc --arg host "$host" --arg tunnel "$tunnel" \
        '{type:"CNAME",name:$host,content:$tunnel,proxied:true,ttl:1}')
      cf_api POST "$path" "$body" >/dev/null
      printf 'DNS CREATED %s\n' "$host"
    fi
  done
fi
printf 'No credentials or owner email identities were printed.\n'
