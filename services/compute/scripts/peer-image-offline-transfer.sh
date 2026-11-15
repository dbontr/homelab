#!/usr/bin/env bash
# Bounded, digest-fenced OCI fallback from an enrolled complete peer.
# Run on Saturn only. This never deletes images or restarts K3s.
set -euo pipefail
umask 077

usage() {
  printf 'Usage: %s --plan SOURCE DEST IMAGE | --apply SOURCE DEST IMAGE --confirm IMAGE\n' "$0" >&2
  exit 2
}

mode=${1:-}
source_host=${2:-}
destination_host=${3:-}
reference=${4:-}
case "$mode" in
  --plan) (( $# == 4 )) || usage ;;
  --apply) (( $# == 6 )) && [[ $5 == --confirm && $6 == "$reference" ]] || usage ;;
  *) usage ;;
esac
case "$source_host" in
  jupiter) source_ip=10.77.30.229 ;;
  venus) source_ip=10.77.30.230 ;;
  mercury) source_ip=10.77.30.231 ;;
  janus) source_ip=10.77.30.232 ;;
  *) usage ;;
esac
case "$destination_host" in
  jupiter) destination_ip=10.77.30.229 ;;
  venus) destination_ip=10.77.30.230 ;;
  mercury) destination_ip=10.77.30.231 ;;
  janus) destination_ip=10.77.30.232 ;;
  *) usage ;;
esac
[[ "$source_host" != "$destination_host" ]] || usage
[[ "$reference" =~ ^images\.dbontr\.internal/([a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)+):([A-Za-z0-9_][A-Za-z0-9._-]*)$ ]] || usage

fail() { printf 'OCI transfer refused: %s\n' "$*" >&2; exit 1; }
[[ ${EUID:-$(id -u)} == 0 ]] || fail 'root is required for enrolled worker SSH'
for binary in ssh kubectl jq awk flock timeout; do
  command -v "$binary" >/dev/null 2>&1 || fail "missing required command: $binary"
done
key=/root/.ssh/saturn-homelab-ed25519
[[ -r "$key" ]] || fail 'dedicated worker SSH identity is unavailable'
helper=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/label-image-content.sh
[[ -r "$helper" ]] || fail 'the exact source-owned content verifier is unavailable'
ssh_opts=(-o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o ConnectTimeout=6 -i "$key")
remote() { timeout 30 ssh -n "${ssh_opts[@]}" "root@$1" "$2"; }

if [[ "$mode" == --apply ]]; then
  exec 9>/run/lock/homelab-peer-offline-transfer.lock
  flock -n 9 || fail 'another peer image transfer is in progress'
fi
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT

for entry in "$source_host:$source_ip" "$destination_host:$destination_ip"; do
  host=${entry%%:*}
  ip=${entry#*:}
  ready=$(kubectl get node "$host" -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}') || fail "cannot read node readiness: $host"
  [[ "$ready" == True ]] || fail "worker is not Ready: $host"
  remote "$ip" 'systemctl is-active --quiet k3s.service' || fail "worker K3s is not active: $host"
done
remote "$source_ip" 'k3s ctr -n k8s.io images list' > "$scratch/source"
remote "$destination_ip" 'k3s ctr -n k8s.io images list' > "$scratch/destination"

image_digest() {
  local filename=$1
  mapfile -t matches < <(awk -v image="$reference" '$1 == image {print $3}' "$filename")
  (( ${#matches[@]} <= 1 )) || fail 'ambiguous image reference'
  if (( ${#matches[@]} == 0 )); then return 0; fi
  [[ ${matches[0]} =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'invalid image digest'
  printf '%s' "${matches[0]}"
}
source_digest=$(image_digest "$scratch/source")
[[ -n "$source_digest" ]] || fail 'source image tag is absent'
destination_digest=$(image_digest "$scratch/destination")
if [[ -n "$destination_digest" && "$destination_digest" != "$source_digest" ]]; then
  fail 'destination image tag resolves to a different digest; never overwrite an existing tag'
fi
remote "$source_ip" 'k3s ctr -n k8s.io images check' > "$scratch/source-check"
awk -v image="$reference" '
  $1 == image && $0 ~ /[[:space:]]complete[[:space:]]*\([0-9]+\/[0-9]+\)/ {passed=1}
  END {exit !passed}
' "$scratch/source-check" || fail 'source image content is not complete'

# Protect already running and initialized Pods from a tag re-import.
kubectl get pods -A -o json > "$scratch/pods"
jq -e --arg image "$reference" '
  all(.items[];
    all(.spec.containers[]?; .image != $image) and
    all(.spec.initContainers[]?; .image != $image) and
    all(.spec.ephemeralContainers[]?; .image != $image)
  )
' "$scratch/pods" >/dev/null || fail 'a Pod currently references the selected image tag'

printf 'SOURCE %s\nDESTINATION %s\nIMAGE %s\nDIGEST %s\n' \
  "$source_host" "$destination_host" "$reference" "$source_digest"
if [[ "$mode" == --plan ]]; then
  printf 'PLAN_ONLY: the source is complete and no conflicting destination tag or live Pod exists.\n'
  exit 0
fi

# A failed import may leave content in the destination. Do not garbage collect
# automatically; a retry is safe only after the complete preflight runs again.
printf 'STREAM_START: bounded OCI transfer without an intermediate archive\n'
if ! (
  set -o pipefail
  timeout 600 ssh -n "${ssh_opts[@]}" "root@$source_ip" \
    "k3s ctr -n k8s.io images export - '$reference'" |
    timeout 600 ssh "${ssh_opts[@]}" "root@$destination_ip" \
      'k3s ctr -n k8s.io images import -'
); then
  fail 'the bounded transfer failed; retain partial content for inspection'
fi
remote "$destination_ip" 'k3s ctr -n k8s.io images list' > "$scratch/final-images"
final_digest=$(image_digest "$scratch/final-images")
[[ "$final_digest" == "$source_digest" ]] || fail 'import changed or failed to establish the expected image digest'
remote "$destination_ip" 'k3s ctr -n k8s.io images check' > "$scratch/final-check"
awk -v image="$reference" '
  $1 == image && $0 ~ /[[:space:]]complete[[:space:]]*\([0-9]+\/[0-9]+\)/ {passed=1}
  END {exit !passed}
' "$scratch/final-check" || fail 'imported image is not complete'

# The helper hashes the index, manifest, config and every compressed blob
# before repairing any missing repository-source label. It does not restart K3s.
timeout 600 ssh "${ssh_opts[@]}" "root@$destination_ip" \
  "bash -s -- --apply '$reference' --confirm '$reference'" < "$helper" ||
    fail 'imported content did not pass SHA-256 and source metadata verification'
timeout 120 ssh "${ssh_opts[@]}" "root@$destination_ip" \
  "bash -s -- --check '$reference'" < "$helper" ||
    fail 'final content source verification failed'
printf 'TRANSFER_VERIFIED %s %s %s\n' "$destination_host" "$reference" "$source_digest"
printf 'Publisher/consumer Spegel indexes are not restarted by this fallback.\n'
