#!/usr/bin/env bash
# Activate a candidate NixOS configuration outside the attached KVM/getty
# session. This is a reversible trial, not a boot-generation change.
set -euo pipefail
umask 077
export PATH="/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin:$PATH"

fail() {
  printf 'Saturn activation: %s\n' "$*" >&2
  exit 1
}

valid_system() {
  local path="$1" actual
  actual=$(readlink -f -- "$path") || return 1
  [[ "$path" == "$actual" &&
     "$path" =~ ^/nix/store/[a-z0-9]{32}-nixos-system-saturn-[a-zA-Z0-9._-]+$ &&
     -x "$path/bin/switch-to-configuration" ]]
}

require_active() {
  local unit="$1" attempt
  for attempt in $(seq 1 15); do
    if systemctl is-active --quiet "$unit"; then
      printf 'READY %s\n' "$unit"
      return 0
    fi
    sleep 2
  done
  printf 'NOT READY %s\n' "$unit" >&2
  return 1
}

if [[ "$#" -eq 3 && "$1" == "--run" ]]; then
  candidate="$2"
  previous="$3"
  valid_system "$candidate" || fail "invalid candidate NixOS store path"
  valid_system "$previous" || fail "invalid previous NixOS store path"
  exec 9>/run/lock/homelab-cutover.lock
  flock -n 9 || fail "another Saturn cutover is already running"

  printf 'Candidate: %s\nPrevious: %s\n' "$candidate" "$previous"
  status=0
  if ! timeout 420 "$candidate/bin/switch-to-configuration" test; then
    printf 'Candidate test activation failed\n' >&2
    status=1
  fi

  if [[ "$status" -eq 0 ]]; then
    for unit in k3s.service cloudflared-saturn.service nginx.service saturn-media.service \
                adaptive-teaching.service personal-health-mcp.service \
                homelab-platform.service homelab-mcp.service homelab-api.service \
                homelab-motivation.service homelab-securityd.service \
                homelab-executord.service; do
      require_active "$unit" || status=1
    done
    security_dir=/run/homelab-securityd
    if [[ ! -S "$security_dir/owner.sock" ||
          ! -S "$security_dir/client.sock" ||
          "$(stat -c '%a' "$security_dir/owner.sock")" != 600 ||
          "$(stat -c '%a' "$security_dir/client.sock")" != 660 ]]; then
      printf 'Security socket type or permissions are invalid\n' >&2
      status=1
    else
      owner_status=$(curl --silent --show-error --max-time 5 \
        --unix-socket "$security_dir/owner.sock" \
        --output /dev/null --write-out '%{http_code}' \
        http://unix/v1/session) || owner_status=""
      client_status=$(curl --silent --show-error --max-time 5 \
        --unix-socket "$security_dir/client.sock" \
        --output /dev/null --write-out '%{http_code}' \
        http://unix/v1/session) || client_status=""
      if [[ "$owner_status" != 200 || "$client_status" != 403 ]]; then
        printf 'Security socket health or unsigned-client rejection failed\n' >&2
        status=1
      fi
    fi
    executor_dir=/run/homelab-executord
    if [[ ! -S "$executor_dir/execute.sock" ||
          "$(stat -c '%a' "$executor_dir/execute.sock")" != 660 ]]; then
      printf 'Executor socket type or permissions are invalid\n' >&2
      status=1
    else
      executor_status=$(curl --silent --show-error --max-time 5 \
        --unix-socket "$executor_dir/execute.sock" \
        --header 'Content-Type: application/json' \
        --data '{"request_id":"00000000000000000000000000000000","operation":"infrastructure__service_restart","target":"saturn","arguments":{"host":"saturn","service":"homelab-motivation.service"}}' \
        --output /dev/null --write-out '%{http_code}' \
        http://unix/v1/execute) || executor_status=""
      if [[ "$executor_status" != 403 ]]; then
        printf 'Unsigned executor request was not rejected\n' >&2
        status=1
      fi
    fi
    for port in 8300 8401 8402; do
      if ! curl --fail --silent --show-error --max-time 5 \
        "http://127.0.0.1:$port/healthz" >/dev/null; then
        printf 'HTTP health check failed on loopback port %s\n' "$port" >&2
        status=1
      fi
    done
  fi

  if [[ "$status" -ne 0 ]]; then
    printf 'ROLLBACK to %s\n' "$previous" >&2
    rollback_status=0
    timeout 420 "$previous/bin/switch-to-configuration" test || rollback_status=1
    previous_go=$(readlink -f -- /tmp/saturn-security-system 2>/dev/null || :)
    if [[ "$previous" == "$previous_go" &&
          -f /var/lib/homelab-securityd/.authority-selection.json ]] &&
          grep -q '"target":"rust"' /var/lib/homelab-securityd/.authority-selection.json; then
      if [[ "$rollback_status" -ne 0 ]] ||
          ! systemctl is-active --quiet homelab-securityd; then
        printf 'Rust selection is committed; generating a Go recovery receipt.\n' >&2
        bash_bin=$(readlink -f -- "$(command -v bash)") || fail "Bash runtime missing"
        timeout 420 "$bash_bin" \
          /srv/homelab-migration-src/scripts/saturn-security-recover-go.sh ||
          fail "receipt-backed Go recovery failed; inspect through NanoKVM"
        rollback_status=0
      fi
    fi
    [[ "$rollback_status" -eq 0 ]] ||
      fail "automatic rollback failed; inspect the system through NanoKVM"
    fail "trial failed and the previous NixOS configuration was restored"
  fi

  printf 'TRIAL PASSED. Boot generation was not changed.\n'
  exit 0
fi

[[ "$#" -eq 1 ]] || fail "usage: saturn-detached-activation.sh /nix/store/...-nixos-system-saturn-..."
[[ "$EUID" -eq 0 ]] || fail "this command requires root"
candidate=$(readlink -f -- "$1")
previous=$(readlink -f -- /run/current-system)
valid_system "$candidate" || fail "candidate must be an immutable Saturn NixOS system closure"
valid_system "$previous" || fail "current NixOS system is not a valid rollback target"
[[ "$candidate" != "$previous" ]] || fail "candidate is already the current system"

unit="homelab-cutover-$(date +%s)"
runner="/run/$unit-runner"
bash_bin=$(readlink -f -- "$(command -v bash)") || fail "cannot resolve the Bash runtime"
[[ -x "$bash_bin" ]] || fail "Bash runtime is unavailable"
install -m 0700 -- "$0" "$runner"
printf '%s\n' "$previous" > /run/homelab-previous-system
systemd-run --no-block --unit="$unit" --collect \
  --property=Type=oneshot --property=TimeoutStartSec=25min \
  --property=StandardOutput=journal --property=StandardError=journal \
  "$bash_bin" "$runner" --run "$candidate" "$previous"
printf 'Detached trial started: %s\n' "$unit"
printf 'Inspect with: journalctl -u %s --no-pager\n' "$unit"
printf 'Previous system: %s\n' "$previous"
