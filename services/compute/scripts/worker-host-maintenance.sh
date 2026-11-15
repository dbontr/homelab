#!/usr/bin/env bash
# Controller-owned, revision-fenced maintenance for every worker on a K3s host.
set -euo pipefail
set +x
umask 077

usage() {
  printf 'Usage: %s plan HOST | pause HOST --confirm HOST | resume HOST --confirm HOST\n' "$0" >&2
  exit 2
}
(( $# >= 2 )) || usage
mode=$1
host=$2
case "$host" in jupiter|venus|mercury|janus) ;; *) usage ;; esac
case "$mode" in
  plan) (( $# == 2 )) || usage ;;
  pause|resume) (( $# == 4 )) && [[ $3 == --confirm && $4 == "$host" ]] || usage ;;
  *) usage ;;
esac
[[ $EUID == 0 ]] || { echo 'Root is required for host maintenance.' >&2; exit 2; }
for command in kubectl curl jq base64 flock mktemp sort; do
  command -v "$command" >/dev/null || { printf 'Missing tool: %s\n' "$command" >&2; exit 2; }
done

state_dir=${PWM_HOST_MAINTENANCE_DIR:-/var/lib/pwm-host-maintenance}
install -d -m 0700 "$state_dir"
exec 9>/run/lock/pwm-host-maintenance.lock
flock -n 9 || { echo 'Another host maintenance operation is active.' >&2; exit 1; }
state="$state_dir/$host"
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
fail() { printf 'REFUSED: %s\n' "$*" >&2; exit 1; }

namespace=pwm-system
service=pwm-controller.$namespace.svc.cluster.local
address=$(kubectl -n "$namespace" get svc pwm-controller -o jsonpath='{.spec.clusterIP}')
[[ "$address" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || fail 'Controller service address is invalid.'
kubectl -n "$namespace" get secret pwm-controller-ca -o 'jsonpath={.data.ca\.crt}' | base64 -d > "$scratch/ca"
token=$(kubectl -n "$namespace" get secret pwm-controller-credentials -o 'jsonpath={.data.admin_token}' | base64 -d)
[[ ${#token} -ge 32 && "$token" != *$'\n'* ]] || fail 'Controller credential is unavailable.'
printf 'Authorization: Bearer %s\n' "$token" > "$scratch/auth"
unset token

controller_get() {
  local endpoint=$1 output=$2
  curl --fail-with-body -fsS --noproxy '*' --max-time 20 \
    --cacert "$scratch/ca" --resolve "$service:8443:$address" \
    --header "@$scratch/auth" --output "$output" \
    "https://$service:8443$endpoint"
}
controller_post() {
  local endpoint=$1 payload=$2 output=$3
  curl --fail-with-body -fsS --noproxy '*' --max-time 20 \
    --cacert "$scratch/ca" --resolve "$service:8443:$address" \
    --header "@$scratch/auth" --header 'Content-Type: application/json' \
    --data-binary "@$payload" --output "$output" \
    "https://$service:8443$endpoint"
}
list_ids() {
  controller_get /api/nodes "$scratch/nodes"
  jq -r --arg host "$host" \
    '.[] | select(.capabilities.workload_node == $host) | .id' "$scratch/nodes" | sort -u
}
status() {
  local id=$1
  controller_get "/api/nodes/$id/maintenance" "$scratch/maintenance"
}
check_drained() {
  local id=$1
  jq -e --arg id "$id" '
    .worker_id == $id and .online == true and .drained == true and
    .active_jobs == 0 and .active_workspaces == 0 and
    .unfinished_operations == 0 and .hardware_leases == 0 and
    .job_device_leases == 0 and
    (.revision | type) == "number" and .revision >= 0
  ' "$scratch/maintenance" >/dev/null
}
node_ready() {
  [[ "$(kubectl get node "$host" -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}')" == True ]]
}

list_ids > "$scratch/ids"
[[ -s "$scratch/ids" ]] || fail 'The controller has no workers assigned to this host.'
while IFS= read -r id; do
  [[ "$id" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ && ${#id} -le 63 ]] ||
    fail 'Invalid controller worker identity.'
done < "$scratch/ids"

if [[ "$mode" == plan || "$mode" == pause ]]; then
  [[ ! -e "$state" ]] || fail 'The host has an unfinished maintenance receipt.'
  # A second host must not be paused by the same controller-side operator.
  if find "$state_dir" -mindepth 1 -maxdepth 1 -type f | grep -q .; then
    fail 'Another host has an unfinished maintenance receipt.'
  fi
  node_ready || fail 'Kubernetes node is not Ready.'
  [[ "$(kubectl get node "$host" -o jsonpath='{.spec.unschedulable}')" != true ]] ||
    fail 'Node is already cordoned.'
  kubectl get nodes -o json | jq -e '
    (.items | length) == 5 and
    all(.items[]; any(.status.conditions[]; .type == "Ready" and .status == "True"))
  ' >/dev/null || fail 'Fleet is not five-of-five Ready.'
  : > "$scratch/baseline"
  while IFS= read -r id; do
    status "$id" || fail 'Maintenance API is unavailable.'
    check_drained "$id" || fail "Worker $id is busy or offline."
    jq -e '.paused == false' "$scratch/maintenance" >/dev/null ||
      fail "Worker $id is already paused by another operator."
    revision=$(jq -er '.revision | floor' "$scratch/maintenance")
    printf '%s %s\n' "$id" "$revision" >> "$scratch/baseline"
  done < "$scratch/ids"
  printf 'HOST %s: %s controller workers online and drained; all five nodes Ready.\n' \
    "$host" "$(wc -l < "$scratch/baseline")"
  if [[ "$mode" == plan ]]; then
    printf 'PLAN ONLY: no scheduling or host state changed.\n'
    exit 0
  fi
  # Persist the complete baseline before the first controller mutation.
  install -m 0600 "$scratch/baseline" "$state"
  kubectl cordon "$host"
  while read -r id revision; do
    status "$id" || fail "Cannot refresh $id; leave the host cordoned."
    check_drained "$id" || fail "Worker $id is busy; leave maintenance in place."
    jq -e --argjson revision "$revision" \
      '.paused == false and .revision == $revision' "$scratch/maintenance" >/dev/null ||
      fail "Worker $id changed after the maintenance baseline."
    jq -nc --argjson revision "$revision" \
      '{paused:true,expected_revision:$revision}' > "$scratch/request"
    controller_post "/api/nodes/$id/maintenance" "$scratch/request" "$scratch/response" ||
      fail "Could not pause $id. Use the receipt to reconcile."
    jq -e --arg id "$id" --argjson revision "$revision" '
      .worker_id == $id and .paused == true and .drained == true and
      .revision == ($revision + 1)
    ' "$scratch/response" >/dev/null || fail "Uncertain pause result for $id."
  done < "$state"
  list_ids > "$scratch/check-ids"
  cmp "$scratch/ids" "$scratch/check-ids" >/dev/null ||
    fail 'Controller membership changed during pause; do not activate.'
  while read -r id revision; do
    status "$id" && check_drained "$id" || fail "Worker $id is not drained."
    jq -e --argjson revision "$revision" '
      .paused == true and .revision == ($revision + 1)
    ' "$scratch/maintenance" >/dev/null || fail "Worker $id lost its pause."
  done < "$state"
  printf 'PAUSED: %s workers on %s. Node is cordoned; persistent receipt: %s\n' \
    "$(wc -l < "$state")" "$host" "$state"
  printf 'Do not resume until the node and all worker deployments are verified.\n'
  exit 0
fi

[[ -f "$state" && -s "$state" ]] || fail 'No owned host-maintenance receipt exists.'
node_ready || fail 'The selected Kubernetes node is not Ready.'
kubectl -n "$namespace" get deployments -o json | jq -e '
  all(.items[]; .spec.replicas == .status.readyReplicas and
                .metadata.generation == .status.observedGeneration)
' >/dev/null || fail 'Workspace deployment readiness is incomplete.'
cut -d' ' -f1 "$state" | sort -u > "$scratch/expected-ids"
cmp "$scratch/expected-ids" "$scratch/ids" >/dev/null ||
  fail 'Controller worker membership drifted; do not resume.'
# Verify every status before changing any maintenance state.
while read -r id revision; do
  status "$id" && check_drained "$id" || fail "Worker $id is offline or busy."
  jq -e --argjson revision "$revision" '
    (.paused == true and .revision == ($revision + 1)) or
    (.paused == false and (.revision == $revision or .revision == ($revision + 2)))
  ' "$scratch/maintenance" >/dev/null || fail "Unexpected maintenance revision for $id."
done < "$state"
while read -r id revision; do
  status "$id" || fail "Cannot refresh $id."
  if jq -e '.paused == false' "$scratch/maintenance" >/dev/null; then continue; fi
  jq -nc --argjson revision "$((revision+1))" \
    '{paused:false,expected_revision:$revision}' > "$scratch/request"
  controller_post "/api/nodes/$id/maintenance" "$scratch/request" "$scratch/response" ||
    fail "Cannot resume $id; keep the host cordoned."
  jq -e --arg id "$id" --argjson revision "$revision" '
    .worker_id == $id and .paused == false and .revision == ($revision + 2)
  ' "$scratch/response" >/dev/null || fail "Unexpected resume result for $id."
done < "$state"
kubectl uncordon "$host"
rm -f -- "$state"
printf 'RESUMED: %s. All controller workers were released and the node is schedulable.\n' "$host"
