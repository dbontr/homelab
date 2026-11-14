#!/usr/bin/env bash
set -euo pipefail

umask 077

usage() {
  printf '%s\n' 'Usage: verify-cluster-image-pulls.sh CONTROLLER@sha256:DIGEST WORKER@sha256:DIGEST NODE [NODE ...]' >&2
  exit 2
}

(( $# >= 3 )) || usage
controller_image=$1
worker_image=$2
shift 2
nodes=("$@")
namespace=${IMAGE_PULL_NAMESPACE:-pwm-system}
timeout_seconds=${IMAGE_PULL_TIMEOUT_SECONDS:-180}

for image in "$controller_image" "$worker_image"; do
  if [[ ! "$image" =~ ^[a-z0-9._:/-]+@sha256:[a-f0-9]{64}$ ]]; then
    printf 'An exact digest-qualified OCI image is required: %s\n' "$image" >&2
    exit 2
  fi
done
if [[ "$controller_image" == "$worker_image" ]]; then
  printf '%s\n' 'Controller and worker references must be distinct' >&2
  exit 2
fi
if [[ ! "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ || ${#namespace} -gt 63 ||
      ! "$timeout_seconds" =~ ^[0-9]{1,3}$ ]] ||
      (( 10#$timeout_seconds < 1 || 10#$timeout_seconds > 600 )); then
  printf '%s\n' 'Invalid Kubernetes namespace or timeout' >&2
  exit 2
fi

declare -A seen_nodes=()
for node in "${nodes[@]}"; do
  if [[ ! "$node" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ || ${#node} -gt 63 ||
        -v seen_nodes[$node] ]]; then
    printf 'Invalid or repeated node name: %s\n' "$node" >&2
    exit 2
  fi
  seen_nodes[$node]=1
done

if [[ -n ${KUBECTL_BIN:-} ]]; then
  kubectl=("$KUBECTL_BIN")
elif command -v kubectl >/dev/null 2>&1; then
  kubectl=(kubectl)
elif command -v k3s >/dev/null 2>&1; then
  kubectl=(k3s kubectl)
else
  printf '%s\n' 'kubectl or k3s is required' >&2
  exit 2
fi
command -v "${kubectl[0]}" >/dev/null 2>&1 || { echo 'Kubernetes CLI is unavailable' >&2; exit 2; }
command -v jq >/dev/null 2>&1 || { echo 'jq is required' >&2; exit 2; }

for node in "${nodes[@]}"; do
  ready=$("${kubectl[@]}" get node "$node" -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}')
  if [[ "$ready" != True ]]; then
    printf 'Node is not Ready: %s\n' "$node" >&2
    exit 1
  fi
done

run_id=$(cat /proc/sys/kernel/random/uuid)
label="homelab.dbontr.me/oci-pull-run=$run_id"
cleanup() {
  local status=$?
  trap - EXIT
  if ! "${kubectl[@]}" -n "$namespace" delete pods -l "$label" \
       --ignore-not-found=true --wait=true --timeout=40s >/dev/null; then
    printf 'Inspect and remove validation Pods with label %s\n' "$label" >&2
    (( status != 0 )) || status=1
  fi
  exit "$status"
}
trap cleanup EXIT

for node in "${nodes[@]}"; do
  for role in worker controller; do
    if [[ "$role" == worker ]]; then image=$worker_image; else image=$controller_image; fi
    pod=$("${kubectl[@]}" -n "$namespace" create -f - \
      -o 'jsonpath={.metadata.name}' <<YAML
apiVersion: v1
kind: Pod
metadata:
  generateName: oci-pull-check-
  labels:
    homelab.dbontr.me/oci-pull-run: "$run_id"
spec:
  nodeName: "$node"
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65534
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: check
      image: "$image"
      imagePullPolicy: Always
      command: ["/bin/sh", "-c", "echo registry-pull-verified"]
      resources:
        requests: {cpu: "10m", memory: "16Mi"}
        limits: {cpu: "100m", memory: "64Mi"}
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: ["ALL"]}
YAML
)
    if ! "${kubectl[@]}" -n "$namespace" wait \
         --for='jsonpath={.status.phase}=Succeeded' "pod/$pod" \
         --timeout="${timeout_seconds}s" >/dev/null; then
      "${kubectl[@]}" -n "$namespace" describe "pod/$pod" >&2 || true
      printf 'Image pull or execution failed on node %s\n' "$node" >&2
      exit 1
    fi

    details=$("${kubectl[@]}" -n "$namespace" get "pod/$pod" -o json)
    if ! jq -e --arg digest "${image##*@}" '
      .status.phase == "Succeeded"
      and .status.containerStatuses[0].state.terminated.exitCode == 0
      and (.status.containerStatuses[0].imageID | endswith($digest))
    ' <<< "$details" >/dev/null; then
      printf 'Completed Pod did not match expected image: %s\n' "$pod" >&2
      exit 1
    fi
    if [[ "$("${kubectl[@]}" -n "$namespace" logs "pod/$pod" --tail=1)" != registry-pull-verified ]]; then
      printf 'Image execution marker was missing: %s\n' "$pod" >&2
      exit 1
    fi
    events=$("${kubectl[@]}" -n "$namespace" get events \
      --field-selector "involvedObject.name=$pod" -o json)
    if ! jq -e '[.items[] | select(.reason == "Pulled")] | length > 0' <<< "$events" >/dev/null; then
      printf 'Kubelet did not report a successful pull: %s\n' "$pod" >&2
      exit 1
    fi
    jq -nc --arg node "$node" --arg role "$role" --arg image "$image" \
      --arg image_id "$(jq -r '.status.containerStatuses[0].imageID' <<< "$details")" \
      '{node:$node,role:$role,image:$image,image_id:$image_id,pull_policy:"Always",exit_code:0,pull_event:true,result:"passed"}'
  done
done
