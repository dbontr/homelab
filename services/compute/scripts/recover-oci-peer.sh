#!/usr/bin/env bash
set -euo pipefail
umask 077
(( $# == 4 )) || { echo 'Usage: recover-oci-peer.sh DONOR RECEIVER IMAGE SHA256_DIGEST' >&2; exit 2; }
donor=$1
receiver=$2
image=$3
digest=$4
case "$donor" in janus|jupiter|mercury|venus) ;; *) exit 2 ;; esac
case "$receiver" in janus|jupiter|mercury|venus) ;; *) exit 2 ;; esac
[[ $donor != "$receiver" ]] || exit 2
[[ $image =~ ^images[.]dbontr[.]internal/[a-z0-9._/-]+(:[a-z0-9._-]+|@sha256:[a-f0-9]{64})$ ]] || exit 2
[[ $digest =~ ^sha256:[a-f0-9]{64}$ ]] || exit 2
command -v timeout >/dev/null || exit 2
remote() {
  timeout 300 ssh -o BatchMode=yes -o StrictHostKeyChecking=yes \
    -o IdentitiesOnly=yes -o ConnectTimeout=10 \
    -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "$@"
}
get_digest() {
  remote "$1" 'k3s ctr -n k8s.io images ls' |
    awk -v wanted="$image" '$1==wanted {print $3; n++} END {if (n!=1) exit 1}'
}
complete() {
  remote "$1" 'k3s ctr -n k8s.io images check' |
    awk -v ref="$image" -v sha="$digest" '$1==ref && $3==sha && $4=="complete" {n++} END {if(n!=1) exit 1}'
}
current=$(get_digest "$donor") || { echo 'Donor image missing' >&2; exit 1; }
[[ $current == "$digest" ]] && complete "$donor" || { echo 'Donor digest or content invalid' >&2; exit 1; }
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
labels_script="$script_dir/label-image-content.sh"
[[ -f "$labels_script" ]] || { echo 'OCI content source verifier is missing' >&2; exit 1; }

transferred=false
if current=$(get_digest "$receiver"); then
  [[ $current == "$digest" ]] || { echo 'Receiver reference conflicts; refusing overwrite' >&2; exit 1; }
  if ! complete "$receiver"; then transferred=true; fi
else
  transferred=true
fi
if [[ $transferred == true ]]; then
  echo 'Transferring verified OCI content over host-key-verified SSH'
  remote "$donor" "k3s ctr -n k8s.io images export - '$image'" |
    remote "$receiver" 'k3s ctr -n k8s.io images import -'
fi
current=$(get_digest "$receiver") || { echo 'Receiver image absent' >&2; exit 1; }
[[ $current == "$digest" ]] && complete "$receiver" || { echo 'Receiver digest or content incomplete' >&2; exit 1; }

# Direct containerd imports can omit the distribution-source labels Spegel needs.
# Repair only missing labels after the verifier checks every OCI content digest.
remote_jq='for j in /nix/store/*-jq-*/bin/jq; do test -x "$j" && export PATH="${j%/jq}:$PATH" && break; done;'
remote "$receiver" "$remote_jq bash -s -- --apply '$image' --confirm '$image'" < "$labels_script" || {
  echo 'Receiver image source metadata could not be repaired' >&2
  exit 1
}
remote "$receiver" "$remote_jq bash -s -- --check '$image'" < "$labels_script" || {
  echo 'Receiver image source metadata verification failed' >&2
  exit 1
}
if [[ $transferred == true ]]; then
  echo 'PASS: peer image transfer, digest integrity, and source metadata verified'
else
  echo 'PASS: complete peer image and source metadata verified'
fi
