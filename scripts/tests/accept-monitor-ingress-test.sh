#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir "$work/bin"
cat > "$work/bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
output=''
authorized=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) output=$2; shift 2 ;;
    --cookie) authorized=true; shift 2 ;;
    *) shift ;;
  esac
done
[[ -n "$output" ]] || exit 4
if [[ "$authorized" == true ]]; then
  if [[ -n ${MOCK_GRAFANA_RESPONSE+x} ]]; then
    printf '%s\n' "$MOCK_GRAFANA_RESPONSE" > "$output"
  else
    printf '%s\n' '{"database":"ok"}' > "$output"
  fi
  printf '%s' "${MOCK_AUTH_CODE:-200}"
else
  printf '%s\n' 'access denied' > "$output"
  printf '%s' "${MOCK_ANON_CODE:-403}"
fi
MOCK
chmod 0755 "$work/bin/curl"
cookie="$work/owner-cookie"
printf '%s\n' 'CF_Authorization=fake-fixture-value' > "$cookie"
chmod 0600 "$cookie"
export PATH="$work/bin:$PATH"
bash "$root/accept-monitor-ingress.sh" "$cookie" > "$work/out"
grep -q 'Monitoring ingress accepted' "$work/out"

if MOCK_ANON_CODE=200 bash "$root/accept-monitor-ingress.sh" "$cookie" > "$work/out" 2>&1; then
  printf '%s\n' 'Public monitoring route was accepted' >&2
  exit 1
fi
if MOCK_AUTH_CODE=302 bash "$root/accept-monitor-ingress.sh" "$cookie" > "$work/out" 2>&1; then
  printf '%s\n' 'Unauthenticated redirect was accepted as Grafana health' >&2
  exit 1
fi
if MOCK_GRAFANA_RESPONSE='{"database":"failed"}' bash "$root/accept-monitor-ingress.sh" "$cookie" > "$work/out" 2>&1; then
  printf '%s\n' 'Unhealthy Grafana JSON was accepted' >&2
  exit 1
fi
chmod 0644 "$cookie"
if bash "$root/accept-monitor-ingress.sh" "$cookie" > "$work/out" 2>&1; then
  printf '%s\n' 'World-readable owner cookie was accepted' >&2
  exit 1
fi
printf '%s\n' 'Monitoring-route acceptance negative and positive tests passed'
