#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/verify-cold-peer-pull.sh"
bash -n "$script"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT

export MOCK_STATE="$tmp" MOCK_REF='images.dbontr.internal/dbontr/acceptance:cold'
export MOCK_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
export MOCK_MANIFEST="sha256:$(printf 'b%.0s' {1..64})"
export MOCK_LAYER1="sha256:$(printf 'c%.0s' {1..64})"
export MOCK_LAYER2="sha256:$(printf 'd%.0s' {1..64})"
export KUBECTL_BIN="$tmp/kubectl" PATH="$tmp:$PATH"

cat > "$tmp/ssh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
while [[ $1 == -o ]]; do shift 2; done
host=$1
shift
cmd="$*"
printf '%s %s\n' "$host" "$cmd" >> "$MOCK_STATE/calls"
case "$cmd" in
  'k3s ctr -n k8s.io images ls')
    echo 'REF TYPE DIGEST SIZE'
    if [[ $host == venus || -f "$MOCK_STATE/pulled" ]]; then
      printf '%s OCI %s 1KiB\n' "$MOCK_REF" "$MOCK_DIGEST"
    fi ;;
  'k3s ctr -n k8s.io images check')
    echo 'REF TYPE DIGEST STATUS'
    if [[ $host == venus || -f "$MOCK_STATE/pulled" ]]; then
      printf '%s OCI %s complete\n' "$MOCK_REF" "$MOCK_DIGEST"
    fi ;;
  'k3s ctr -n k8s.io images ls -q')
    if [[ $host == janus && -f "$MOCK_STATE/pulled" ]]; then
      printf '%s\n' "$MOCK_REF"
    fi ;;
  'k3s ctr -n k8s.io content ls -q')
    echo "$MOCK_LAYER1"
    if [[ $host == janus && -f "$MOCK_STATE/pulled" ]]; then
      echo "$MOCK_LAYER2"
    fi ;;
  "k3s ctr -n k8s.io content get '$MOCK_DIGEST'")
    [[ $host == venus ]] || exit 2
    if [[ ${MOCK_DUPLICATE_MANIFEST:-0} == 1 ]]; then
      printf '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"},"digest":"%s"},{"platform":{"os":"linux","architecture":"amd64"},"digest":"%s"}]}\n' "$MOCK_MANIFEST" "$MOCK_MANIFEST"
    else
      printf '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"},"digest":"%s"}]}\n' "$MOCK_MANIFEST"
    fi ;;
  "k3s ctr -n k8s.io content get '$MOCK_MANIFEST'")
    [[ $host == venus ]] || exit 2
    printf '{"layers":[{"digest":"%s"},{"digest":"%s"}]}\n' "$MOCK_LAYER1" "$MOCK_LAYER2" ;;
  *'k3s crictl pull '*)
    [[ $host == janus ]] || exit 2
    [[ ${MOCK_FAIL_PULL:-0} == 0 ]] || exit 1
    touch "$MOCK_STATE/pulled"
    echo pulled ;;
  *'k3s crictl inspecti '*)
    [[ $host == janus && -f "$MOCK_STATE/pulled" ]] ;;
  *) echo "Unexpected SSH command: $cmd" >&2; exit 2 ;;
esac
MOCK
chmod 0700 "$tmp/ssh"

cat > "$tmp/kubectl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == get && $2 == node ]]; then
  [[ $3 == venus || $3 == janus ]] || exit 2
  echo True
elif [[ $1 == get && $2 == --raw ]]; then
  node=$3
  [[ $node == /api/v1/nodes/venus/proxy/metrics ||
     $node == /api/v1/nodes/janus/proxy/metrics ]] || exit 2
  hit=0
  if [[ -f "$MOCK_STATE/pulled" ]]; then
    if [[ $node == *janus* && ${MOCK_ONLY_DONOR_HIT:-0} != 1 ]]; then hit=1; fi
    if [[ $node == *venus* && ${MOCK_ONLY_DONOR_HIT:-0} == 1 ]]; then hit=1; fi
  fi
  echo 'spegel_advertised_image_digests{registry="images.dbontr.internal"} 2'
  printf 'spegel_mirror_requests_total{registry="images.dbontr.internal",cache="hit"} %s\n' "$hit"
  echo 'spegel_mirror_requests_total{registry="images.dbontr.internal",cache="miss"} 0'
else
  echo "Unexpected kubectl command: $*" >&2
  exit 2
fi
MOCK
chmod 0700 "$tmp/kubectl"

if bash "$script" 'venus;touch /tmp/unsafe' janus "$MOCK_REF" "$MOCK_DIGEST" >/dev/null 2>&1; then
  echo 'Unsafe SSH node input accepted' >&2; exit 1
fi
[[ ! -e "$tmp/calls" ]] || {
  echo 'Invalid node caused remote activity' >&2; exit 1;
}
if MOCK_FAIL_PULL=1 bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" \
    > "$tmp/failed" 2>&1; then
  echo 'Failed cold peer pull accepted' >&2; exit 1
fi
if grep -q '^PASS:' "$tmp/failed"; then
  echo 'Failed peer pull emitted successful receipt' >&2; exit 1
fi
[[ ! -f "$tmp/pulled" ]] || {
  echo 'Failed peer pull changed receiver state' >&2; exit 1;
}
if MOCK_DUPLICATE_MANIFEST=1 bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/ambiguous" 2>&1; then
  echo 'Multiple Linux amd64 manifests were accepted' >&2; exit 1
fi
[[ ! -f "$tmp/pulled" ]] || {
  echo 'Ambiguous image index caused a remote pull' >&2; exit 1;
}
if MOCK_ONLY_DONOR_HIT=1 bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/donor_only" 2>&1; then
  echo 'Donor-only peer cache hit was misattributed to the receiver' >&2; exit 1
fi
if grep -q '^PASS:' "$tmp/donor_only"; then
  echo 'Donor-only cache hit emitted a successful receipt' >&2; exit 1
fi
rm -f "$tmp/pulled"
bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" > "$tmp/accepted"
grep -q '^PASS: cold OCI layer pull, digest, CRI availability and new peer-cache hit verified$' "$tmp/accepted"
grep -Fxq "PREVIOUSLY_MISSING_LAYER=$MOCK_LAYER2" "$tmp/accepted"
[[ -f "$tmp/pulled" ]]
if bash "$script" venus janus "$MOCK_REF" "$MOCK_DIGEST" >/dev/null 2>&1; then
  echo 'Cached receiver was mislabeled as an uncached pull' >&2; exit 1
fi
echo 'Cold OCI acceptance verifies an uncached layer, a complete digest, CRI visibility and a new mirror cache hit.'
