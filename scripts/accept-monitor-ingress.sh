#!/usr/bin/env bash
set -euo pipefail

# Read-only acceptance for the Access-protected Grafana route. A Netscape
# cookie jar or curl cookie file carries the owner's session without exposing
# it in command arguments, logs, or repository files.
export LC_ALL=C
if [[ $# -ne 1 || "$1" != /* || ! -f "$1" || -L "$1" ]]; then
  printf '%s\n' 'Usage: accept-monitor-ingress.sh ABSOLUTE_OWNER_COOKIE_FILE' >&2
  exit 2
fi
cookie_file=$1
for executable in curl stat jq mktemp; do
  command -v "$executable" >/dev/null || {
    printf 'Missing monitoring acceptance tool: %s\n' "$executable" >&2
    exit 2
  }
done
permissions=$(stat -c %a -- "$cookie_file")
owner=$(stat -c %u -- "$cookie_file")
if [[ ! $permissions =~ ^[0-7]{3,4}$ ]] ||
   (( (8#$permissions & 077) != 0 )) ||
   [[ "$owner" != "$(id -u)" || ! -s "$cookie_file" ]]; then
  printf '%s\n' 'Owner session cookie file must be nonempty and owner-only' >&2
  exit 2
fi

umask 077
response=$(mktemp)
trap 'rm -f -- "$response"' EXIT
url=https://monitor.dbontr.me/api/health
probe() {
  curl -q --silent --show-error --connect-timeout 3 --max-time 15 \
    --proto '=https' --max-redirs 0 --output "$response" \
    --write-out '%{http_code}' "$@" --url "$url"
}

# A successful anonymous response means that Access is not enforcing the route.
if ! anonymous=$(probe); then
  printf '%s\n' 'Anonymous monitoring request failed to complete' >&2
  exit 1
fi
case "$anonymous" in
  302|303|307|308|401|403) ;;
  *) printf 'Monitoring Access boundary failed: anonymous HTTP %s\n' "$anonymous" >&2; exit 1 ;;
esac

# Do not follow redirects: a login page is not evidence of Grafana readiness.
if ! authenticated=$(probe --cookie "$cookie_file"); then
  printf '%s\n' 'Authenticated monitoring request failed to complete' >&2
  exit 1
fi
if [[ "$authenticated" != 200 ]] || ! jq -e '.database == "ok"' "$response" >/dev/null 2>&1; then
  printf 'Authenticated monitoring health failed: HTTP %s or invalid Grafana health\n' "$authenticated" >&2
  exit 1
fi
printf '%s\n' 'Monitoring ingress accepted: anonymous access denied; authenticated Grafana health OK'
