#!/usr/bin/env bash
set -euo pipefail

# Only local SQLite fixtures, fake Kubernetes HTTP servers, and mocked SSH
# commands are allowed in this suite. No production cluster access is needed.
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"
command -v go >/dev/null || { echo 'Go toolchain required' >&2; exit 2; }
command -v pkg-config >/dev/null || { echo 'pkg-config required' >&2; exit 2; }
pkg-config --exists sqlite3 || { echo 'SQLite development package required' >&2; exit 2; }
command -v jq >/dev/null || { echo 'jq required' >&2; exit 2; }

# The Go fixtures use dedicated temporary SQLite files and httptest TLS.
# Keep accidental inherited cluster credentials unavailable to child tests.
unset KUBECONFIG PWM_KUEUE_ENABLED PWM_KUEUE_TOKEN_FILE PWM_KUEUE_CA_FILE
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
go test -count=1 -timeout=180s \
  -run '^(TestKueue|TestBatchSubmissionIdempotenceAndWorkerIsolation|TestTaskHardware|TestFailedTaskHardware|TestInterruptedWorkerReconciliationFencesOldClaims)' \
  ./internal/controller

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
