#!/usr/bin/env bash
set -euo pipefail

# Read-only acceptance of K3s embedded-registry peer advertisements.
usage() {
  printf '%s\n' 'Usage: verify-registry-mirror.sh [--require-hit] NODE [NODE ...]' >&2
  exit 2
}

require_hit=false
if [[ ${1:-} == --require-hit ]]; then
  require_hit=true
  shift
fi
(( $# > 0 )) || usage

if [[ -n ${KUBECTL_BIN:-} ]]; then
  kubectl=("$KUBECTL_BIN")
elif command -v kubectl >/dev/null 2>&1; then
  kubectl=(kubectl)
elif command -v k3s >/dev/null 2>&1; then
  kubectl=(k3s kubectl)
else
  printf '%s\n' 'kubectl or k3s is required.' >&2
  exit 2
fi
command -v "${kubectl[0]}" >/dev/null 2>&1 || usage
command -v awk >/dev/null 2>&1 || usage
command -v jq >/dev/null 2>&1 || usage

registry=images.dbontr.internal
declare -A seen=()
declare -a receipts=()
total_hits=0

for node in "$@"; do
  if [[ ! "$node" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ||
        ${#node} -gt 63 || -v seen[$node] ]]; then
    printf 'Invalid or repeated node: %s\n' "$node" >&2
    exit 2
  fi
  seen[$node]=1
  if ! ready=$("${kubectl[@]}" get node "$node" \
      -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}'); then
    printf 'Cannot inspect node: %s\n' "$node" >&2
    exit 1
  fi
  if [[ "$ready" != True ]]; then
    printf 'Node is not Ready: %s\n' "$node" >&2
    exit 1
  fi
  if ! metrics=$("${kubectl[@]}" get --raw "/api/v1/nodes/$node/proxy/metrics"); then
    printf 'Cannot read kubelet metrics: %s\n' "$node" >&2
    exit 1
  fi
  read -r advertised hits misses < <(printf '%s\n' "$metrics" | awk -v registry="$registry" '
    index($0, "registry=\"" registry "\"") {
      if (index($0, "spegel_advertised_image_digests{") == 1)
        advertised += $NF
      if (index($0, "spegel_mirror_requests_total{") == 1) {
        if (index($0, "cache=\"hit\"")) hits += $NF
        if (index($0, "cache=\"miss\"")) misses += $NF
      }
    }
    END { printf "%.0f %.0f %.0f\n", advertised, hits, misses }
  ')
  if [[ ! "$advertised" =~ ^[0-9]+$ ]] || (( advertised == 0 )); then
    printf 'No Spegel image digests advertised on %s for %s.\n' "$node" "$registry" >&2
    exit 1
  fi
  total_hits=$((total_hits + hits))
  receipts+=("$(jq -nc --arg node "$node" --arg registry "$registry" \
    --argjson advertised "$advertised" --argjson hits "$hits" \
    --argjson misses "$misses" \
    '{node:$node,registry:$registry,advertised_image_digests:$advertised,mirror_cache_hits:$hits,mirror_cache_misses:$misses,result:"passed"}')")
done

if [[ "$require_hit" == true ]] && (( total_hits == 0 )); then
  printf 'No peer-mirror cache hit has been observed on the selected nodes.\n' >&2
  exit 1
fi

printf '%s\n' "${receipts[@]}"
