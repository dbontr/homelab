#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
mode=${1:---plan}
if [[ $# -gt 1 ]] || [[ "$mode" != --plan && "$mode" != --run ]]; then
  printf 'Usage: %s [--plan|--run]\n' "$0" >&2
  exit 2
fi

if [[ "$mode" == --plan ]]; then
  cat <<'PLAN'
Offline integration checks (source and test fixtures only):
  1. Repository-wide Go, Rust, Helm and Nix acceptance
  2. Release manifest, staging and ingress-contract tests
  3. Rust security and executor contract tests
  4. Encrypted Restic backup and exact-snapshot restore test
  5. Kueue and cold OCI recovery fixture tests
  6. Project-catalog preservation and revision tests
Live ingress, host recovery, real Kueue outages, off-host backup and
owner-approved rollback require separate evidence and authorization.
PLAN
  exit 0
fi

# This is a safety precheck, not proof of isolation. Run in an offline,
# unprivileged, disposable VM/container with no production mounts or routes.
if [[ ${HOMELAB_ACCEPTANCE_ISOLATED:-} != 1 ]] || [[ $(id -u) -eq 0 ]] ||
   [[ $(hostname -s) == saturn ]] || [[ -e /etc/rancher/k3s/k3s.yaml ]] ||
   [[ -n ${KUBECONFIG:-} ]] || [[ -n ${RESTIC_REPOSITORY:-} ]] ||
   [[ -n ${PWM_RESTIC_REPOSITORY:-} ]] || [[ -n ${CLOUDFLARE_API_TOKEN:-} ]]; then
  printf 'Refusing to run without a non-root, offline isolated environment.\n' >&2
  exit 3
fi
if [[ -n $(git -C "$root" status --porcelain) ]]; then
  printf 'Refusing a source tree with uncommitted changes.\n' >&2
  exit 3
fi
sha=$(git -C "$root" rev-parse --verify HEAD)
[[ $sha =~ ^[a-f0-9]{40}$ ]] || { printf 'Invalid Git source revision.\n' >&2; exit 3; }

export GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
export GOMAXPROCS=2 CARGO_BUILD_JOBS=2
failures=0
run() {
  local label=$1
  shift
  printf 'CHECK %s at %s\n' "$label" "$sha" >&2
  if "$@" >&2; then
    printf '{"check":"%s","result":"passed","source_sha":"%s"}\n' "$label" "$sha"
  else
    printf '{"check":"%s","result":"failed","source_sha":"%s"}\n' "$label" "$sha"
    failures=$((failures + 1))
  fi
}

run full_offline bash "$root/scripts/verify-offline-nix.sh" --all
run protected_release bash "$root/scripts/tests/verify-release-test.sh"
run release_staging bash "$root/scripts/tests/stage-component-test.sh"
run rust_authority bash -c 'cd "$1/services/security-authority" && cargo test --locked --offline --all-targets' _ "$root"
run rust_executor bash -c 'cd "$1/services/executor" && cargo test --locked --offline --all-targets' _ "$root"
run encrypted_recovery bash -c 'command -v restic >/dev/null && cd "$1/services/compute" && go test -count=1 ./internal/controller -run "^TestEncryptedOffsiteBackupRestoreAndVerification$"' _ "$root"
run kueue_policy bash "$root/scripts/tests/kueue-policy-test.sh"
run cold_oci_fixture bash "$root/services/compute/scripts/tests/verify-cold-peer-pull-test.sh"
run project_catalog bash -c 'cd "$1/services/compute" && go test -count=1 ./internal/controller -run "^TestProjectCatalogVisibilityIsReversibleAndNonDestructive$"' _ "$root"
run compute_batch bash -c 'cd "$1/services/compute" && go test -count=1 ./internal/controller -run "^TestBatchSubmissionIdempotenceAndWorkerIsolation$"' _ "$root"

run executor_format bash -c 'cd "$1/services/executor" && cargo fmt --all -- --check' _ "$root"
run executor_clippy bash -c 'cd "$1/services/executor" && cargo clippy --locked --offline --all-targets -- -D warnings' _ "$root"
run protected_backup_helm bash "$root/services/compute/scripts/tests/compute-backup-chart-test.sh"
run helm_manifest_compat bash "$root/scripts/tests/production-manifest-compat-test.sh" "53c40842cc7354deb29ce64608dcb4bd0794a6fa"
run encrypted_backup_scheduler bash -c 'cd "$1/services/compute" && go test -count=1 ./cmd/project-workspace-controller -run "TestBackup|TestScheduledOffsite"' _ "$root"
run compute_resilience bash "$root/services/compute/scripts/tests/compute-resilience-isolated.sh"
run legacy_project_reconcile bash -c 'cd "$1/services/compute" && go test -race -count=1 ./cmd/legacy-reconcile' _ "$root"
run monitor_ingress_contract bash "$root/scripts/tests/accept-monitor-ingress-test.sh"
run five_host_evaluation bash -c 'cd "$1"; for host in saturn jupiter venus mercury janus; do nix eval --offline --raw ".#nixosConfigurations.$host.config.system.build.toplevel.drvPath" >/dev/null || exit 1; done' _ "$root"

if (( failures )); then
  printf '%d offline integration checks failed; no deployment was attempted.\n' "$failures" >&2
  exit 1
fi
printf 'Offline source and fixture gates passed at %s. Live acceptance is separate.\n' "$sha" >&2
