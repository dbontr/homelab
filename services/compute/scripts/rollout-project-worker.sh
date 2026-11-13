#!/usr/bin/env bash
set -euo pipefail
set +x
umask 077

usage() {
  printf 'Usage: %s plan|apply RELEASE WORKER_IMAGE@sha256:DIGEST [--confirm RELEASE]\n' "$0" >&2
  exit 2
}
(( $# == 3 || $# == 5 )) || usage
mode=$1
release=$2
image=$3
[[ "$mode" == plan || "$mode" == apply ]] || usage
[[ "$release" =~ ^pwm-project-[a-f0-9]{12}-(jupiter|venus|mercury|janus)$ ]] ||
  { echo 'Only an exact project-specific worker Helm release is admitted.' >&2; exit 2; }
[[ "$image" =~ ^ghcr\.io/dbontr/homelab/worker@sha256:[a-f0-9]{64}$ ]] ||
  { echo 'Worker image must use the approved GHCR repository and SHA-256 digest.' >&2; exit 2; }
if [[ "$mode" == apply ]]; then
  [[ $# == 5 && "$4" == --confirm && "$5" == "$release" ]] ||
    { echo 'Apply requires --confirm with the exact release name.' >&2; exit 2; }
else
  (( $# == 3 )) || usage
fi

: "${PWM_HELM_CHART:?Set PWM_HELM_CHART to the reviewed source-owned chart directory}"
namespace="${PWM_SYSTEM_NAMESPACE:-pwm-system}"
[[ "$namespace" == pwm-system ]] || { echo 'Unexpected system namespace.' >&2; exit 2; }
[[ -d "$PWM_HELM_CHART" && -f "$PWM_HELM_CHART/Chart.yaml" ]] ||
  { echo 'The Helm chart directory is unavailable.' >&2; exit 2; }
for command in kubectl helm jq curl base64 sed diff awk date grep sha256sum; do
  command -v "$command" >/dev/null || { echo "Required command unavailable: $command" >&2; exit 2; }
done
host="${release##*-}"
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
chmod 700 "$scratch"

fail() {
  printf 'REFUSED: %s\n' "$*" >&2
  exit 1
}
read_json() {
  jq -e "$@" >/dev/null || fail 'Safety admission data was missing or invalid.'
}

# Take the release's effective values from Helm, not from a stale site profile.
helm -n "$namespace" status "$release" -o json > "$scratch/release.json"
read_json '.info.status == "deployed"' "$scratch/release.json"
initial_revision=$(jq -er '.version | tonumber' "$scratch/release.json")
[[ "$initial_revision" =~ ^[1-9][0-9]*$ ]] || fail 'Helm release revision is invalid.'
helm -n "$namespace" get values "$release" --all -o json > "$scratch/values.json"

read_json --arg host "$host" --arg release "$release" '
  .controller.enabled == false and .worker.enabled == true and
  .worker.workloadNode == $host and .worker.nodeSelector["kubernetes.io/hostname"] == $host and
  (.worker.name | type) == "string" and
  (.worker.projectID | type) == "string" and
  (.worker.instanceID | type) == "string" and
  (.worker.workloadNamespace | type) == "string" and
  (.worker.tokenSecretName | type) == "string" and
  (.worker.controllerCASecretName | type) == "string" and
  (.hardware.nvidia.enabled != true) and (.hardware.intelNpu.enabled != true)
' "$scratch/values.json"
project=$(jq -r '.worker.projectID' "$scratch/values.json")
instance=$(jq -r '.worker.instanceID' "$scratch/values.json")
worker=$(jq -r '.worker.name' "$scratch/values.json")
workload_ns=$(jq -r '.worker.workloadNamespace' "$scratch/values.json")
token_secret=$(jq -r '.worker.tokenSecretName' "$scratch/values.json")
ca_secret=$(jq -r '.worker.controllerCASecretName' "$scratch/values.json")
current_image=$(jq -r '.worker.image' "$scratch/values.json")
key="${release#pwm-project-}"
key="${key%-$host}"
expected_key=$(printf '%s' "$project" | sha256sum | cut -c1-12)
[[ "$key" == "$expected_key" ]] || fail 'Worker release hash does not match the project identifier.'
[[ "$project" =~ ^[a-z0-9]([-a-z0-9]{0,50}[a-z0-9])?$ &&
   "$instance" == "p-$key-$host" &&
   "$worker" == "worker-$key-$host" &&
   "$workload_ns" == "pwm-$key-$host" &&
   "$token_secret" == "pwm-worker-$key-$host-credentials" ]] ||
  fail 'Helm worker identity does not match the registered project release.'
[[ "$current_image" =~ ^[a-z0-9][a-z0-9./:_@-]*$ ]] ||
  fail 'Unsafe currently recorded worker image reference.'
kubectl -n "$namespace" get secret "$token_secret" -o name >/dev/null
kubectl -n "$namespace" get secret "$ca_secret" -o name >/dev/null

deployment="pwm-worker-$instance"
kubectl -n "$namespace" get deployment "$deployment" -o json > "$scratch/live.json"
read_json --arg release "$release" --arg name "$worker" --arg project "$project" \
  --arg host "$host" --arg ns "$workload_ns" --arg secret "$token_secret" \
  --arg ca "$ca_secret" --arg image "$current_image" '
  .metadata.annotations["meta.helm.sh/release-name"] == $release and
  .spec.replicas == 1 and .status.replicas == 1 and
  .status.readyReplicas == 1 and .status.availableReplicas == 1 and
  .status.updatedReplicas == 1 and
  .metadata.generation == .status.observedGeneration and
  .spec.template.spec.serviceAccountName == ("pwm-worker-" + ($name | sub("^worker-"; "p-"))) and
  .spec.template.spec.nodeSelector["kubernetes.io/hostname"] == $host and
  ([.spec.template.spec.containers[] | select(.name == "worker")] | length) == 1 and
  ([.spec.template.spec.containers[] | select(.name == "worker")][0].image == $image) and
  ([.spec.template.spec.containers[] | select(.name == "worker")][0].args |
    index("--node=" + $name) != null and
    index("--project=" + $project) != null and
    index("--namespace=" + $ns) != null) and
  ([.spec.template.spec.containers[] | select(.name == "worker")][0].env |
    any(.name == "PWM_WORKER_TOKEN" and .valueFrom.secretKeyRef.name == $secret)) and
  (.spec.template.spec.volumes |
    any(.name == "ca" and .secret.secretName == $ca))
' "$scratch/live.json"

# The project must not have any pending or executing workload. Controller
# admission and Kubernetes resources are separate observations.
controller_fqdn="pwm-controller.$namespace.svc.cluster.local"
controller_ip=$(kubectl -n "$namespace" get svc pwm-controller -o jsonpath='{.spec.clusterIP}')
[[ "$controller_ip" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] ||
  fail 'Controller ClusterIP is unavailable.'
kubectl -n "$namespace" get secret pwm-controller-ca -o 'jsonpath={.data.ca\.crt}' |
  base64 -d > "$scratch/controller-ca.pem"
admin=$(kubectl -n "$namespace" get secret pwm-controller-credentials \
  -o 'jsonpath={.data.admin_token}' | base64 -d)
[[ ${#admin} -ge 32 && "$admin" != *$'\n'* && "$admin" != *$'\r'* ]] ||
  fail 'The controller administrator credential is unavailable.'
printf 'Authorization: Bearer %s\n' "$admin" > "$scratch/auth.header"
unset admin

controller_get() {
  local url=$1 output=$2
  curl --fail-with-body --silent --show-error --noproxy '*' --max-time 20 \
    --cacert "$scratch/controller-ca.pem" \
    --resolve "$controller_fqdn:8443:$controller_ip" \
    --header "@$scratch/auth.header" \
    --output "$output" \
    "https://$controller_fqdn:8443$url"
}
controller_post() {
  local url=$1 payload=$2 output=$3
  curl --fail-with-body --silent --show-error --noproxy '*' --max-time 20 \
    --cacert "$scratch/controller-ca.pem" \
    --resolve "$controller_fqdn:8443:$controller_ip" \
    --header "@$scratch/auth.header" \
    --header 'Content-Type: application/json' \
    --request POST --data-binary "@$payload" \
    --output "$output" \
    "https://$controller_fqdn:8443$url"
}
maintenance_get() {
  local phase=$1
  controller_get "/api/nodes/$worker/maintenance" "$scratch/maintenance-$phase.json" ||
    fail 'Cannot inspect authoritative worker maintenance state.'
}
maintenance_require() {
  local phase=$1 revision=$2
  read_json --arg worker "$worker" --argjson rev "$revision" '
    .worker_id == $worker and .online == true and .paused == true and .drained == true and
    .revision == $rev and
    .active_jobs == 0 and .active_workspaces == 0 and
    .unfinished_operations == 0 and .hardware_leases == 0 and
    .job_device_leases == 0
  ' "$scratch/maintenance-$phase.json"
}
check_admission() {
  local phase=$1
  kubectl get nodes -o json > "$scratch/nodes-$phase.json"
  read_json --arg host "$host" '
    ([.items[] | select(
      ([.status.conditions[] | select(.type == "Ready" and .status == "True")] | length) == 1
    )] | length) >= 5 and
    ([.items[] | select(.metadata.name == $host and .spec.unschedulable != true)] | length) == 1 and
    all(.items[]; any(.status.conditions[]; .type == "Ready" and .status == "True"))
  ' "$scratch/nodes-$phase.json"
  kubectl -n "$workload_ns" get pods,jobs -o json > "$scratch/resources-$phase.json"
  read_json '(.items | type) == "array" and (.items | length) == 0' \
    "$scratch/resources-$phase.json"
  # The maintenance endpoint counts all active state belonging to this
  # worker. Project job listings are bounded and cannot prove a worker idle.
  controller_get /api/nodes "$scratch/workers-$phase.json" ||
    fail 'Controller worker heartbeat could not be inspected.'
  read_json --arg name "$worker" --arg project "$project" --arg host "$host" '
    type == "array" and
    ([.[] | select(.id == $name and .online == true and
      .capabilities.project_id == $project and
      .capabilities.workload_node == $host and
      .capabilities.kubernetes == true and
      .capabilities.docker == false and
      .capabilities.devices == [])] | length) == 1
  ' "$scratch/workers-$phase.json"
}

# The checked source chart must produce an image-only change. If it changes
# identities, RBAC, namespace policy, security settings, or mounts, it needs
# a separate reviewed migration, not this tool.
helm -n "$namespace" get manifest "$release" > "$scratch/current.yaml"
helm template "$release" "$PWM_HELM_CHART" --namespace "$namespace" \
  -f "$scratch/values.json" \
  --set controller.enabled=false --set worker.enabled=true \
  --set-string "worker.image=$image" > "$scratch/candidate.yaml"
[[ $(grep -c '^kind: Deployment$' "$scratch/candidate.yaml") == 1 ]] ||
  fail 'Candidate contains an unexpected number of Deployments.'
! grep -Fq 'name: pwm-controller' "$scratch/candidate.yaml" ||
  fail 'Candidate contains controller resources.'
grep -Fq "image: \"$current_image\"" "$scratch/current.yaml" ||
  fail 'Current manifest does not contain the exact Helm-recorded image.'
grep -Fq "image: \"$image\"" "$scratch/candidate.yaml" ||
  fail 'Candidate does not contain the approved worker image.'
sed "s#image: \"$current_image\"#image: \"$image\"#g" \
  "$scratch/current.yaml" > "$scratch/expected.yaml"
# Helm's stored manifest can end with one extra empty line after the
# final resource. Compare the resource bodies without that terminal separator.
sed '${/^$/d;}' "$scratch/expected.yaml" > "$scratch/expected-normalized.yaml"
sed '${/^$/d;}' "$scratch/candidate.yaml" > "$scratch/candidate-normalized.yaml"
# A release can predate the worker's read-only check of its isolation
# NetworkPolicy. This is the only non-image chart migration admitted here:
# one "get" permission for one named NetworkPolicy in its own namespace.
if ! diff -u "$scratch/expected-normalized.yaml" "$scratch/candidate-normalized.yaml" > "$scratch/manifest.diff"; then
  ! grep -Fq '    resourceNames: ["pwm-isolate-workloads"]' "$scratch/expected-normalized.yaml" ||
    fail 'Candidate changes resources beyond the worker image.'
  awk '
    /^  - apiGroups: \["batch"\]$/ { in_jobs = 1 }
    {
      print
      if (in_jobs && $0 == "    verbs: [\"get\", \"list\", \"watch\", \"create\", \"delete\"]") {
        print "  - apiGroups: [\"networking.k8s.io\"]"
        print "    resources: [\"networkpolicies\"]"
        print "    resourceNames: [\"pwm-isolate-workloads\"]"
        print "    verbs: [\"get\"]"
        additions++
        in_jobs = 0
      }
    }
    END { if (additions != 1) exit 1 }
  ' "$scratch/expected-normalized.yaml" > "$scratch/expected-scoped-policy.yaml" ||
    fail 'The worker Role does not match the expected narrow policy migration.'
  diff -u "$scratch/expected-scoped-policy.yaml" "$scratch/candidate-normalized.yaml" > "$scratch/manifest.diff" ||
    fail 'Candidate changes resources beyond the worker image and the named policy read.'
  printf 'ADMISSION: scoped read of the workload isolation NetworkPolicy is the only chart change.\n'
fi
helm upgrade "$release" "$PWM_HELM_CHART" --namespace "$namespace" \
  --reset-then-reuse-values --set controller.enabled=false \
  --set worker.enabled=true --set-string "worker.image=$image" \
  --dry-run=server --hide-secret > "$scratch/dry-run.txt"

check_admission preflight
maintenance_get preflight
read_json --arg worker "$worker" '
  .worker_id == $worker and .online == true and .paused == false and .drained == true and
  (.revision | type) == "number" and
  .active_jobs == 0 and .active_workspaces == 0 and
  .unfinished_operations == 0 and .hardware_leases == 0 and
  .job_device_leases == 0
' "$scratch/maintenance-preflight.json"
if [[ "$current_image" == "$image" ]]; then
  printf 'ACCEPTED: %s is already digest-pinned and ready; no release change.\n' "$release"
  exit 0
fi
printf 'PLAN: %s (%s) on %s\n' "$release" "$project" "$host"
printf 'IMAGE: %s -> %s\n' "$current_image" "$image"
printf 'CHECKS: controller, node, pods, jobs, workspaces, Helm state and image-only manifest passed.\n'
if [[ "$mode" == plan ]]; then
  printf 'PLAN ONLY: no Kubernetes resources have been modified.\n'
  exit 0
fi

# A controller-side pause is serialized with job leases and workspace
# allocation by the same SQLite write lock. A stale plan is not a scheduler
# barrier. The revision prevents this release from clearing another pause.
check_admission apply
maintenance_get before-pause
read_json --arg worker "$worker" '
  .worker_id == $worker and .online == true and .paused == false and .drained == true and
  (.revision | type) == "number"
' "$scratch/maintenance-before-pause.json"
maintenance_previous_revision=$(jq -er '.revision | tonumber' "$scratch/maintenance-before-pause.json")
jq -n --argjson rev "$maintenance_previous_revision" '
  {paused:true,expected_revision:$rev}
' > "$scratch/pause-request.json"
controller_post "/api/nodes/$worker/maintenance"   "$scratch/pause-request.json" "$scratch/paused.json" ||
  fail 'Cannot confirm scheduler pause; inspect maintenance state before retrying.'
pause_revision=$(jq -er '.revision | tonumber' "$scratch/paused.json") ||
  fail 'Pause result lacks a maintenance revision. Leave this worker paused.'
[[ "$pause_revision" == "$((maintenance_previous_revision + 1))" ]] ||
  fail 'Unexpected maintenance revision after pause. Leave this worker paused.'
cp "$scratch/paused.json" "$scratch/maintenance-paused.json"
maintenance_require paused "$pause_revision"
check_admission paused
maintenance_get before-upgrade
maintenance_require before-upgrade "$pause_revision"
previous_revision=$(helm -n "$namespace" status "$release" -o json |
  jq -er '.version | tonumber')
[[ "$previous_revision" =~ ^[1-9][0-9]*$ && "$previous_revision" == "$initial_revision" ]] ||
  fail 'The Helm revision changed during admission.'
kubectl -n "$namespace" get deployment "$deployment" -o json |
  jq -e --arg image "$current_image" '
    .status.readyReplicas == 1 and .status.updatedReplicas == 1 and
    ([.spec.template.spec.containers[] | select(.name == "worker")][0].image == $image)
  ' >/dev/null || fail 'The running worker changed during admission.'

if ! helm upgrade "$release" "$PWM_HELM_CHART" --namespace "$namespace" \
  --reset-then-reuse-values --set controller.enabled=false \
  --set worker.enabled=true --set-string "worker.image=$image" \
  --atomic --wait --timeout 5m; then
  fail 'Helm upgrade failed; inspect atomic rollback and live worker state.'
fi
if kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout=90s &&
   kubectl -n "$namespace" get deployment "$deployment" -o json |
     jq -e --arg image "$image" '
       .status.readyReplicas == 1 and .status.availableReplicas == 1 and
       .status.updatedReplicas == 1 and
       .metadata.generation == .status.observedGeneration and
       ([.spec.template.spec.containers[] | select(.name == "worker")][0].image == $image)
     ' >/dev/null; then
  # The old Pod can heartbeat after the upgrade starts but before its
  # replacement is Ready. Require an observation after readiness, not
  # merely one later than Helm invocation.
  ready_after=$(date +%s)
  for attempt in {1..20}; do
    if controller_get /api/nodes "$scratch/after.json" &&
      jq -e --arg name "$worker" --argjson since "$ready_after" '
      any(.[]; .id == $name and .online == true and (.last_seen | tonumber) >= $since + 1)
    ' "$scratch/after.json" >/dev/null; then
      maintenance_get before-resume
      maintenance_require before-resume "$pause_revision"
      jq -n --argjson rev "$pause_revision" '{paused:false,expected_revision:$rev}'         > "$scratch/resume-request.json"
      controller_post "/api/nodes/$worker/maintenance"         "$scratch/resume-request.json" "$scratch/resumed.json" ||
        fail 'Worker upgraded, but scheduler resume is unconfirmed; reconcile the pause explicitly.'
      read_json --arg worker "$worker" --argjson rev "$((pause_revision+1))" '
        .worker_id == $worker and .online == true and .paused == false and .revision == $rev
      ' "$scratch/resumed.json"
      printf 'ACCEPTED: %s running %s with a confirmed heartbeat and resumed scheduler.\n' "$release" "$image"
      exit 0
    fi
    sleep 3
  done
fi

printf 'Post-upgrade acceptance failed for %s; checking rollback ownership.\n' "$release" >&2
revision=$(helm -n "$namespace" status "$release" -o json |
  jq -er '.version | tonumber') || fail 'Cannot safely identify current Helm revision.'
if (( revision != previous_revision + 1 )); then
  fail 'Concurrent Helm revision change detected; manual reconciliation required.'
fi
helm rollback "$release" "$previous_revision" --namespace "$namespace" \
  --wait --timeout 5m ||
  fail 'Rollback failed; manual operator recovery is required.'
kubectl -n "$namespace" get deployment "$deployment" -o json |
  jq -e --arg image "$current_image" '
    .status.readyReplicas == 1 and
    ([.spec.template.spec.containers[] | select(.name == "worker")][0].image == $image)
  ' >/dev/null ||
  fail 'Rollback readiness and image were not confirmed; manual recovery required.'
fail 'Post-upgrade acceptance failed; the prior Helm image was restored, but the controller pause remains active until explicit recovery.'
