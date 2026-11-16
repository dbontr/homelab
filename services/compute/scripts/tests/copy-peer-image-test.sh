#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/copy-peer-image.sh"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
export MOCK_STATE="$tmp" KUBECTL_BIN="$tmp/kubectl"
export PATH="$tmp:$PATH"
image="images.dbontr.internal/dbontr/controller@sha256:$(printf 'a%.0s' {1..64})"
digest="sha256:$(printf 'a%.0s' {1..64})"

cat > "$KUBECTL_BIN" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == get && $2 == node && $4 == -o && $5 == json ]] || exit 1
case "$3" in venus) ip=10.77.30.230 ;; mercury) ip=10.77.30.231 ;; *) exit 1 ;; esac
jq -nc --arg ip "$ip" '{status:{conditions:[{type:"Ready",status:"True"}],addresses:[{type:"InternalIP",address:$ip}]}}'
MOCK

cat > "$tmp/ssh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
for arg in "$@"; do
  case "$arg" in root@10.77.30.230) node=source ;; root@10.77.30.231) node=destination ;; esac
done
args="$*"
printf '%s\n' "$args" >> "$MOCK_STATE/ssh_calls"
if [[ ${MOCK_SSH_DENY:-0} == 1 ]]; then exit 255; fi
if [[ "$args" == *' true' ]]; then exit 0; fi
if [[ "$args" == *'images ls | awk'* ]]; then
  if [[ "$node" == source ]]; then
    echo "${MOCK_SOURCE_DIGEST:-sha256:$(printf 'a%.0s' {1..64})}"
  elif [[ -f "$MOCK_STATE/imported" ]]; then
    echo "sha256:$(printf 'a%.0s' {1..64})"
  else
    printf '%s' "${MOCK_DEST_DIGEST:-}"
  fi
elif [[ "$args" == *'images check | awk'* ]]; then
  exit 0
elif [[ "$args" == *'images export -'* ]]; then
  printf 'verified OCI image content'
elif [[ "$args" == *'images import --digests -'* ]]; then
  cat > "$MOCK_STATE/imported_bytes"
  cmp -s "$MOCK_STATE/imported_bytes" "$MOCK_STATE/expected_bytes"
  touch "$MOCK_STATE/imported"
else
  echo "Unexpected SSH invocation: $args" >&2
  exit 1
fi
MOCK
# Escaped interpolation in the outer source template must not persist in fixture.
chmod 0700 "$tmp/ssh" "$KUBECTL_BIN"
printf 'verified OCI image content' > "$tmp/expected_bytes"
bash -n "$script"

for invalid in \
  "venus venus $image --confirm venus" \
  "venus mercury images.dbontr.internal/dbontr/controller:latest --confirm mercury" \
  "venus mercury $image --confirm venus" \
  "unknown mercury $image --confirm mercury"; do
  if bash "$script" $invalid >/dev/null 2>&1; then
    echo "Accepted unsafe image-copy arguments: $invalid" >&2
    exit 1
  fi
done
[[ ! -e "$tmp/ssh_calls" ]]

if MOCK_SSH_DENY=1 bash "$script" venus mercury "$image" --confirm mercury >/dev/null 2>&1; then
  echo 'Accepted unauthorized peer session' >&2
  exit 1
fi
if MOCK_SOURCE_DIGEST="sha256:$(printf 'b%.0s' {1..64})" \
    bash "$script" venus mercury "$image" --confirm mercury >/dev/null 2>&1; then
  echo 'Accepted mismatched source manifest digest' >&2
  exit 1
fi
if MOCK_DEST_DIGEST="sha256:$(printf 'b%.0s' {1..64})" \
    bash "$script" venus mercury "$image" --confirm mercury >/dev/null 2>&1; then
  echo 'Overwrote a conflicting destination image' >&2
  exit 1
fi
[[ ! -e "$tmp/imported" ]]

bash "$script" venus mercury "$image" --confirm mercury > "$tmp/result.json"
jq -e --arg expected "$digest" '
  .source == "venus" and .destination == "mercury" and
  .manifest_digest == $expected and .result == "copied" and
  .content == "complete"
' "$tmp/result.json" >/dev/null
cmp -s "$tmp/imported_bytes" "$tmp/expected_bytes"
bash "$script" venus mercury "$image" --confirm mercury > "$tmp/repeat.json"
jq -e '.result == "already_present"' "$tmp/repeat.json" >/dev/null
[[ $(grep -c 'images import --digests -' "$tmp/ssh_calls") == 1 ]]
echo 'Guarded peer image-copy preflight, digest, conflict, and idempotency checks passed.'
