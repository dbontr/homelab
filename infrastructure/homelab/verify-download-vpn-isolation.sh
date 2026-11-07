#!/usr/bin/env bash
# Read-only acceptance checks for the confined download service mount namespaces.
set -euo pipefail

ns=download-vpn
required=(qbittorrent prowlarr radarr sonarr lidarr saturn-download-dns)
expected_ns="$(stat -Lc '%i' "/run/netns/${ns}")"
if [[ -z "$expected_ns" ]]; then
  echo "FAIL: missing VPN network namespace" >&2
  exit 1
fi

check_service() {
  local unit="$1" pid service_ns resolver hosts
  pid="$(systemctl show --property=MainPID --value "${unit}.service")"
  if ! [[ "$pid" =~ ^[1-9][0-9]*$ ]]; then
    echo "FAIL: ${unit} has no running main process" >&2
    return 1
  fi
  service_ns="$(stat -Lc '%i' "/proc/${pid}/ns/net")"
  if [[ "$service_ns" != "$expected_ns" ]]; then
    echo "FAIL: ${unit} is outside ${ns}" >&2
    return 1
  fi

  if [[ "$unit" == "saturn-download-dns" ]]; then
    echo "PASS: ${unit} network namespace"
    return 0
  fi

  resolver="$(nsenter --target "$pid" --mount -- cat /etc/resolv.conf)"
  hosts="$(nsenter --target "$pid" --mount -- sed -n '/^[[:space:]]*hosts:/p' /etc/nsswitch.conf)"
  if ! grep -qx 'nameserver 127.0.0.53' <<<"$resolver"; then
    echo "FAIL: ${unit} does not use the confined resolver" >&2
    return 1
  fi
  if ! grep -Eq '^[[:space:]]*hosts:[[:space:]]*files[[:space:]]+dns[[:space:]]*$' <<<"$hosts"; then
    echo "FAIL: ${unit} uses a non-confined host lookup mechanism" >&2
    return 1
  fi
  if ! nsenter --target "$pid" --mount -- test ! -S /run/nscd/socket; then
    echo "FAIL: ${unit} can access host nscd" >&2
    return 1
  fi
  if ! nsenter --target "$pid" --mount -- test ! -S /run/systemd/resolve/io.systemd.Resolve; then
    echo "FAIL: ${unit} can access host systemd-resolved" >&2
    return 1
  fi
  echo "PASS: ${unit} network, DNS, NSS and resolver sockets"
}

for unit in "${required[@]}"; do
  check_service "$unit"
done
if systemctl is-active --quiet readarr.service; then
  check_service readarr
fi

if ! ip -n "$ns" -4 route show default | grep -Eq '^default dev wg-download( |$)'; then
  echo "FAIL: VPN namespace lacks the WireGuard-only IPv4 default route" >&2
  exit 1
fi
if [[ -n "$(ip -n "$ns" -6 route show default)" ]]; then
  echo "FAIL: VPN namespace has an IPv6 default route" >&2
  exit 1
fi
echo "PASS: VPN namespace default routes"
echo "NOTE: packet-capture DNS-leak, tunnel-loss and startup tests are separate acceptance gates"
