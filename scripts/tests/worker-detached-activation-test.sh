#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
script="$root/scripts/worker-detached-activation.sh"
bash -n "$script"
if bash "$script" > /dev/null 2>&1; then
  echo 'Worker activation accepted missing explicit approval' >&2
  exit 1
fi
if bash "$script" --maintenance-approved /tmp/not-a-nixos-closure > /dev/null 2>&1; then
  echo 'Worker activation accepted a non-store closure' >&2
  exit 1
fi
if bash "$script" --run /tmp/none /tmp/none unknown-host > /dev/null 2>&1; then
  echo 'Worker activation accepted an unregistered host' >&2
  exit 1
fi
printf '%s\n' 'Compute-worker activation argument and syntax checks passed.'
