#!/usr/bin/env bash
set -euo pipefail

# Only local SQLite fixtures, fake Kubernetes HTTP servers, and mocked SSH
# commands are allowed in this suite. No production cluster access is needed.
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"
command -v go >/dev/null || { echo 'Go toolchain required' >&2; exit 2; }
required_go=$(awk '$1 == "go" {print $2; exit}' go.mod)
installed_go=$(GOTOOLCHAIN=local go version | awk '{sub(/^go/, "", $3); print $3}')
[[ -n $required_go && $required_go == "$installed_go" ]] || {
  printf 'UNSUPPORTED: Go %s required, local Go %s found\n' "$required_go" "$installed_go" >&2
  exit 77
}
repeats=${COMPUTE_RESILIENCE_REPEATS:-1}
[[ $repeats =~ ^([1-9]|10)$ ]] || {
  echo 'COMPUTE_RESILIENCE_REPEATS must be between 1 and 10' >&2
  exit 2
}
race_flags=()
if (( repeats > 1 )); then
  race_flags=(-race -parallel=2 -p=1)
  export GOMAXPROCS=2
fi
command -v pkg-config >/dev/null || { echo 'pkg-config required' >&2; exit 2; }
pkg-config --exists sqlite3 || { echo 'SQLite development package required' >&2; exit 2; }
command -v jq >/dev/null || { echo 'jq required' >&2; exit 2; }

# The Go fixtures use dedicated temporary SQLite files and httptest TLS.
# Keep accidental inherited cluster credentials unavailable to child tests.
unset KUBECONFIG PWM_KUEUE_ENABLED PWM_KUEUE_TOKEN_FILE PWM_KUEUE_CA_FILE
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
go test "${race_flags[@]}" -count="$repeats" -timeout="$((180 * repeats))s" \
  -run '^(TestKueue|TestBatchSubmissionIdempotenceAndWorkerIsolation|TestTaskHardware|TestFailedTaskHardware|TestInterruptedWorkerReconciliationFencesOldClaims)' \
  ./internal/controller
# Standalone standard-library OCI archive verification and corruption cases.
go test -count=1 -timeout=180s ./cmd/oci-archive-verify

for test_script in \
  scripts/tests/recover-oci-peer-test.sh \
  scripts/tests/verify-cold-peer-pull-test.sh \
  scripts/tests/verify-registry-mirror-test.sh \
  scripts/tests/copy-peer-image-test.sh \
  scripts/tests/label-image-content-test.sh
do
  bash "$test_script"
done
printf 'PASS: local Compute resilience and OCI failure-injection suite completed\n'
