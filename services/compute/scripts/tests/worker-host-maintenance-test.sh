#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/../../../.." && pwd)
script="$root/services/compute/scripts/worker-host-maintenance.sh"
bash -n "$script"
for arguments in   "plan unknown-host"   "pause venus"   "pause venus --confirm janus"   "resume venus"   "resume venus --confirm mercury"; do
  read -r -a argv <<< "$arguments"
  if bash "$script" "${argv[@]}" >/dev/null 2>&1; then
    printf 'Host maintenance accepted invalid invocation: %s\n' "$arguments" >&2
    exit 1
  fi
done
printf '%s\n' 'Host maintenance argument and syntax checks passed.'
