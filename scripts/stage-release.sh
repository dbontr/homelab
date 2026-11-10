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
mode=complete
component=""
if [[ $# -eq 3 && "${1:-}" == --component ]]; then
  mode=component
  component=$2
  destination=$3
elif [[ $# -eq 1 ]]; then
  destination=$1
else
  printf '%s\n' 'usage: stage-release.sh [--component COMPONENT] ABSOLUTE_OUTPUT_DIRECTORY' >&2
  exit 2
fi

source_module=""
case "$component" in
  "") [[ "$mode" == complete ]] || exit 2 ;;
  homelab-platform|homelab-calendard|homelab-engineeringd|homelab-approve|homelab-securityd|homelab-executord|homelab-release|homelab-admin)
    source_module=services/core ;;
  homelab-compute-controller|homelab-compute-worker)
    source_module=services/compute ;;
  homelab-learning) source_module=services/learning ;;
  homelab-health) source_module=services/health ;;
  homelab-mcp|homelab-api) source_module=. ;;
  homelab-console) source_module=ui/console ;;
  *) printf 'Unknown release component: %s\n' "$component" >&2; exit 2 ;;
esac
if [[ "$destination" != /* || "$destination" == "/" || "$destination" == */ ]]; then
  printf 'Provide an unused absolute output directory without a trailing slash\n' >&2
  exit 2
fi
if [[ -e "$destination" ]]; then
  printf 'Release destination already exists: %s\n' "$destination" >&2
  exit 2
fi
for tool in git sha256sum find sort mktemp; do
  command -v "$tool" >/dev/null || { printf 'Missing build tool: %s\n' "$tool" >&2; exit 1; }
done
if [[ "$mode" == complete ]]; then
  required_tools=(go dx)
elif [[ "$component" == homelab-console ]]; then
  required_tools=(cargo dx)
else
  required_tools=(go)
fi
for tool in "${required_tools[@]}"; do
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
if [[ "$mode" == complete ]]; then
  bash "$root/scripts/verify-offline.sh" --all
elif [[ "$component" == homelab-console ]]; then
  (
    cd "$root/ui/console"
    cargo fmt --all -- --check
    cargo test --offline
    cargo check --offline --target wasm32-unknown-unknown
  )
else
  (
    cd "$root/$source_module"
    go test ./...
    go test -race ./...
    go vet ./...
  )
fi

stage=$(mktemp -d "$parent/.homelab-release.XXXXXXXX")
cleanup() {
  if [[ -z "$stage" || ! -d "$stage" || -L "$stage" ]]; then
    return
  fi
  # Verification can fail after the staged release becomes read-only.
  chmod -R u+w -- "$stage" 2>/dev/null || true
  rm -rf -- "$stage"
}
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

if [[ "$mode" == complete ]]; then
  build_go services/core ./cmd/homelab-platform homelab-platform
  build_go services/core ./cmd/homelab-calendard homelab-calendard
  build_go . ./mcp homelab-mcp
  build_go . ./api homelab-api
  build_go services/core ./cmd/homelab-engineeringd homelab-engineeringd
  build_go services/core ./cmd/homelab-approve homelab-approve
  build_go services/core ./cmd/homelab-securityd homelab-securityd
  build_go services/core ./cmd/homelab-executord homelab-executord
  build_go services/core ./cmd/homelab-release homelab-release
  build_go services/core ./cmd/homelab-admin homelab-admin
  build_go services/compute ./cmd/project-workspace-controller homelab-compute-controller
  build_go services/compute ./cmd/project-workspace-worker homelab-compute-worker
  build_go services/learning ./cmd/adaptive-teaching homelab-learning
  build_go services/health ./cmd/personal-health-mcp homelab-health
elif [[ "$component" != homelab-console ]]; then
  case "$component" in
    homelab-compute-controller) package=./cmd/project-workspace-controller ;;
    homelab-compute-worker) package=./cmd/project-workspace-worker ;;
    homelab-learning) package=./cmd/adaptive-teaching ;;
    homelab-health) package=./cmd/personal-health-mcp ;;
    homelab-mcp) package=./mcp ;;
    homelab-api) package=./api ;;
    *) package="./cmd/$component" ;;
  esac
  build_go "$source_module" "$package" "$component"
fi

if [[ "$mode" == complete || "$component" == homelab-console ]]; then
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
fi

(
  cd "$stage"
  find bin console -type f -print0 |
    sort -z |
    xargs -0 -r sha256sum > SHA256SUMS
  printf 'source_revision=%s\n' "$revision" > RELEASE
  printf 'build_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> RELEASE
  if [[ "$mode" == component ]]; then
    printf 'component=%s\n' "$component" >> RELEASE
  fi
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
