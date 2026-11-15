#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/peer-image-offline-transfer.sh"
bash -n "$script"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
mkdir -p "$tmp/bin"
touch "$tmp/key"
chmod 0600 "$tmp/key"
export PEER_SSH_IDENTITY="$tmp/key" MOCK_STATE="$tmp" 
export MOCK_IMAGE=images.dbontr.internal/dbontr/compute-gateway:0.1.11
export MOCK_SOURCE_DIGEST=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export MOCK_TARGET_DIGEST=''
export MOCK_SOURCE_COMPLETE=yes MOCK_POD_IN_USE=no MOCK_IMPORT_MISMATCH=no

cat > "$tmp/bin/kubectl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == get && $2 == node ]]; then
  printf 'True'
elif [[ $1 == get && $2 == pods ]]; then
  if [[ $MOCK_POD_IN_USE == yes ]]; then
    printf '{"items":[{"spec":{"containers":[{"image":"%s"}]}}]}' "$MOCK_IMAGE"
  else
    printf '{"items":[]}'
  fi
else
  echo 'Unexpected kubectl mock request' >&2
  exit 90
fi
MOCK

cat > "$tmp/bin/ssh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
while (( $# )); do
  case "$1" in
    root@*) host=$1; shift; break ;;
    *) shift ;;
  esac
done
cmd=${1:-}
case "$cmd" in
  'systemctl is-active --quiet k3s.service')
    exit 0
    ;;
  'k3s ctr -n k8s.io images list')
    printf 'REF TYPE DIGEST SIZE\n'
    if [[ $host == root@10.77.30.230 ]]; then
      printf '%s OCI %s 1KiB\n' "$MOCK_IMAGE" "$MOCK_SOURCE_DIGEST"
    elif [[ -f "$MOCK_STATE/imported" ]]; then
      if [[ $MOCK_IMPORT_MISMATCH == yes ]]; then
        digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      else
        digest=$MOCK_SOURCE_DIGEST
      fi
      printf '%s OCI %s 1KiB\n' "$MOCK_IMAGE" "$digest"
    elif [[ -n $MOCK_TARGET_DIGEST ]]; then
      printf '%s OCI %s 1KiB\n' "$MOCK_IMAGE" "$MOCK_TARGET_DIGEST"
    fi
    ;;
  'k3s ctr -n k8s.io images check')
    printf 'REF TYPE STATUS\n'
    if [[ $host == root@10.77.30.230 ]]; then
      if [[ $MOCK_SOURCE_COMPLETE == yes ]]; then
        printf '%s OCI complete (8/8)\n' "$MOCK_IMAGE"
      else
        printf '%s OCI incomplete (5/8)\n' "$MOCK_IMAGE"
      fi
    elif [[ -f "$MOCK_STATE/imported" ]]; then
      printf '%s OCI complete (8/8)\n' "$MOCK_IMAGE"
    fi
    ;;
  "k3s ctr -n k8s.io images export - '$MOCK_IMAGE'")
    printf 'FAKE-VERIFIED-OCI'
    ;;
  'k3s ctr -n k8s.io images import -')
    stream=$(cat)
    [[ $stream == FAKE-VERIFIED-OCI ]] || exit 89
    touch "$MOCK_STATE/imported"
    ;;
  "bash -s -- --apply '$MOCK_IMAGE' --confirm '$MOCK_IMAGE'")
    cat >/dev/null
    touch "$MOCK_STATE/verified"
    ;;
  "bash -s -- --check '$MOCK_IMAGE'")
    cat >/dev/null
    [[ -f "$MOCK_STATE/verified" ]]
    ;;
  *)
    printf 'Unexpected SSH mock request: %s\n' "$cmd" >&2
    exit 91
    ;;
esac
MOCK
chmod 0700 "$tmp/bin/kubectl" "$tmp/bin/ssh"
export PATH="$tmp/bin:$PATH"

ref=$MOCK_IMAGE
for args in \
  '--plan venus venus images.dbontr.internal/dbontr/compute-gateway:0.1.11' \
  '--plan venus saturn images.dbontr.internal/dbontr/compute-gateway:0.1.11' \
  '--plan venus janus evil.invalid/tag:latest' \
  '--apply venus janus images.dbontr.internal/dbontr/compute-gateway:0.1.11' \
  '--apply venus janus images.dbontr.internal/dbontr/compute-gateway:0.1.11 --confirm wrong'; do
  read -r -a words <<< "$args"
  if bash "$script" "${words[@]}" >/dev/null 2>&1; then
    printf 'Invalid input accepted: %s\n' "$args" >&2
    exit 1
  fi
done
bash "$script" --plan venus janus "$ref" > "$tmp/plan"
grep -q '^PLAN_ONLY:' "$tmp/plan"
[[ ! -e "$tmp/imported" ]] || { echo 'Read-only plan imported image data' >&2; exit 1; }

export MOCK_TARGET_DIGEST=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
if bash "$script" --plan venus janus "$ref" >/dev/null 2>&1; then
  echo 'Conflicting destination digest accepted' >&2; exit 1
fi
if [[ $(id -u) == 0 ]] && bash "$script" --apply venus janus "$ref" --confirm "$ref" >/dev/null 2>&1; then
  echo 'Conflicting destination digest was overwritten' >&2; exit 1
fi
[[ ! -e "$tmp/imported" ]] || { echo 'Conflict caused a partial import' >&2; exit 1; }
export MOCK_TARGET_DIGEST=''
export MOCK_SOURCE_COMPLETE=no
if bash "$script" --plan venus janus "$ref" >/dev/null 2>&1; then
  echo 'Incomplete source image accepted' >&2; exit 1
fi
export MOCK_SOURCE_COMPLETE=yes MOCK_POD_IN_USE=yes
if bash "$script" --plan venus janus "$ref" >/dev/null 2>&1; then
  echo 'Image used by a running Pod was accepted' >&2; exit 1
fi
export MOCK_POD_IN_USE=no
bash "$script" --plan venus janus "$ref" >/dev/null

if (( EUID == 0 )); then
  export MOCK_IMPORT_MISMATCH=yes
  if bash "$script" --apply venus janus "$ref" --confirm "$ref" >/dev/null 2>&1; then
    echo 'Mismatched post-import image digest accepted' >&2; exit 1
  fi
  [[ ! -e "$tmp/verified" ]] || { echo 'Mismatch reached content repair' >&2; exit 1; }
  rm -f "$tmp/imported"
  export MOCK_IMPORT_MISMATCH=no
  bash "$script" --apply venus janus "$ref" --confirm "$ref" > "$tmp/applied"
  grep -q '^TRANSFER_VERIFIED ' "$tmp/applied"
  [[ -f "$tmp/imported" && -f "$tmp/verified" ]] || { echo 'Transfer was not completed or verified' >&2; exit 1; }
fi
printf '%s\n' 'Offline OCI fallback argument, completeness, Pod-use, digest-conflict, bounded-import, and verification tests passed.'
