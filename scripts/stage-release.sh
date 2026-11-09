#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C
export GOWORK=off
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off
export CARGO_NET_OFFLINE=true

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
destination=${1:?usage: stage-release.sh ABSOLUTE_OUTPUT_DIRECTORY}
if [[ "$destination" != /* || "$destination" == "/" || "$destination" == */ ]]; then
  printf 'Provide an unused absolute output directory without a trailing slash\n' >&2
  exit 2
fi
if [[ -e "$destination" ]]; then
  printf 'Release destination already exists: %s\n' "$destination" >&2
  exit 2
fi
for tool in git go dx sha256sum find sort mktemp; do
  command -v "$tool" >/dev/null || { printf 'Missing build tool: %s\n' "$tool" >&2; exit 1; }
done

parent=$(dirname -- "$destination")
if [[ ! -d "$parent" ]]; then
  printf 'Release parent does not exist: %s\n' "$parent" >&2
  exit 2
fi
if [[ -L "$parent" || ! -O "$parent" ||
      -n "$(find "$parent" -maxdepth 0 -perm /022 -print -quit)" ]]; then
  printf '%s\n' 'Release parent must be an operator-owned, non-symlink directory without group or world write access' >&2
  exit 2
fi

revision=$(git -C "$root" rev-parse --verify 'HEAD^{commit}' 2>/dev/null) || {
  printf '%s\n' 'A trusted exact Git commit is required for the release' >&2
  exit 1
}
if [[ ! "$revision" =~ ^[a-f0-9]{40}$ ||
      -n "$(git -C "$root" status --porcelain --untracked-files=all)" ]]; then
  printf '%s\n' 'Release source must be an exact clean Git revision' >&2
  exit 1
fi
printf 'Verifying locally pinned source at %s\n' "$revision"
bash "$root/scripts/verify-offline.sh" --all

stage=$(mktemp -d "$parent/.homelab-release.XXXXXXXX")
cleanup() { [[ -z "$stage" ]] || rm -rf -- "$stage"; }
trap cleanup EXIT
mkdir -p "$stage/bin" "$stage/console"

build_go() {
  local directory=$1 package=$2 name=$3
  printf 'Building %s\n' "$name"
  (
    cd "$root/$directory"
    go build -trimpath -buildvcs=false -o "$stage/bin/$name" "$package"
  )
}

build_go system ./cmd/homelab-platform homelab-platform
build_go . ./mcp homelab-mcp
build_go . ./api homelab-api
build_go system ./cmd/homelab-engineeringd homelab-engineeringd
build_go system ./cmd/homelab-approve homelab-approve
build_go system ./cmd/homelab-securityd homelab-securityd
build_go system ./cmd/homelab-executord homelab-executord
build_go system ./cmd/homelab-release homelab-release
build_go system ./cmd/homelab-admin homelab-admin
build_go services/compute ./cmd/project-workspace-controller homelab-compute-controller
build_go services/compute ./cmd/project-workspace-worker homelab-compute-worker
build_go services/learning ./cmd/adaptive-teaching homelab-learning
build_go services/health ./cmd/personal-health-mcp homelab-health

(
  cd "$root/ui/console"
  dx bundle --web --release --out-dir "$stage/console-bundle"
)
if [[ ! -f "$stage/console-bundle/public/index.html" ]]; then
  printf 'The Dioxus web bundle did not contain public/index.html\n' >&2
  exit 1
fi
cp -a "$stage/console-bundle/public/." "$stage/console/"
rm -rf -- "$stage/console-bundle"

(
  cd "$stage"
  find bin console -type f -print0 |
    sort -z |
    xargs -0 -r sha256sum > SHA256SUMS
  printf 'source_revision=%s\n' "$revision" > RELEASE
  printf 'build_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> RELEASE
)

# Artifacts contain no runtime credentials. Allow isolated DynamicUser
# services to execute source-built binaries without making releases writable.
find "$stage/bin" "$stage/console" -type f -exec chmod 0444 {} +
find "$stage/bin" -type f -exec chmod 0555 {} +
find "$stage/bin" "$stage/console" -type d -exec chmod 0555 {} +
chmod 0444 "$stage/RELEASE" "$stage/SHA256SUMS"
chmod 0555 "$stage"
bash "$root/scripts/verify-release.sh" "$stage"
mv -- "$stage" "$destination"
stage=""
printf 'Staged source-built release: %s\n' "$destination"
printf 'No service was started, replaced or deployed.\n'
