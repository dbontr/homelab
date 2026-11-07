#!/usr/bin/env bash
# Shared request path for private Cloudflare administration scripts.

cf_fail() { printf 'Cloudflare operation failed: %s\n' "$*" >&2; exit 1; }

cf_initialize() {
  CF_ACCOUNT=${CLOUDFLARE_ACCOUNT_ID:-22d29ce5a32660fa8cfc99f1b7ac79e1}
  CF_TOKEN_FILE=${CLOUDFLARE_TOKEN_FILE:-/srv/homelab/secrets/cloudflare-api-token}
  CF_API=https://api.cloudflare.com/client/v4
  [[ -f $CF_TOKEN_FILE && ! -L $CF_TOKEN_FILE ]] ||
    cf_fail 'Private API token file is unavailable'
  CF_TOKEN=$(<"$CF_TOKEN_FILE")
  [[ $CF_TOKEN =~ ^[A-Za-z0-9_~.-]+$ ]] || cf_fail 'Malformed Cloudflare API token'
  for tool in jq curl mktemp; do
    command -v "$tool" >/dev/null || cf_fail "Missing Cloudflare tool: $tool"
  done
}

cf_api() {
  local method=$1 path=$2 payload=${3:-} body_file='' response
  [[ $path == /* ]] || cf_fail 'API path must be absolute'
  if [[ $# -ge 3 ]]; then
    body_file=$(mktemp)
    chmod 0600 "$body_file"
    printf '%s' "$payload" > "$body_file"
  fi
  # The credential is supplied through a private curl configuration stream,
  # not through process arguments, logs, or the repository.
  if [[ -n $body_file ]]; then
    if ! response=$(curl --config <(printf 'header = "Authorization: Bearer %s"\n' "$CF_TOKEN") \
      --silent --show-error --fail --max-time 25 --noproxy '*' \
      --header 'Accept: application/json' --header 'Content-Type: application/json' \
      --request "$method" --data-binary @"$body_file" "$CF_API$path"); then
      rm -f -- "$body_file"
      cf_fail "$method ${path%%\?*} failed"
    fi
    rm -f -- "$body_file"
  else
    response=$(curl --config <(printf 'header = "Authorization: Bearer %s"\n' "$CF_TOKEN") \
      --silent --show-error --fail --max-time 25 --noproxy '*' \
      --header 'Accept: application/json' --request "$method" "$CF_API$path") ||
      cf_fail "$method ${path%%\?*} failed"
  fi
  jq -ce 'if .success == true then .result else error("Cloudflare API returned an error") end' \
    <<<"$response" || cf_fail "$method ${path%%\?*} did not succeed"
}
