#!/usr/bin/env bash
set -euo pipefail

# Source-only Helm acceptance. Never talks to a Kubernetes API or reads Secrets.
chart="${1:-services/compute/deploy/helm/project-workspace}"
image="ghcr.io/dbontr/homelab/controller@sha256:$(printf '%064d' 0)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

args=(
  --set controller.enabled=true
  --set-string "controller.image=$image"
  --set-string controller.backupInterval=24h
  --set-string controller.offsite.repository='sftp:compute-backup@10.77.30.232:/repository'
  --set-string controller.offsite.passwordSecretName=synthetic-password
  --set-string controller.offsite.sshSecretName=synthetic-ssh
)

# A backup with neither an HTTPS alert receiver nor credential must be refused.
if helm template verify "$chart" "${args[@]}" >"$scratch/missing-alert.yaml" 2>"$scratch/error"; then
  echo "ERROR: backup enabled without protected notification gate" >&2
  exit 1
fi
grep -q 'protected HTTPS alert receiver' "$scratch/error"

if helm template verify "$chart" "${args[@]}" \
  --set-string controller.offsite.alertURL=http://unprotected.example.invalid/hook \
  --set-string controller.offsite.alertSecretName=synthetic-alert \
  >"$scratch/insecure.yaml" 2>"$scratch/error"; then
  echo "ERROR: plaintext notification receiver accepted" >&2
  exit 1
fi
grep -q 'must use HTTPS' "$scratch/error"

helm template verify "$chart" "${args[@]}" \
  --set-string controller.offsite.alertURL=https://alerts.example.invalid/hook \
  --set-string controller.offsite.alertSecretName=synthetic-alert \
  >"$scratch/rendered.yaml"

grep -q 'name: PWM_RESTIC_BACKUP_INTERVAL' "$scratch/rendered.yaml"
grep -q 'name: PWM_RESTIC_SFTP_SSH_CONFIG' "$scratch/rendered.yaml"
grep -q 'value: /tmp/.ssh/config' "$scratch/rendered.yaml"
grep -q 'name: PWM_RESTIC_ALERT_URL' "$scratch/rendered.yaml"
grep -q 'name: PWM_RESTIC_ALERT_TOKEN_FILE' "$scratch/rendered.yaml"
grep -q 'secretName: synthetic-alert' "$scratch/rendered.yaml"
grep -q 'name: restic-ssh-input' "$scratch/rendered.yaml"
grep -q 'name: restic-alert-input' "$scratch/rendered.yaml"
grep -q 'name: restic-alert' "$scratch/rendered.yaml"
grep -q 'name: pwm-controller-state' "$scratch/rendered.yaml"
echo 'PASS: protected-backup Helm admission, mounts, and preserved PVC'
