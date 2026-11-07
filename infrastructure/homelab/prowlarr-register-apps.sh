#!/usr/bin/env bash
set -euo pipefail
umask 077

die() { printf 'Registration failed: %s\n' "$*" >&2; exit 1; }
prowlarr=http://127.0.0.1:9696

api_key() {
  local file=$1 key
  [[ -f $file && ! -L $file ]] || die 'Missing native application XML config'
  key=$(xmlstarlet sel -t -v '/Config/ApiKey' "$file") ||
    die 'Cannot read application API key'
  [[ $key =~ ^[[:alnum:]]+$ ]] || die 'Missing or malformed native API key'
  printf '%s' "$key"
}

api() {
  local path=$1 key=$2 method=${3:-GET} payload=${4:-} body=''
  local output
  if [[ -n $payload ]]; then
    body=$(mktemp)
    chmod 0600 "$body"
    printf '%s' "$payload" > "$body"
  fi
  # curl receives its secret header from a private configuration stream,
  # not from process arguments or environment variables.
  if [[ -n $body ]]; then
    output=$(curl --config <(printf 'header = "X-Api-Key: %s"\n' "$key") \
      --noproxy '*' --silent --show-error --fail --max-time 12 --max-filesize 1048576 \
      --header 'Content-Type: application/json' --request "$method" \
      --data-binary @"$body" "$prowlarr$path") ||
      { rm -f -- "$body"; die "Prowlarr $path connection or API error"; }
    rm -f -- "$body"
  else
    output=$(curl --config <(printf 'header = "X-Api-Key: %s"\n' "$key") \
      --noproxy '*' --silent --show-error --fail --max-time 12 --max-filesize 1048576 \
      --header 'Content-Type: application/json' --request "$method" \
      "$prowlarr$path") || die "Prowlarr $path connection or API error"
  fi
  if [[ -n $output ]]; then
    jq -ec '.' <<<"$output" || die "Invalid Prowlarr JSON at $path"
  else
    printf 'null\n'
  fi
}

register() {
  for tool in jq curl xmlstarlet stat; do
    command -v "$tool" >/dev/null || die "Required tool unavailable: $tool"
  done
  [[ $EUID == 0 && -e /run/netns/download-vpn ]] ||
    die 'Use root inside the download-vpn network namespace'
  current_ns=$(stat -Lc %i /proc/self/ns/net)
  vpn_ns=$(stat -Lc %i /run/netns/download-vpn)
  [[ $current_ns == "$vpn_ns" ]] ||
    die 'Refusing registration outside download-vpn network namespace'

  local key current schemas existing template candidate secret body tested created name base file
  key=$(api_key /var/lib/private/prowlarr/config.xml)
  current=$(api '/api/v1/applications' "$key")
  schemas=$(api '/api/v1/applications/schema' "$key")
  jq -e 'type == "array"' <<<"$current" >/dev/null || die 'Unexpected Prowlarr applications'
  jq -e 'type == "array"' <<<"$schemas" >/dev/null || die 'Unexpected Prowlarr schemas'

  for name in Radarr Sonarr; do
    if [[ $name == Radarr ]]; then
      base=http://127.0.0.1:7878
      file=/var/lib/radarr/.config/Radarr/config.xml
    else
      base=http://127.0.0.1:8989
      file=/var/lib/sonarr/.config/NzbDrone/config.xml
    fi
    existing=$(jq -c --arg name "$name" '[.[] | select(.name == $name)]' <<<"$current")
    if [[ $(jq -r 'length' <<<"$existing") != 0 ]]; then
      jq -e --arg name "$name" 'length == 1 and .[0].implementation == $name' \
        <<<"$existing" >/dev/null || die "Conflicting $name registration"
      printf '%s: already registered; not overwritten\n' "$name"
      continue
    fi
    template=$(jq -c --arg name "$name" '[.[] | select(.implementation == $name)]' <<<"$schemas")
    [[ $(jq -r 'length' <<<"$template") == 1 ]] ||
      die "Expected exactly one $name application schema"
    secret=$(api_key "$file")
    candidate=$(jq -ce --arg name "$name" --arg base "$base" \
      --arg prowlarr "$prowlarr" --rawfile secret <(printf '%s' "$secret") '
        .[0] | .name = $name | .syncLevel = "fullSync" |
        .fields |= map(
          if .name == "apiKey" then .value = $secret
          elif .name == "prowlarrUrl" then .value = $prowlarr
          elif .name == "baseUrl" then .value = $base else . end
        ) |
        if ([.fields[].name] | (index("apiKey") != null and
           index("prowlarrUrl") != null and index("baseUrl") != null))
        then . else error("Incomplete application schema") end
      ' <<<"$template") || die "Invalid $name application schema"
    tested=$(api '/api/v1/applications/test' "$key" POST "$candidate")
    jq -e 'type != "array" or length == 0' <<<"$tested" >/dev/null ||
      die "$name connection test failed"
    created=$(api '/api/v1/applications' "$key" POST "$candidate")
    jq -e --arg name "$name" 'type == "object" and .name == $name and (.id != null)' \
      <<<"$created" >/dev/null || die "$name registration was not confirmed"
    printf '%s: authenticated application registration succeeded\n' "$name"
  done
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then register "$@"; fi
