#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  printf 'Usage: %s SOURCE_NODE DESTINATION_NODE IMAGE@sha256:DIGEST --confirm DESTINATION_NODE\n' "$0" >&2
  exit 2
}
[[ $# == 5 && $4 == --confirm && $5 == "$2" ]] || usage
source_node=$1
destination_node=$2
image=$3
[[ $source_node != "$destination_node" ]] || usage
for node in "$source_node" "$destination_node"; do
  case "$node" in
    saturn|jupiter|venus|mercury|janus) ;;
    *) usage ;;
  esac
done
[[ $image =~ ^[a-z0-9][a-z0-9.:-]*(/[a-z0-9][a-z0-9._-]*)+@sha256:[a-f0-9]{64}$ ]] || usage
digest=${image##*@}
user=${OCI_SSH_USER:-root}
[[ $user =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || usage
command -v jq >/dev/null || { echo 'jq is required' >&2; exit 2; }
command -v ssh >/dev/null || { echo 'ssh is required' >&2; exit 2; }
if [[ -n ${KUBECTL_BIN:-} ]]; then
  kubectl=("$KUBECTL_BIN")
elif command -v k3s >/dev/null 2>&1; then
  kubectl=(k3s kubectl)
else
  kubectl=(kubectl)
fi
command -v "${kubectl[0]}" >/dev/null || { echo 'Kubernetes CLI is required' >&2; exit 2; }
(( EUID == 0 )) || { echo 'Root is required to hold the local release lock' >&2; exit 1; }
exec 9>/run/lock/homelab-peer-image-copy.lock
flock -n 9 || { echo 'Another peer image copy is active' >&2; exit 1; }

node_ip() {
  local node=$1 state
  state=$("${kubectl[@]}" get node "$node" -o json)
  jq -er '
    if ([.status.conditions[]? | select(.type=="Ready" and .status=="True")] | length) != 1
    then error("node is not Ready") else
      [.status.addresses[]? | select(.type=="InternalIP") | .address]
      | if length == 1 and (.[0] | test("^[0-9.]+$")) then .[0]
        else error("node has no unique IPv4 InternalIP") end
    end
  ' <<< "$state"
}
source_ip=$(node_ip "$source_node") || { echo 'Source node is unavailable' >&2; exit 1; }
destination_ip=$(node_ip "$destination_node") || { echo 'Destination node is unavailable' >&2; exit 1; }
if [[ $user == root ]]; then
  prefix='k3s'
else
  prefix='sudo -n k3s'
fi
remote() {
  local ip=$1 command=$2
  ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=yes \
    "$user@$ip" "$command"
}
remote "$source_ip" true >/dev/null || { echo 'Source SSH authorization failed' >&2; exit 1; }
remote "$destination_ip" true >/dev/null || { echo 'Destination SSH authorization failed' >&2; exit 1; }

image_record() {
  local ip=$1
  remote "$ip" "$prefix ctr -n k8s.io images ls | awk -v image='$image' '\$1 == image {print \$3}'"
}
complete() {
  local ip=$1
  remote "$ip" "$prefix ctr -n k8s.io images check | awk -v image='$image' '\$1 == image && /complete/ {found=1} END {exit !found}'"
}

source_digest=$(image_record "$source_ip")
[[ $source_digest == "$digest" ]] || {
  echo 'Source image does not have the expected immutable digest' >&2; exit 1;
}
complete "$source_ip" || { echo 'Source image content is incomplete' >&2; exit 1; }
target_digest=$(image_record "$destination_ip")
if [[ -n $target_digest && $target_digest != "$digest" ]]; then
  echo 'Destination has a conflicting image reference; refusing to overwrite it' >&2
  exit 1
fi
if [[ $target_digest == "$digest" ]] && complete "$destination_ip"; then
  result=already_present
else
  remote "$source_ip" "$prefix ctr -n k8s.io images export - '$image'" |
    remote "$destination_ip" "$prefix ctr -n k8s.io images import --digests -" >&2
  actual=$(image_record "$destination_ip")
  [[ $actual == "$digest" ]] || {
    echo 'Imported image digest differs from the expected digest' >&2; exit 1;
  }
  complete "$destination_ip" || { echo 'Imported OCI content is incomplete' >&2; exit 1; }
  result=copied
fi

jq -nc --arg src "$source_node" --arg dst "$destination_node" \
  --arg image "$image" --arg digest "$digest" --arg result "$result" \
  '{source:$src,destination:$dst,image:$image,manifest_digest:$digest,result:$result,content:"complete"}'
