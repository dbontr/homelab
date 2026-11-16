#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
script="$root/../label-image-content.sh"
bash -n "$script"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
mkdir -p "$tmp"/{blobs,labels}
export MOCK_STATE="$tmp" CTR_BIN="$tmp/k3s"
reference=images.dbontr.internal/dbontr/acceptance:1.0
export MOCK_IMAGE="$reference"

save_blob() {
  local blob=$1 hash
  hash=$(printf %s "$blob" | sha256sum | awk '{print $1}')
  printf %s "$blob" > "$tmp/blobs/sha256:$hash"
  printf 'sha256:%s' "$hash"
}
layer=$(save_blob 'test compressed layer')
config=$(save_blob '{"architecture":"amd64","os":"linux"}')
manifest=$(save_blob "{\"schemaVersion\":2,\"config\":{\"digest\":\"$config\"},\"layers\":[{\"digest\":\"$layer\"}]}")
index=$(save_blob "{\"schemaVersion\":2,\"manifests\":[{\"digest\":\"$manifest\",\"platform\":{\"os\":\"linux\",\"architecture\":\"amd64\"}}]}")
export MOCK_INDEX="$index"

cat > "$tmp/k3s" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == ctr && $2 == -n && $3 == k8s.io ]] || exit 2
shift 3
case "$1 $2" in
  'images list')
    printf 'REF TYPE DIGEST SIZE\n%s application/vnd.oci.image.index.v1+json %s 1KiB\n' "$MOCK_IMAGE" "$MOCK_INDEX"
    ;;
  'content get')
    cat "$MOCK_STATE/blobs/$3"
    ;;
  'content ls')
    printf 'DIGEST SIZE AGE LABELS\n'
    for f in "$MOCK_STATE"/blobs/sha256:*; do
      d=$(basename "$f")
      printf '%s 1B 1m %s\n' "$d" "$(cat "$MOCK_STATE/labels/$d" 2>/dev/null || true)"
    done
    ;;
  'content label')
    d=$3
    [[ -f $MOCK_STATE/blobs/$d && $4 == containerd.io/distribution.source.images.dbontr.internal=* ]] || exit 2
    printf '%s\n' "$4" > "$MOCK_STATE/labels/$d"
    printf '%s\n' "$d" >> "$MOCK_STATE/writes"
    ;;
  *) printf 'Unexpected mock: %s\n' "$*" >&2; exit 2 ;;
esac
MOCK
chmod 0700 "$tmp/k3s"

if bash "$script" --check "$reference" >/dev/null 2>&1; then
  echo 'Missing source labels incorrectly passed read-only verification' >&2
  exit 1
fi
[[ ! -e "$tmp/writes" ]] || { echo 'Check mutated containerd state' >&2; exit 1; }
for args in '--apply other/reference --confirm other/reference' '--apply images.dbontr.internal/dbontr/acceptance:1.0' '--check https://malicious.invalid/foo:1'; do
  read -r -a words <<< "$args"
  if bash "$script" "${words[@]}" >/dev/null 2>&1; then
    printf 'Invalid invocation accepted: %s\n' "$args" >&2; exit 1
  fi
done

printf 'containerd.io/distribution.source.images.dbontr.internal=../../invalid\n' > "$tmp/labels/$config"
if bash "$script" --apply "$reference" --confirm "$reference" >/dev/null 2>&1; then
  echo 'Malformed shared source was accepted' >&2; exit 1
fi
[[ ! -e "$tmp/writes" ]] || { echo 'Invalid label had partial writes' >&2; exit 1; }
printf 'containerd.io/distribution.source.images.dbontr.internal=other/repository\n' > "$tmp/labels/$config"
printf %s 'CORRUPTED' >> "$tmp/blobs/$layer"
if bash "$script" --apply "$reference" --confirm "$reference" >/dev/null 2>&1; then
  echo 'Corrupt blob was accepted' >&2; exit 1
fi
[[ ! -e "$tmp/writes" ]] || { echo 'Corrupt input had partial writes' >&2; exit 1; }
printf %s 'test compressed layer' > "$tmp/blobs/$layer"
bash "$script" --apply "$reference" --confirm "$reference" > "$tmp/applied"
[[ $(wc -l < "$tmp/writes") == 4 ]] || { echo 'Not all content descriptors were labeled' >&2; exit 1; }
[[ $(cat "$tmp/labels/$config") == 'containerd.io/distribution.source.images.dbontr.internal=other/repository,dbontr/acceptance' ]] || {
  echo 'Shared OCI digest lost the original repository source' >&2; exit 1;
}
bash "$script" --check "$reference" > "$tmp/checked"
bash "$script" --apply "$reference" --confirm "$reference" >/dev/null
[[ $(wc -l < "$tmp/writes") == 4 ]] || { echo 'Idempotent repair wrote additional metadata' >&2; exit 1; }
if grep -v '^SOURCE_COMPLETE' "$tmp/checked"; then
  echo 'Completed image still has missing source labels' >&2; exit 1
fi
# Immutable references must resolve to their declared content index.
export MOCK_IMAGE="images.dbontr.internal/dbontr/acceptance@$index"
bash "$script" --check "$MOCK_IMAGE" > "$tmp/immutable-checked"
grep -q '^SOURCE_COMPLETE ' "$tmp/immutable-checked"
bash "$script" --apply "$MOCK_IMAGE" --confirm "$MOCK_IMAGE" >/dev/null
[[ $(wc -l < "$tmp/writes") == 4 ]] || {
  echo 'Immutable recheck performed unnecessary writes' >&2; exit 1;
}
if bash "$script" --check "images.dbontr.internal/dbontr/acceptance@sha256:$(printf 'f%.0s' {1..64})" >/dev/null 2>&1; then
  echo 'Mismatched digest-qualified image reference was accepted' >&2
  exit 1
fi
printf '%s\n' 'Peer image content label verification, corruption refusal, conflict refusal, and repair tests passed.'
