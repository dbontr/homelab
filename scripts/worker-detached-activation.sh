#!/usr/bin/env bash
# Run a reversible NixOS compute-worker trial independently of the SSH session.
# The boot profile is never modified by this script.
set -euo pipefail
umask 077
export PATH="/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin:$PATH"

fail() {
  printf 'Compute worker trial: %s\n' "$*" >&2
  exit 1
}

valid_host() {
  case "$1" in jupiter|venus|mercury|janus) return 0 ;; *) return 1 ;; esac
}

valid_system() {
  local path=$1 host=$2 resolved
  resolved=$(readlink -f -- "$path") || return 1
  [[ "$path" == "$resolved" &&
     "$path" =~ ^/nix/store/[a-z0-9]{32}-nixos-system-${host}-[a-zA-Z0-9._-]+$ &&
     -x "$path/bin/switch-to-configuration" ]]
}

contains_mirrors() {
  local file=$1
  [[ -f "$file" ]] &&
    grep -Eq '^[[:space:]]*"images\.dbontr\.internal":[[:space:]]*$' "$file" &&
    grep -Eq '^[[:space:]]*"ghcr\.io":[[:space:]]*$' "$file"
}

require_active() {
  local unit=$1 attempt
  for attempt in $(seq 1 30); do
    if systemctl is-active --quiet "$unit"; then
      printf 'READY %s\n' "$unit"
      return 0
    fi
    sleep 2
  done
  printf 'NOT READY %s\n' "$unit" >&2
  return 1
}

if [[ "$#" -eq 4 && "$1" == --run ]]; then
  candidate=$2 previous=$3 host=$4
  valid_host "$host" || fail 'unexpected host'
  [[ "$(hostname -s)" == "$host" ]] || fail 'host identity changed'
  valid_system "$candidate" "$host" || fail 'candidate is invalid'
  valid_system "$previous" "$host" || fail 'rollback closure is invalid'
  exec 9>/run/lock/homelab-worker-cutover.lock
  flock -n 9 || fail 'another worker trial holds the lock'

  printf 'HOST %s\nCANDIDATE %s\nPREVIOUS %s\n' "$host" "$candidate" "$previous"
  status=0
  timeout 420 "$candidate/bin/switch-to-configuration" test || status=1
  if [[ "$status" -eq 0 ]]; then
    require_active k3s.service || status=1
    require_active sshd.service || status=1
    contains_mirrors /etc/rancher/k3s/registries.yaml || {
      printf 'Installed registry mirrors do not match the candidate\n' >&2
      status=1
    }
  fi

  if [[ "$status" -ne 0 ]]; then
    printf 'ROLLBACK %s\n' "$previous" >&2
    timeout 420 "$previous/bin/switch-to-configuration" test ||
      fail 'automatic rollback failed; recover through independent console access'
    require_active k3s.service || fail 'K3s did not recover after rollback'
    require_active sshd.service || fail 'SSH did not recover after rollback'
    fail 'trial failed; previous configuration restored'
  fi
  printf 'TRIAL PASSED. Boot profile unchanged. Verify Kubernetes and leases externally before promotion.\n'
  exit 0
fi

[[ "$#" -eq 2 && "$1" == --maintenance-approved ]] ||
  fail 'usage: worker-detached-activation.sh --maintenance-approved /nix/store/...-nixos-system-HOST-...'
[[ "$EUID" -eq 0 ]] || fail 'root is required'
host=$(hostname -s)
valid_host "$host" || fail 'only registered compute workers may be activated'
candidate=$(readlink -f -- "$2")
previous=$(readlink -f -- /run/current-system)
boot_profile=$(readlink -f -- /nix/var/nix/profiles/system)
valid_system "$candidate" "$host" || fail 'candidate is not a valid immutable host closure'
valid_system "$previous" "$host" || fail 'current closure is invalid'
[[ "$previous" == "$boot_profile" ]] || fail 'live system differs from selected boot generation'
[[ "$candidate" != "$previous" ]] || fail 'candidate is already running'
contains_mirrors "$candidate/etc/rancher/k3s/registries.yaml" ||
  fail 'candidate is missing one or both required registry mirrors'
systemctl is-active --quiet k3s.service || fail 'K3s is not active before trial'
systemctl is-active --quiet sshd.service || fail 'SSH is not active before trial'

# Maintenance clearance includes workload/lease inspection at the controller.
# This host-local script cannot establish that external condition.
unit="homelab-worker-trial-$(date +%s%N)"
runner="/run/$unit-runner"
bash_bin=$(readlink -f -- "$(command -v bash)") || fail 'Bash runtime is missing'
install -m 0700 -- "$0" "$runner"
systemd-run --no-block --unit="$unit" --collect \
  --property=Type=oneshot --property=TimeoutStartSec=20min \
  --property=StandardOutput=journal --property=StandardError=journal \
  "$bash_bin" "$runner" --run "$candidate" "$previous" "$host"
printf 'Detached trial: %s\nInspect: journalctl -u %s --no-pager\n' "$unit" "$unit"
printf 'Rollback closure: %s\n' "$previous"
