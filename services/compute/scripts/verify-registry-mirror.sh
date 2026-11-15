#!/usr/bin/env bash
set -euo pipefail

# Read-only acceptance of K3s embedded-registry peer advertisements.
usage() {
  printf '%s\n' 'Usage: verify-registry-mirror.sh [--require-hit | --require-hit-since BASELINE] NODE [NODE ...]' >&2
  exit 2
}

require_hit=false
baseline=
if [[ ${1:-} == --require-hit ]]; then
  require_hit=true
  shift
elif [[ ${1:-} == --require-hit-since ]]; then
  (( $# >= 3 )) || usage
  baseline=$2
  shift 2
  [[ -f $baseline && -r $baseline ]] || {
    printf 'Baseline receipt is unreadable: %s\n' "$baseline" >&2
    exit 2
  }
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
total_new_hits=0

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
  if [[ ! $advertised =~ ^[0-9]+$ || ! $hits =~ ^[0-9]+$ ||
        ! $misses =~ ^[0-9]+$ ]] || (( advertised == 0 )); then
    printf 'Missing or malformed Spegel metrics on %s for %s.\n' "$node" "$registry" >&2
    exit 1
  fi
  total_hits=$((total_hits + hits))
  new_hits=0
  if [[ -n $baseline ]]; then
    if ! prior=$(jq -ers --arg node "$node" --arg registry "$registry" '
      [.[] | select(type == "object" and .node == $node and .registry == $registry)]
      | if length == 1 then .[0].mirror_cache_hits
        else error("missing or duplicate baseline node") end
      | select(type == "number" and . >= 0 and . == floor)
    ' "$baseline"); then
      printf 'Invalid baseline receipt for %s.\n' "$node" >&2
      exit 1
    fi
    if (( hits < prior )); then
      printf 'Spegel hit counter decreased on %s; capture a new baseline.\n' "$node" >&2
      exit 1
    fi
    new_hits=$((hits - prior))
    total_new_hits=$((total_new_hits + new_hits))
  fi
  receipts+=("$(jq -nc --arg node "$node" --arg registry "$registry" \
    --argjson advertised "$advertised" --argjson hits "$hits" \
    --argjson misses "$misses" --argjson new_hits "$new_hits" \
    --argjson with_baseline "$([[ -n $baseline ]] && echo true || echo false)" \
    '{node:$node,registry:$registry,advertised_image_digests:$advertised,
      mirror_cache_hits:$hits,mirror_cache_misses:$misses,result:"passed"}
      + (if $with_baseline then {new_mirror_cache_hits:$new_hits} else {} end)')")
done

if [[ "$require_hit" == true ]] && (( total_hits == 0 )); then
  printf 'No peer-mirror cache hit has been observed on the selected nodes.\n' >&2
  exit 1
fi
if [[ -n $baseline ]] && (( total_new_hits == 0 )); then
  printf 'No new peer-mirror cache hit was observed since the baseline.\n' >&2
  exit 1
fi

printf '%s\n' "${receipts[@]}"
