#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
base=${1:-}
if [[ ! "$base" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'Supply the exact Git baseline revision.' >&2
  exit 2
fi
for tool in git helm tar diff; do
  command -v "$tool" >/dev/null || { printf 'Missing %s\n' "$tool" >&2; exit 2; }
done
if ! git -C "$root" cat-file -e "$base^{commit}" ||
   ! git -C "$root" merge-base --is-ancestor "$base" HEAD; then
  echo 'The baseline must be available and an ancestor of the tested revision.' >&2
  exit 2
fi
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
chart_rel=services/compute/deploy/helm/project-workspace
git -C "$root" archive "$base" "$chart_rel" | tar -xf - -C "$scratch"
baseline="$scratch/$chart_rel"
candidate="$root/$chart_rel"
controller="ghcr.io/dbontr/homelab/controller@sha256:$(printf '%064d' 0)"
helm template pwm-controller-compat "$baseline" \
  --set controller.enabled=true --set-string "controller.image=$controller" \
  > "$scratch/controller-baseline.yaml"
helm template pwm-controller-compat "$candidate" \
  --set controller.enabled=true --set-string "controller.image=$controller" \
  > "$scratch/controller-candidate.yaml"
diff -u "$scratch/controller-baseline.yaml" "$scratch/controller-candidate.yaml"
for host in jupiter venus mercury janus; do
  values="$root/infrastructure/homelab/workspace/worker-$host.yaml"
  helm template "pwm-worker-$host" "$baseline" -f "$values" > "$scratch/$host-baseline.yaml"
  helm template "pwm-worker-$host" "$candidate" -f "$values" > "$scratch/$host-candidate.yaml"
  diff -u "$scratch/$host-baseline.yaml" "$scratch/$host-candidate.yaml"
done
printf '%s\n' 'Default controller and four worker manifests preserve their baseline contracts.'
