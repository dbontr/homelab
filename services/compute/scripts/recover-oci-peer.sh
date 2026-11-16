#!/usr/bin/env bash
set -euo pipefail
umask 077
(( $# == 4 )) || { echo 'Usage: recover-oci-peer.sh DONOR RECEIVER IMAGE SHA256_DIGEST' >&2; exit 2; }
donor=$1
receiver=$2
image=$3
digest=$4
[[ $donor =~ ^[a-z][a-z0-9-]{0,62}$ && $receiver =~ ^[a-z][a-z0-9-]{0,62}$ && $donor != "$receiver" ]] || exit 2
[[ $image =~ ^images[.]dbontr[.]internal/[a-z0-9._/-]+(:[a-z0-9._-]+|@sha256:[a-f0-9]{64})$ ]] || exit 2
[[ $digest =~ ^sha256:[a-f0-9]{64}$ ]] || exit 2
get_digest() {
  ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o IdentitiesOnly=yes "$1" 'k3s ctr -n k8s.io images ls' |
    awk -v wanted="$image" '$1==wanted {print $3; n++} END {if (n!=1) exit 1}'
}
complete() {
  ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o IdentitiesOnly=yes "$1" 'k3s ctr -n k8s.io images check' |
    awk -v ref="$image" -v sha="$digest" '$1==ref && $3==sha && $4=="complete" {n++} END {if(n!=1) exit 1}'
}
current=$(get_digest "$donor") || { echo 'Donor image missing' >&2; exit 1; }
[[ $current == "$digest" ]] && complete "$donor" || { echo 'Donor digest or content invalid' >&2; exit 1; }
if current=$(get_digest "$receiver"); then
  [[ $current == "$digest" ]] || { echo 'Receiver reference conflicts; refusing overwrite' >&2; exit 1; }
  if complete "$receiver"; then echo 'PASS: complete OCI image already present'; exit 0; fi
fi
echo 'Transferring verified OCI content over host-key-verified SSH'
ssh -o BatchMode=yes -o StrictHostKeyChecking=yes "$donor" "k3s ctr -n k8s.io images export - '$image'" |
  ssh -o BatchMode=yes -o StrictHostKeyChecking=yes "$receiver" 'k3s ctr -n k8s.io images import -'
current=$(get_digest "$receiver") || { echo 'Receiver image absent' >&2; exit 1; }
[[ $current == "$digest" ]] && complete "$receiver" || { echo 'Receiver digest or content incomplete' >&2; exit 1; }
echo 'PASS: receiver digest and all image layers verified'
