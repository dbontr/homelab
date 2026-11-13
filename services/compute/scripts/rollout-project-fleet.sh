#!/usr/bin/env bash
set -euo pipefail
set +x
umask 077

usage() {
  printf 'Usage: %s plan IMAGE@sha256:DIGEST | apply IMAGE@sha256:DIGEST --confirm-count NUMBER\n' "$0" >&2
  exit 2
}
(( $# == 2 || $# == 4 )) || usage
mode=$1
image=$2
[[ "$mode" == plan || "$mode" == apply ]] || usage
[[ "$image" =~ ^ghcr\.io/dbontr/homelab/worker@sha256:[a-f0-9]{64}$ ]] ||
  { echo 'The approved worker registry and immutable image digest are required.' >&2; exit 2; }
if [[ "$mode" == apply ]]; then
  [[ $# == 4 && "$3" == --confirm-count && "$4" =~ ^[1-9][0-9]*$ ]] || usage
  confirmed_count=$4
else
  (( $# == 2 )) || usage
fi
: "\${PWM_HELM_CHART:?Set PWM_HELM_CHART to the source-owned Compute chart directory}"
system_namespace="\${PWM_SYSTEM_NAMESPACE:-pwm-system}"
[[ "$system_namespace" == pwm-system ]] ||
  { echo 'Unexpected controller namespace.' >&2; exit 2; }
for command in helm kubectl jq bash cmp; do
  command -v "$command" >/dev/null ||
    { printf 'Required executable is unavailable: %s\n' "$command" >&2; exit 2; }
done
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
single="$root/rollout-project-worker.sh"
[[ -f "$single" ]] || { echo 'Single-worker rollout utility is unavailable.' >&2; exit 2; }
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
chmod 700 "$scratch"

inventory() {
  helm -n "$system_namespace" list --deployed --max 1024 -o json > "$scratch/helm.json"
  kubectl -n "$system_namespace" get deployments -l app=pwm-worker \
    -o json > "$scratch/deployments.json"

  # A project release must own exactly one worker Deployment. A missing or
  # unexpected Deployment makes the inventory incomplete and blocks the fleet.
  jq -e '
    type == "array" and all(.[] | select(.name | startswith("pwm-project-"));
      (.name | test("^pwm-project-[0-9a-f]{12}-(jupiter|venus|mercury|janus)$")) and
      .status == "deployed")
  ' "$scratch/helm.json" >/dev/null ||
    { echo 'Helm returned invalid or nondeployed project releases.' >&2; return 1; }
  jq -e '
    (.items | type) == "array" and
    all(.items[] | select(.metadata.annotations["meta.helm.sh/release-name"] | startswith("pwm-project-"));
      (.metadata.annotations["meta.helm.sh/release-name"] |
        test("^pwm-project-[0-9a-f]{12}-(jupiter|venus|mercury|janus)$")) and
      ([.spec.template.spec.containers[] | select(.name == "worker")] | length) == 1)
  ' "$scratch/deployments.json" >/dev/null ||
    { echo 'Kubernetes returned invalid project worker ownership.' >&2; return 1; }

  jq -nr --slurpfile helm "$scratch/helm.json" \
    --slurpfile pods "$scratch/deployments.json" '
    ($helm[0] | map(select(.name | startswith("pwm-project-")) | .name) | sort) as $releases |
    ($pods[0].items | map(select(.metadata.annotations["meta.helm.sh/release-name"] |
       startswith("pwm-project-")) | .metadata.annotations["meta.helm.sh/release-name"]) | sort) as $deployed |
    if $releases == $deployed then "matched" else error("Helm/Deployment ownership mismatch") end
  ' > "$scratch/ownership"

  jq -r --arg image "$image" '
    [.items[] |
      select(.metadata.annotations["meta.helm.sh/release-name"] | startswith("pwm-project-")) |
      {name:.metadata.annotations["meta.helm.sh/release-name"],
       current:([.spec.template.spec.containers[] | select(.name == "worker")][0].image)}] |
    sort_by(.name) | .[] | select(.current != $image) | .name
  ' "$scratch/deployments.json" > "$scratch/candidates"
}

inventory
cp "$scratch/candidates" "$scratch/initial-candidates"
count=$(wc -l < "$scratch/candidates")
printf 'FLEET: %s project-worker releases require the target image.\n' "$count"
if (( count == 0 )); then
  printf 'ACCEPTED: all deployed project workers already use %s.\n' "$image"
  exit 0
fi
if [[ "$mode" == apply && "$confirmed_count" != "$count" ]]; then
  printf 'REFUSED: expected %s releases, discovered %s.\n' "$confirmed_count" "$count" >&2
  exit 1
fi

# Every candidate must satisfy the single-worker admission gate before any
# live change. This also protects the admission test from partial discovery.
while IFS= read -r release; do
  [[ "$release" =~ ^pwm-project-[a-f0-9]{12}-(jupiter|venus|mercury|janus)$ ]] ||
    { echo 'Invalid project release in fleet inventory.' >&2; exit 1; }
  printf 'PREFLIGHT %s\n' "$release"
  bash "$single" plan "$release" "$image"
done < "$scratch/initial-candidates"

if [[ "$mode" == plan ]]; then
  printf 'PLAN ONLY: %s releases inspected; no live upgrades.\n' "$count"
  exit 0
fi

inventory
cmp -s "$scratch/initial-candidates" "$scratch/candidates" ||
  { echo 'REFUSED: the fleet image inventory changed after preflight.' >&2; exit 1; }

completed=0
while IFS= read -r release; do
  printf 'UPGRADING %s (%s of %s)\n' "$release" "$((completed+1))" "$count"
  # The release-level admission and revision fence are repeated immediately
  # before this mutation. No two Helm upgrades run concurrently.
  if ! bash "$single" apply "$release" "$image" --confirm "$release"; then
    printf 'FLEET STOPPED after %s accepted releases; retain Helm history and inspect %s.\n' \
      "$completed" "$release" >&2
    exit 1
  fi
  completed=$((completed+1))
done < "$scratch/initial-candidates"

printf 'ACCEPTED: %s project-worker Helm releases upgraded serially.\n' "$completed"
