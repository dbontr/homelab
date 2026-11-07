#!/usr/bin/env bash
# Read-only checks for Seerr's private Arr API bridge.
set -euo pipefail

gateway="$(docker network inspect saturn-media_default --format '{{(index .IPAM.Config 0).Gateway}}')"
seerr_ip="$(docker inspect saturn-seerr --format '{{(index .NetworkSettings.Networks "saturn-media_default").IPAddress}}')"
if [[ "$gateway" != "172.19.0.1" || "$seerr_ip" != "172.19.0.3" ]]; then
  echo "FAIL: private media-network addresses do not match declarations" >&2
  exit 1
fi

for entry in radarr:17878 sonarr:18989; do
  name="${entry%%:*}"
  port="${entry##*:}"
  systemctl is-active --quiet "saturn-seerr-${name}.socket" || {
    echo "FAIL: ${name} bridge socket is not active" >&2; exit 1;
  }
  if ! ss -lnt "( sport = :${port} )" | grep -q "172.19.0.1:${port}"; then
    echo "FAIL: ${name} listener is not bound to the private gateway" >&2
    exit 1
  fi
  output="$(docker exec saturn-seerr sh -c "wget -S -O /dev/null -T 5 http://172.19.0.1:${port}/api/v3/system/status" 2>&1 || true)"
  if ! grep -Eq 'HTTP/1\.[01] 401' <<<"$output"; then
    echo "FAIL: Seerr cannot reach the authenticated ${name} API" >&2
    exit 1
  fi
  echo "PASS: Seerr reaches the authenticated ${name} API through its private bridge"
done

if ! ip -n download-vpn route show default | grep -q 'default dev wg-download'; then
  echo "FAIL: downloader namespace lacks VPN default route" >&2
  exit 1
fi
if [[ -n "$(ip -n download-vpn -6 route show default)" ]]; then
  echo "FAIL: downloader namespace has IPv6 default route" >&2
  exit 1
fi
echo "PASS: downloader default route remains WireGuard-only"
echo "NOTE: request approval, indexer, download and media-import workflows require separate tests"
