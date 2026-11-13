#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(CDPATH= cd -- "$(dirname -- "$0")/../../../.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin" "$scratch/chart"
export MOCK_ROLLOUT="$scratch"
printf 'apiVersion: v2\nname: project-workspace\nversion: 0.1.0\n' > "$scratch/chart/Chart.yaml"
release=pwm-project-046910e8ae58-janus
digest=ghcr.io/dbontr/homelab/worker@sha256:c7ab035296db9aec360611c52737ca4e94143fb959c7ad56ca536cc20fd6ee4f
old=docker.io/library/project-workspace-worker:cuda-exec-756b88c

cat > "$scratch/values.json" <<'JSON'
{
  "controller":{"enabled":false},
  "worker":{
    "enabled":true,
    "image":"docker.io/library/project-workspace-worker:cuda-exec-756b88c",
    "name":"worker-046910e8ae58-janus",
    "projectID":"aup-zu3-pynq",
    "instanceID":"p-046910e8ae58-janus",
    "workloadNode":"janus",
    "workloadNamespace":"pwm-046910e8ae58-janus",
    "tokenSecretName":"pwm-worker-046910e8ae58-janus-credentials",
    "controllerCASecretName":"pwm-controller-ca",
    "nodeSelector":{"kubernetes.io/hostname":"janus"}
  },
  "hardware":{"nvidia":{"enabled":false},"intelNpu":{"enabled":false}}
}
JSON
cat > "$scratch/live.json" <<'JSON'
{
  "metadata":{"annotations":{"meta.helm.sh/release-name":"pwm-project-046910e8ae58-janus"},"generation":1},
  "spec":{"replicas":1,"template":{"spec":{
    "serviceAccountName":"pwm-worker-p-046910e8ae58-janus",
    "nodeSelector":{"kubernetes.io/hostname":"janus"},
    "containers":[{
      "name":"worker",
      "image":"docker.io/library/project-workspace-worker:cuda-exec-756b88c",
      "args":["serve","--node=worker-046910e8ae58-janus","--project=aup-zu3-pynq","--namespace=pwm-046910e8ae58-janus"],
      "env":[{"name":"PWM_WORKER_TOKEN","valueFrom":{"secretKeyRef":{"name":"pwm-worker-046910e8ae58-janus-credentials"}}}]
    }],
    "volumes":[{"name":"ca","secret":{"secretName":"pwm-controller-ca"}}]
  }}},
  "status":{"replicas":1,"readyReplicas":1,"availableReplicas":1,"updatedReplicas":1,"observedGeneration":1}
}
JSON
jq --arg image "$digest" '.spec.template.spec.containers[0].image = $image' \
  "$scratch/live.json" > "$scratch/after.json"
cat > "$scratch/current.yaml" <<'YAML'
---
# Source: project-workspace/templates/worker.yaml
kind: Deployment
metadata:
  name: pwm-worker-p-046910e8ae58-janus
spec:
  template:
    spec:
      containers:
        - name: worker
          image: "docker.io/library/project-workspace-worker:cuda-exec-756b88c"

YAML
sed "s#image: \"$old\"#image: \"$digest\"#" \
  "$scratch/current.yaml" > "$scratch/candidate.yaml"
jq -n '{items:([ "saturn", "jupiter", "venus", "mercury", "janus" ] |
  map({metadata:{name:.},spec:{unschedulable:false},status:{conditions:[{type:"Ready",status:"True"}]}}))}' \
  > "$scratch/nodes.json"
printf '{"items":[]}\n' > "$scratch/resources.json"
printf '[]\n' > "$scratch/jobs.json"
printf '[]\n' > "$scratch/workspaces.json"
cat > "$scratch/workers.json" <<'JSON'
[{"id":"worker-046910e8ae58-janus","online":true,"last_seen":4102444800,
"capabilities":{"kubernetes":true,"docker":false,"devices":[],"project_id":"aup-zu3-pynq","workload_node":"janus"}}]
JSON

printf '[{"name":"pwm-project-046910e8ae58-janus","status":"deployed"}]\n' > "$scratch/helm-list.json"
jq -s '{items:.}' "$scratch/live.json" > "$scratch/deployments.json"

cat > "$scratch/bin/helm" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == -n ]]; then shift 2; fi
case "${1:-}" in
  list) cat "$MOCK_ROLLOUT/helm-list.json" ;;
  status)
    if [[ -f "$MOCK_ROLLOUT/applied" ]]; then
      printf '{"info":{"status":"deployed"},"version":5}\n'
    else
      printf '{"info":{"status":"deployed"},"version":4}\n'
    fi ;;
  get)
    case "$2" in
      values) cat "$MOCK_ROLLOUT/values.json" ;;
      manifest) cat "$MOCK_ROLLOUT/current.yaml" ;;
      *) exit 90 ;;
    esac ;;
  template) cat "$MOCK_ROLLOUT/candidate.yaml" ;;
  upgrade)
    if [[ " $* " == *' --dry-run=server '* ]]; then
      printf 'dryrun\n' >> "$MOCK_ROLLOUT/trace"
    else
      printf 'upgrade\n' >> "$MOCK_ROLLOUT/trace"
      touch "$MOCK_ROLLOUT/applied"
    fi ;;
  rollback)
    printf 'rollback\n' >> "$MOCK_ROLLOUT/trace"
    rm -f "$MOCK_ROLLOUT/applied" ;;
  *) printf 'Unexpected helm command: %s\n' "$*" >&2; exit 91 ;;
esac
MOCK
cat > "$scratch/bin/kubectl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
args=" $* "
case "$args" in
  *' get deployments '*) cat "$MOCK_ROLLOUT/deployments.json" ;;
  *' get deployment '*)
    if [[ -f "$MOCK_ROLLOUT/applied" ]]; then
      cat "$MOCK_ROLLOUT/after.json"
    else
      cat "$MOCK_ROLLOUT/live.json"
    fi ;;
  *' get nodes '*) cat "$MOCK_ROLLOUT/nodes.json" ;;
  *' get pods,jobs '*) cat "$MOCK_ROLLOUT/resources.json" ;;
  *' get svc '*)
    printf '10.43.0.10' ;;
  *' get secret '*)
    if [[ "$args" == *'data.admin_token'* ]]; then
      printf 'controller-test-admin-token-0123456789abcdef' | base64 -w 0
    elif [[ "$args" == *'data.ca'* ]]; then
      printf 'test-controller-ca' | base64 -w 0
    else
      printf 'secret/mocked\n'
    fi ;;
  *' rollout status '*) : ;;
  *) printf 'Unexpected kubectl command: %s\n' "$*" >&2; exit 92 ;;
esac
MOCK
cat > "$scratch/bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
output=''
header=''
payload=''
method=''
previous=''
for arg in "$@"; do
  if [[ "$arg" == *'controller-test-admin-token'* ]]; then
    printf 'Controller credential exposed in curl process arguments\n' >&2
    exit 93
  fi
  if [[ "$previous" == --output ]]; then output="$arg"; fi
  if [[ "$previous" == --header && "$arg" == @* ]]; then header="${arg#@}"; fi
  if [[ "$previous" == --request ]]; then method="$arg"; fi
  if [[ "$previous" == --data-binary && "$arg" == @* ]]; then payload="${arg#@}"; fi
  previous="$arg"
done
[[ -n "$output" && -n "$header" ]] || exit 94
grep -q '^Authorization: Bearer controller-test-admin-token' "$header"
if [[ -f "$MOCK_ROLLOUT/api-down" && -f "$MOCK_ROLLOUT/applied" ]]; then
  exit 22
fi
case "${*: -1}" in
  */jobs) cat "$MOCK_ROLLOUT/jobs.json" > "$output" ;;
  */workspaces) cat "$MOCK_ROLLOUT/workspaces.json" > "$output" ;;
  */nodes) cat "$MOCK_ROLLOUT/workers.json" > "$output" ;;
  */maintenance)
    if [[ "$method" == POST ]]; then
      [[ -n "$payload" ]] || exit 96
      if jq -e '.paused == true and .expected_revision == 0' "$payload" >/dev/null; then
        [[ ! -f "$MOCK_ROLLOUT/paused" ]] || exit 97
        touch "$MOCK_ROLLOUT/paused"
      elif jq -e '.paused == false and .expected_revision == 1' "$payload" >/dev/null; then
        [[ -f "$MOCK_ROLLOUT/paused" ]] || exit 98
        rm -f "$MOCK_ROLLOUT/paused"
      else
        printf 'Invalid maintenance revision request\n' >&2
        exit 99
      fi
    fi
    if [[ -f "$MOCK_ROLLOUT/paused" ]]; then
      printf '{"worker_id":"worker-046910e8ae58-janus","online":true,"paused":true,"revision":1,"drained":true,"active_jobs":0,"active_workspaces":0,"unfinished_operations":0,"hardware_leases":0,"job_device_leases":0}\n' > "$output"
    elif [[ "$method" == POST ]]; then
      printf '{"worker_id":"worker-046910e8ae58-janus","online":true,"paused":false,"revision":2,"drained":true,"active_jobs":0,"active_workspaces":0,"unfinished_operations":0,"hardware_leases":0,"job_device_leases":0}\n' > "$output"
    elif [[ -f "$MOCK_ROLLOUT/busy" ]]; then
      printf '{"worker_id":"worker-046910e8ae58-janus","online":true,"paused":false,"revision":0,"drained":false,"active_jobs":1,"active_workspaces":0,"unfinished_operations":0,"hardware_leases":0,"job_device_leases":0}\n' > "$output"
    else
      printf '{"worker_id":"worker-046910e8ae58-janus","online":true,"paused":false,"revision":0,"drained":true,"active_jobs":0,"active_workspaces":0,"unfinished_operations":0,"hardware_leases":0,"job_device_leases":0}\n' > "$output"
    fi ;;
  *) printf 'Unexpected controller URL\n' >&2; exit 95 ;;
esac
MOCK
cat > "$scratch/bin/sleep" <<'MOCK'
#!/usr/bin/env bash
exit 0
MOCK
chmod 700 "$scratch/bin/"*
export PATH="$scratch/bin:$PATH" PWM_HELM_CHART="$scratch/chart"
script="$root/services/compute/scripts/rollout-project-worker.sh"

fail() {
  printf 'TEST FAILED: %s\n' "$*" >&2
  exit 1
}

# Neither validation failure nor plan mode may issue a live Helm upgrade.
if bash "$script" apply "$release" "$digest" > "$scratch/output" 2>&1; then
  fail 'Apply without release confirmation was accepted'
fi
if bash "$script" plan "$release" 'worker:unverified' > "$scratch/output" 2>&1; then
  fail 'Mutable worker image was accepted'
fi
if bash "$script" plan 'pwm-controller' "$digest" > "$scratch/output" 2>&1; then
  fail 'Controller release was admitted as a worker'
fi
[[ ! -f "$scratch/trace" ]] || fail 'Invalid input reached Helm'

bash "$script" plan "$release" "$digest" > "$scratch/output"
grep -q '^PLAN ONLY:' "$scratch/output" || fail 'Plan did not complete'
! grep -q '^upgrade$' "$scratch/trace" || fail 'Plan changed a release'

# Historical Helm releases may lack the named isolation-policy read.
# Admit that exact permission without allowing broader RBAC changes.
cp "$scratch/current.yaml" "$scratch/baseline-current.yaml"
cp "$scratch/candidate.yaml" "$scratch/baseline-candidate.yaml"
cat > "$scratch/legacy-role.yaml" <<'YAML'
---
# Source: project-workspace/templates/worker.yaml
kind: Role
metadata:
  name: pwm-worker
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "delete"]
YAML
cat > "$scratch/scoped-role.yaml" <<'YAML'
---
# Source: project-workspace/templates/worker.yaml
kind: Role
metadata:
  name: pwm-worker
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["networkpolicies"]
    resourceNames: ["pwm-isolate-workloads"]
    verbs: ["get"]
YAML
cat "$scratch/legacy-role.yaml" "$scratch/baseline-current.yaml" > "$scratch/current.yaml"
cat "$scratch/scoped-role.yaml" "$scratch/baseline-candidate.yaml" > "$scratch/candidate.yaml"
bash "$script" plan "$release" "$digest" > "$scratch/output"
grep -q '^ADMISSION: scoped read' "$scratch/output" ||
  fail 'Named policy-read migration was refused'
sed -i 's/    verbs: \["get"\]$/    verbs: ["get", "list"]/' "$scratch/candidate.yaml"
if bash "$script" plan "$release" "$digest" > "$scratch/output" 2>&1; then
  fail 'Broader NetworkPolicy permissions were accepted'
fi
mv "$scratch/baseline-current.yaml" "$scratch/current.yaml"
mv "$scratch/baseline-candidate.yaml" "$scratch/candidate.yaml"

touch "$scratch/busy"
if bash "$script" plan "$release" "$digest" > "$scratch/output" 2>&1; then
  fail 'Active worker work was ignored'
fi
rm -f "$scratch/busy"
printf '{"items":[{"kind":"Pod"}]}\n' > "$scratch/resources.json"
if bash "$script" plan "$release" "$digest" > "$scratch/output" 2>&1; then
  fail 'Active namespace workload was ignored'
fi
printf '{"items":[]}\n' > "$scratch/resources.json"

cp "$scratch/candidate.yaml" "$scratch/valid.yaml"
printf 'unexpected: security-policy-drift\n' >> "$scratch/candidate.yaml"
if bash "$script" plan "$release" "$digest" > "$scratch/output" 2>&1; then
  fail 'Manifest drift was accepted'
fi
mv "$scratch/valid.yaml" "$scratch/candidate.yaml"

fleet="$root/services/compute/scripts/rollout-project-fleet.sh"
bash "$fleet" plan "$digest" > "$scratch/output"
grep -q '^PLAN ONLY: 1 releases inspected' "$scratch/output" ||
  fail 'Fleet planning did not inspect the candidate'
if bash "$fleet" apply "$digest" --confirm-count 2 > "$scratch/output" 2>&1; then
  fail 'Fleet accepted an unexpected candidate count'
fi
bash "$fleet" apply "$digest" --confirm-count 1 > "$scratch/output"
grep -q '^ACCEPTED: 1 project-worker Helm releases upgraded serially' "$scratch/output" ||
  fail 'Fleet did not apply the admitted release'
[[ ! -f "$scratch/paused" ]] || fail 'Fleet left the scheduler paused after successful upgrade'
rm -f "$scratch/applied"

bash "$script" apply "$release" "$digest" --confirm "$release" > "$scratch/output"
grep -q '^ACCEPTED:' "$scratch/output" || fail 'Approved worker did not finish'
[[ ! -f "$scratch/paused" ]] || fail 'Scheduler did not resume after successful upgrade'
grep -q '^upgrade$' "$scratch/trace" || fail 'Confirmed upgrade was not recorded'

# A Ready Deployment cannot be certified by a heartbeat recorded before
# the replacement worker became Ready.
rm -f "$scratch/applied"
jq --argjson previous "$(date +%s)" '.[0].last_seen = $previous' \
  "$scratch/workers.json" > "$scratch/stale-workers.json"
mv "$scratch/stale-workers.json" "$scratch/workers.json"
if bash "$script" apply "$release" "$digest" --confirm "$release" \
    > "$scratch/output" 2>&1; then
  fail 'Stale controller heartbeat was accepted'
fi
grep -q '^rollback$' "$scratch/trace" || fail 'Stale heartbeat did not trigger rollback'
[[ -f "$scratch/paused" ]] || fail 'Stale heartbeat incorrectly resumed scheduling'
[[ ! -f "$scratch/applied" ]] || fail 'Rollback did not restore the prior deployment'

# Simulate a completed, operator-confirmed recovery before testing a
# separate controller outage.
rm -f "$scratch/paused"
jq '.[0].last_seen = 4102444800' "$scratch/workers.json" > "$scratch/fresh-workers.json"
mv "$scratch/fresh-workers.json" "$scratch/workers.json"
touch "$scratch/api-down"
if bash "$script" apply "$release" "$digest" --confirm "$release" \
    > "$scratch/output" 2>&1; then
  fail 'Controller outage was accepted after upgrade'
fi
grep -q '^rollback$' "$scratch/trace" || fail 'Post-upgrade outage did not request rollback'
[[ -f "$scratch/paused" ]] || fail 'Failed migration incorrectly resumed scheduling'
[[ ! -f "$scratch/applied" ]] || fail 'Rollback did not restore the prior deployment'

printf 'Worker rollout admission and post-upgrade recovery verified\n'
