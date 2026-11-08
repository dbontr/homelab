#!/usr/bin/env bash
set -euo pipefail

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
for tool in go dx sha256sum find sort mktemp; do
  command -v "$tool" >/dev/null || { printf 'Missing build tool: %s\n' "$tool" >&2; exit 1; }
done

parent=$(dirname -- "$destination")
if [[ ! -d "$parent" ]]; then
  printf 'Release parent does not exist: %s\n' "$parent" >&2
  exit 2
fi

stage=$(mktemp -d "$parent/.homelab-release.XXXXXXXX")
cleanup() { [[ -z "$stage" ]] || rm -rf -- "$stage"; }
trap cleanup EXIT
mkdir -p "$stage/bin" "$stage/console"

build_go() {
  local directory=$1 package=$2 name=$3
  printf 'Building %s\n' "$name"
  (
    cd "$root/$directory"
    GOWORK=off go build -trimpath -buildvcs=false -o "$stage/bin/$name" "$package"
  )
}

build_go system ./cmd/homelab-platform homelab-platform
build_go system ./cmd/homelab-approve homelab-approve
build_go system/compute ./cmd/project-workspace-controller project-workspace-controller
build_go system/compute ./cmd/project-workspace-worker project-workspace-worker
build_go system/learning ./cmd/adaptive-teaching adaptive-teaching
build_go system/health ./cmd/personal-health-mcp personal-health-mcp

(
  cd "$root/system/console"
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
  printf 'source_revision=%s\n' "$(git -C "$root" rev-parse HEAD 2>/dev/null || printf 'unknown')" > RELEASE
  printf 'build_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> RELEASE
)

mv -- "$stage" "$destination"
stage=""
printf 'Staged source-built release: %s\n' "$destination"
printf 'No service was started, replaced or deployed.\n'
