#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  echo 'Usage: verify-cold-peer-pull.sh DONOR RECEIVER IMAGE SHA256_DIGEST' >&2
  exit 2
}

(( $# == 4 )) || usage
donor=$1
receiver=$2
image=$3
digest=$4
case "$donor" in janus|jupiter|mercury|venus) ;; *) usage ;; esac
case "$receiver" in janus|jupiter|mercury|venus) ;; *) usage ;; esac
[[ "$donor" != "$receiver" ]] || usage
[[ $image =~ ^images[.]dbontr[.]internal/[a-z0-9._/-]+(:[a-z0-9._-]+|@sha256:[a-f0-9]{64})$ ]] || usage
[[ $digest =~ ^sha256:[a-f0-9]{64}$ ]] || usage
command -v jq >/dev/null || usage

remote() {
  local host=$1 command=$2
  ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o IdentitiesOnly=yes \
      -o ConnectTimeout=10 "$host" "$command"
}
get_digest() {
  remote "$1" 'k3s ctr -n k8s.io images ls' |
    awk -v ref="$image" '$1==ref {print $3; n++} END {if(n!=1)exit 1}'
}
complete() {
  remote "$1" 'k3s ctr -n k8s.io images check' |
    awk -v ref="$image" -v sha="$digest" \
      '$1==ref && $3==sha && $4=="complete" {n++} END {if(n!=1)exit 1}'
}
[[ $(get_digest "$donor") == "$digest" ]] && complete "$donor" || {
  echo 'Donor image digest or OCI content is incomplete' >&2; exit 1;
}
if remote "$receiver" 'k3s ctr -n k8s.io images ls -q' | grep -Fx -- "$image" >/dev/null; then
  echo 'Receiver already contains this image: not a cold-pull acceptance' >&2
  exit 1
fi
source_index=$(remote "$donor" "k3s ctr -n k8s.io content get '$digest'")
manifest_digest=$(jq -r '
  [.manifests[]? | select(.platform.os=="linux" and .platform.architecture=="amd64")]
  | if length==0 then "" else .[0].digest end
' <<< "$source_index")
if [[ -n "$manifest_digest" ]]; then
  [[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || {
    echo 'Invalid Linux amd64 manifest digest' >&2; exit 1;
  }
  manifest=$(remote "$donor" "k3s ctr -n k8s.io content get '$manifest_digest'")
else
  manifest=$source_index
fi
mapfile -t layers < <(jq -r '.layers[]?.digest // empty' <<< "$manifest")
(( ${#layers[@]} > 0 && ${#layers[@]} <= 128 )) || {
  echo 'No bounded Linux amd64 layer inventory' >&2; exit 1;
}
for layer in "${layers[@]}"; do
  [[ "$layer" =~ ^sha256:[a-f0-9]{64}$ ]] || {
    echo 'Invalid OCI layer descriptor' >&2; exit 1;
  }
done
received=$(remote "$receiver" 'k3s ctr -n k8s.io content ls -q')
missing=
for layer in "${layers[@]}"; do
  if ! grep -Fxq -- "$layer" <<< "$received"; then
    missing=$layer
    break
  fi
done
[[ -n "$missing" ]] || {
  echo 'All OCI layers are already cached; cold-layer qualification requires an uncached receiver' >&2
  exit 1
}
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
baseline=$(mktemp)
trap 'rm -f -- "$baseline"' EXIT
bash "$dir/verify-registry-mirror.sh" "$donor" "$receiver" > "$baseline"

echo 'Attempting cold peer pull (no image deletion, K3s restart or deployment mutation)'
timeout 240 ssh -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o IdentitiesOnly=yes -o ConnectTimeout=10 "$receiver" \
  "k3s crictl pull '$image'" || {
  echo 'Cold CRI pull failed; use digest-verified peer recovery, not a success receipt' >&2
  exit 1
}
[[ $(get_digest "$receiver") == "$digest" ]] && complete "$receiver" || {
  echo 'Receiver image did not match complete donor content' >&2; exit 1;
}
if ! remote "$receiver" 'k3s ctr -n k8s.io content ls -q' | grep -Fx -- "$missing" >/dev/null; then
  echo 'Previously missing OCI layer remains absent' >&2; exit 1;
fi
remote "$receiver" "k3s crictl inspecti '$image' >/dev/null" || {
  echo 'Image is not available through CRI' >&2; exit 1;
}
bash "$dir/verify-registry-mirror.sh" --require-hit-since "$baseline" \
  "$donor" "$receiver"
printf 'PASS: cold OCI layer pull, digest, CRI availability and new peer-cache hit verified\n'
printf 'IMAGE=%s\nINDEX=%s\nPREVIOUSLY_MISSING_LAYER=%s\nDONOR=%s\nRECEIVER=%s\n' \
  "$image" "$digest" "$missing" "$donor" "$receiver"
