#!/usr/bin/env bash
set -euo pipefail
set +x
umask 077

if (( $# != 4 )); then
  printf 'Usage: %s PROJECT WORKLOAD_NAMESPACE HOST IMAGE@sha256:DIGEST\n' "$0" >&2
  exit 2
fi
project=$1
namespace=$2
host=$3
image=$4

fail() {
  printf 'REFUSED: %s\n' "$*" >&2
  exit 1
}

[[ "$project" =~ ^[a-z0-9]([-a-z0-9]{0,50}[a-z0-9])?$ ]] ||
  fail 'Invalid project identifier.'
[[ "$host" =~ ^(jupiter|venus|mercury|janus)$ ]] ||
  fail 'Unapproved Kubernetes worker host.'
[[ "$image" =~ ^ghcr\.io/dbontr/homelab/engineering-(go|axiom)@sha256:[a-f0-9]{64}$ ]] ||
  fail 'The compiler image must use the approved registry and immutable digest.'
for command in kubectl jq sha256sum grep cut; do
  command -v "$command" >/dev/null || fail "Required command unavailable: $command"
done

key=$(printf '%s' "$project" | sha256sum | cut -c1-12)
[[ "$namespace" == "pwm-$key-$host" ]] ||
  fail 'Namespace does not belong to the specified project and worker.'

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
policy="$script_dir/../images/project-allowlist.json"
[[ -f "$policy" ]] || fail 'Source-owned project image policy is missing.'
jq -e --arg project "$project" --arg image "$image" \
  '.[$project] | type == "array" and index($image) != null' "$policy" >/dev/null ||
  fail 'Compiler image is not admitted by the exact project source policy.'

# Read-only checks: the probe must use the existing isolated project namespace
# and must not weaken its deny-all ingress or egress policy.
kubectl -n "$namespace" get networkpolicy pwm-isolate-workloads -o json |
  jq -e '
    .spec.podSelector == {} and
    (.spec.policyTypes | index("Ingress") != null and index("Egress") != null) and
    (.spec.ingress | length == 0) and (.spec.egress | length == 0)
  ' >/dev/null || fail 'The workload namespace lacks the expected isolation policy.'
[[ "$(kubectl get node "$host" -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}')" == True ]] ||
  fail 'Kubernetes worker is not Ready.'

if [[ "$image" == ghcr.io/dbontr/homelab/engineering-go@* ]]; then
  probe='go version && test "$(go env GOVERSION)" = go1.27.3 && printf "compiler-image-probe-ok\n"'
else
  probe='rustc --version && lean --version && lake --version && rustc --print target-list | grep -qx wasm32-unknown-unknown && printf "compiler-image-probe-ok\n"'
fi

# One isolated, bounded Pod proves that Kubernetes can pull and execute the
# image. The worker Deployment and all project credentials remain unchanged.
run_id=$(cat /proc/sys/kernel/random/uuid)
pod=''
cleanup() {
  local exit_status=$?
  trap - EXIT
  if [[ -n "$pod" ]]; then
    if ! kubectl -n "$namespace" delete pod "$pod" \
      --ignore-not-found --wait=true --timeout=60s >/dev/null; then
      printf 'Probe cleanup is unconfirmed: namespace=%s pod=%s\n' "$namespace" "$pod" >&2
      (( exit_status != 0 )) || exit_status=1
    fi
  fi
  exit "$exit_status"
}
trap cleanup EXIT

pod=$(kubectl -n "$namespace" create -f - -o 'jsonpath={.metadata.name}' <<YAML
apiVersion: v1
kind: Pod
metadata:
  generateName: compiler-image-probe-
  labels:
    homelab.dbontr.me/engineering-probe: "$run_id"
spec:
  nodeName: "$host"
  serviceAccountName: pwm-sandbox
  automountServiceAccountToken: false
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 65534
    runAsGroup: 65534
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: compiler
      image: "$image"
      imagePullPolicy: Always
      command: ["/bin/sh", "-ec"]
      args:
        - |-
          $probe
      resources:
        requests: {cpu: "10m", memory: "32Mi"}
        limits: {cpu: "250m", memory: "256Mi"}
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: ["ALL"]}
YAML
)
[[ "$pod" =~ ^compiler-image-probe-[a-z0-9-]+$ ]] ||
  fail 'The probe Pod name is invalid or absent; inspect the namespace for an orphan.'

if ! kubectl -n "$namespace" wait \
  --for='jsonpath={.status.phase}=Succeeded' "pod/$pod" --timeout=180s >/dev/null; then
  kubectl -n "$namespace" describe "pod/$pod" >&2 || true
  fail 'The compiler image did not execute successfully.'
fi
details=$(kubectl -n "$namespace" get "pod/$pod" -o json)
digest=$(printf '%s' "$image" | cut -d@ -f2)
image_id=$(jq -er --arg image "$image" --arg digest "$digest" '
  if .status.phase == "Succeeded" and
    .spec.containers[0].image == $image and
    .status.containerStatuses[0].state.terminated.exitCode == 0 and
    (.status.containerStatuses[0].imageID | endswith($digest))
  then .status.containerStatuses[0].imageID else error("image digest mismatch") end
' <<< "$details") || fail 'The runtime did not confirm the expected OCI digest.'
[[ "$(kubectl -n "$namespace" logs "pod/$pod" --tail=1)" == compiler-image-probe-ok ]] ||
  fail 'Compiler probe output was not verified.'
kubectl -n "$namespace" get events --field-selector "involvedObject.name=$pod" -o json |
  jq -e '[.items[] | select(.reason == "Pulled")] | length > 0' >/dev/null ||
  fail 'Kubernetes did not report a successful image pull.'
jq -nc --arg project "$project" --arg host "$host" --arg image "$image" \
  --arg image_id "$image_id" --arg pod "$pod" \
  '{project:$project,host:$host,image:$image,image_id:$image_id,probe_pod:$pod,exit_code:0,pull_event:true,result:"passed"}'
