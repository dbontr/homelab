#!/usr/bin/env bash
set -euo pipefail

unsupported() {
  printf 'UNSUPPORTED: %s\n' "$*" >&2
  exit 77
}
refuse() {
  printf 'REFUSED: %s\n' "$*" >&2
  exit 2
}

(( $# == 1 )) || refuse 'Usage: disposable-k3s-preflight.sh CLUSTER_NAME'
cluster=$1
[[ $cluster =~ ^compute-qualification-[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
  refuse 'Cluster name must start with compute-qualification- and contain lowercase DNS characters'
(( ${#cluster} <= 49 )) || refuse 'Cluster name exceeds the test naming limit'

# Never consult the current/default Kubernetes context. A qualification run
# requires a separate, explicitly selected k3d cluster and kubeconfig file.
[[ -n ${KUBECONFIG:-} && $KUBECONFIG == /* && $KUBECONFIG != *:* && -f $KUBECONFIG ]] ||
  unsupported 'An absolute, single-file KUBECONFIG for a disposable k3d cluster is required'
for tool in kubectl k3d docker jq go pkg-config; do
  command -v "$tool" >/dev/null 2>&1 || unsupported "$tool is unavailable"
done
pkg-config --exists sqlite3 || unsupported 'SQLite development files are unavailable'

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
required_go=$(awk '$1 == "go" {print $2; exit}' "$root/go.mod")
installed_go=$(GOTOOLCHAIN=local go version | awk '{sub(/^go/, "", $3); print $3}')
[[ -n $required_go && $installed_go == "$required_go" ]] ||
  unsupported "Go $required_go is required; local Go version is $installed_go"

docker info >/dev/null 2>&1 || unsupported 'No usable local Docker engine for disposable nodes'
context=$(kubectl --kubeconfig "$KUBECONFIG" config current-context) ||
  unsupported 'Cannot inspect the explicit test kubeconfig'
[[ $context == "k3d-$cluster" ]] || refuse 'Kubernetes context does not identify the requested disposable k3d cluster'
k3d cluster list "$cluster" -o json |
  jq -e --arg c "$cluster" '[.[] | select(.name == $c)] | length == 1' >/dev/null ||
  unsupported 'Requested disposable k3d cluster does not exist'

nodes=$(kubectl --kubeconfig "$KUBECONFIG" get nodes -o json) ||
  unsupported 'Cannot inspect nodes through the explicit test kubeconfig'
if ! jq -e --arg prefix "k3d-$cluster-" '
  (.items | length >= 3) and
  all(.items[];
    (.metadata.name | startswith($prefix)) and
    (any(.status.conditions[]?; .type == "Ready" and .status == "True")))
' <<< "$nodes" >/dev/null; then
  refuse 'Nodes must all be Ready disposable k3d containers; production or ambiguous nodes are prohibited'
fi
printf '%s\n' "$nodes" | jq -c --arg c "$cluster" '
  {qualification_cluster:$c, nodes:[.items[].metadata.name],
   isolation:"explicit_kubeconfig_and_disposable_k3d_nodes_verified",
   production_changes:false, result:"preflight_passed"}'
