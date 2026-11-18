#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/recover-oci-peer.sh"
bash -n "$script"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
export MOCK_STATE="$tmp" MOCK_REF='images.dbontr.internal/dbontr/acceptance:alpine321'
export MOCK_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
export PATH="$tmp:$PATH"

cat > "$tmp/ssh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == -o && "${2:-}" == BatchMode=yes ]] || exit 2
while [[ "${1:-}" == -o ]]; do shift 2; done
host=$1
shift
command="$*"
case "$command" in
  'k3s ctr -n k8s.io images ls')
    echo 'REF TYPE DIGEST SIZE'
    if [[ "$host" == venus ]]; then
      printf '%s OCI %s 1KiB\n' "$MOCK_REF" "$MOCK_DIGEST"
    elif [[ -f "$MOCK_STATE/imported" ]]; then
      printf '%s OCI %s 1KiB\n' "$MOCK_REF" "${MOCK_RECEIVER_DIGEST:-$MOCK_DIGEST}"
    fi
    ;;
  'k3s ctr -n k8s.io images check')
    echo 'REF TYPE DIGEST STATUS'
    if [[ "$host" == venus || -f "$MOCK_STATE/imported" ]]; then
      printf '%s OCI %s complete\n' "$MOCK_REF" "$MOCK_DIGEST"
    fi
    ;;
  *'images export - '*)
    [[ "$host" == venus ]] || exit 2
    echo export >> "$MOCK_STATE/calls"
    printf 'verified-image-content'
    ;;
  'k3s ctr -n k8s.io images import -')
    [[ "$host" == janus ]] || exit 2
    [[ $(cat) == verified-image-content ]] || exit 1
    [[ ${MOCK_IMPORT_FAIL:-0} == 0 ]] || exit 1
    touch "$MOCK_STATE/imported"
    echo import >> "$MOCK_STATE/calls"
    ;;
  *'bash -s -- --apply '*)
    [[ "$host" == janus ]] || exit 2
    cat >/dev/null
    echo label-apply >> "$MOCK_STATE/calls"
    [[ "${MOCK_LABEL_FAIL:-0}" == 0 ]] || exit 1
    touch "$MOCK_STATE/labeled"
    echo 'SOURCE_COMPLETE (4 descriptors)'
    ;;
  *'bash -s -- --check '*)
    [[ "$host" == janus ]] || exit 2
    cat >/dev/null
    echo label-check >> "$MOCK_STATE/calls"
    [[ -f "$MOCK_STATE/labeled" ]] || exit 1
    echo 'SOURCE_COMPLETE (4 descriptors)'
    ;;
  *) printf 'Unexpected SSH command: %s\n' "$command" >&2; exit 2 ;;
esac
MOCK
chmod 0700 "$tmp/ssh"

if bash "$script" venus janus "$MOCK_REF" sha256:invalid >/dev/null 2>&1; then
  echo 'Invalid digest was accepted' >&2; exit 1
fi
if [[ -e "$tmp/calls" ]]; then
  echo 'Invalid input caused remote activity' >&2; exit 1
fi

if bash "$script" 'venus;touch /tmp/unsafe' janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/invalid-host" 2>&1; then
  echo 'Unknown recovery host was accepted' >&2; exit 1
fi
if bash "$script" venus venus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/identical-hosts" 2>&1; then
  echo 'Source and receiver cannot be the same host' >&2; exit 1
fi
if MOCK_IMPORT_FAIL=1 bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/import-fail" 2>&1; then
  echo 'Incomplete image import was accepted' >&2; exit 1
fi
if [[ -f "$tmp/imported" ]] || grep -q '^PASS:' "$tmp/import-fail"; then
  echo 'Interrupted import produced a complete receipt' >&2; exit 1
fi

bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/first"
grep -q '^PASS: peer image transfer, digest integrity, and source metadata verified$' "$tmp/first"
[[ $(grep -c '^import$' "$tmp/calls") == 1 ]]
[[ $(grep -c '^label-apply$' "$tmp/calls") == 1 ]]
[[ $(grep -c '^label-check$' "$tmp/calls") == 1 ]]

bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/repeat"
grep -q '^PASS: complete peer image and source metadata verified$' "$tmp/repeat"
[[ $(grep -c '^import$' "$tmp/calls") == 1 ]] || {
  echo 'Idempotent recovery repeated an import' >&2; exit 1;
}
[[ $(grep -c '^label-check$' "$tmp/calls") == 2 ]] || {
  echo 'Existing image skipped source verification' >&2; exit 1;
}

if MOCK_RECEIVER_DIGEST="sha256:$(printf 'b%.0s' {1..64})" \
  bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" >/dev/null 2>&1; then
  echo 'Conflicting receiver digest was accepted' >&2; exit 1
fi
if MOCK_LABEL_FAIL=1 bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/fail" 2>&1; then
  echo 'Metadata repair failure was reported as success' >&2; exit 1
fi
if grep -q '^PASS:' "$tmp/fail"; then
  echo 'Metadata failure emitted success receipt' >&2; exit 1
fi

export MOCK_REF="images.dbontr.internal/dbontr/acceptance@$MOCK_DIGEST"
bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/immutable"
grep -q '^PASS: complete peer image and source metadata verified$' "$tmp/immutable"
echo 'OCI peer recovery verifies source labels after import and on idempotent rechecks.'
