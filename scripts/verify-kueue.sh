#!/usr/bin/env bash
set -euo pipefail

case "${1:---check}" in
  --check|--smoke) mode=${1:---check} ;;
  *) echo 'Usage: verify-kueue.sh [--check|--smoke]' >&2; exit 2 ;;
esac
(( $# <= 1 )) || { echo 'Unexpected arguments' >&2; exit 2; }

if [[ -n "${KUBECTL_BIN:-}" ]]; then
  if [[ ${KUBECTL_BIN##*/} == k3s ]]; then
    kube=("$KUBECTL_BIN" kubectl)
  else
    kube=("$KUBECTL_BIN")
  fi
elif command -v k3s >/dev/null 2>&1; then
  kube=(k3s kubectl)
else
  kube=(kubectl)
fi
command -v jq >/dev/null || { echo 'jq is required' >&2; exit 1; }
"${kube[@]}" get --raw=/readyz >/dev/null
"${kube[@]}" -n kueue-system get deployment kueue-controller-manager -o json |
  jq -e '.status.availableReplicas >= 1 and .status.observedGeneration == .metadata.generation' >/dev/null
"${kube[@]}" get crd workloads.kueue.x-k8s.io >/dev/null
"${kube[@]}" get namespace pwm-batch -o json |
  jq -e '.metadata.labels["homelab.dbontr.me/kueue"] == "enabled" and
         .metadata.labels["pod-security.kubernetes.io/enforce"] == "restricted"' >/dev/null
"${kube[@]}" get clusterqueue pwm-cpu -o json |
  jq -e 'any(.status.conditions[]?; .type == "Active" and .status == "True")' >/dev/null
"${kube[@]}" -n pwm-batch get localqueue research-cpu -o json |
  jq -e 'any(.status.conditions[]?; .type == "Active" and .status == "True")' >/dev/null
"${kube[@]}" -n kueue-system get configmap kueue-manager-config -o json |
  jq -e '.data["controller_manager_config.yaml"] | contains("manageJobsWithoutQueueName: false") and
         contains("homelab.dbontr.me/kueue: enabled")' >/dev/null
"${kube[@]}" get nodes -o json |
  jq -e '([.items[] | select(any(.status.conditions[]; .type == "Ready" and .status == "True"))] | length) == 5' >/dev/null
echo 'Kueue controller, namespace, queue policy, and five-node cluster healthy'

if [[ "$mode" == --check ]]; then exit 0; fi

manifest=$(mktemp)
job=''
cleanup() {
  if [[ -n "$job" ]]; then
    "${kube[@]}" -n pwm-batch delete job "$job" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
  rm -f -- "$manifest"
}
trap cleanup EXIT

cat > "$manifest" <<'YAML'
apiVersion: batch/v1
kind: Job
metadata:
  generateName: homelab-kueue-acceptance-
  namespace: pwm-batch
  labels:
    kueue.x-k8s.io/queue-name: research-cpu
spec:
  activeDeadlineSeconds: 180
  ttlSecondsAfterFinished: 300
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      securityContext:
        runAsUser: 1000
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: acceptance
          image: docker.io/library/alpine:3.20@sha256:c64c687cbea9300178b30c95835354e34c4efebc4badfe27102879de0483b5e
          command: ["/bin/sh", "-c", "printf 'kueue-admission-ok'"]
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 64Mi
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
YAML

job=$("${kube[@]}" -n pwm-batch create -f "$manifest" -o jsonpath='{.metadata.name}')
[[ "$job" == homelab-kueue-acceptance-* ]] || {
  echo 'Unexpected acceptance Job name' >&2; exit 1;
}
"${kube[@]}" -n pwm-batch wait "job/$job" --for=condition=complete --timeout=240s
uid=$("${kube[@]}" -n pwm-batch get job "$job" -o jsonpath='{.metadata.uid}')
"${kube[@]}" -n pwm-batch get workloads -o json |
  jq -e --arg uid "$uid" '
    any(.items[];
      (any(.metadata.ownerReferences[]?; .uid == $uid)) and
      (any(.status.conditions[]?; .type == "Admitted" and .status == "True")))
  ' >/dev/null
echo 'Kueue admitted and completed an isolated Kubernetes Job'
