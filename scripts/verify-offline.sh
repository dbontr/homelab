#!/bin/sh
set -eu

case "${1:---all}" in
  --all|--core-only) mode="${1:---all}" ;;
  *) printf '%s\n' 'usage: scripts/verify-offline.sh [--all|--core-only]' >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export GOWORK=off
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off

verify_go() {
  dir=$1
  printf 'Checking %s\n' "$dir"
  (
    cd "$root/$dir"
    go test ./...
    go test -race ./...
    go vet ./...
    go build ./cmd/...
  )
}

if ! command -v go >/dev/null 2>&1; then
  printf "%s\n" "Go is required for the selected verification mode." >&2
  exit 1
fi

verify_go system

if [ "$mode" = --core-only ]; then
  printf '%s\n' 'Core offline checks complete; imported modules and NixOS remain unverified.'
  exit 0
fi

verify_go system/compute
verify_go system/learning
verify_go system/health

if ! command -v cargo >/dev/null 2>&1; then
  printf '%s\n' 'Rust Cargo is required to verify the console source.' >&2
  exit 1
fi
(
  cd "$root/system/console"
  cargo fmt --all -- --check
  cargo test --offline
  cargo check --offline --target wasm32-unknown-unknown
)

if ! command -v helm >/dev/null 2>&1; then
  printf "%s\n" "Helm is required to verify Compute controller templates." >&2
  exit 1
fi
chart="$root/system/compute/deploy/helm/project-workspace"
helm lint "$chart"
pinned_controller="ghcr.io/dbontr/eminence/controller@sha256:$(printf '%064d' 0)"
pinned_worker="ghcr.io/dbontr/eminence/worker@sha256:$(printf '%064d' 0)"
render=$(helm template homelab-acceptance "$chart" \
  --set controller.enabled=true \
  --set-string controller.image="$pinned_controller" \
  --set controller.embedding.url=http://127.0.0.1:9001/v1/embeddings \
  --set controller.embedding.model=acceptance \
  --set controller.embedding.tokenSecretName=acceptance-embedding-secret)
printf "%s\n" "$render" | grep -q "name: PWM_EMBEDDINGS_TOKEN_FILE"
printf "%s\n" "$render" | grep -q "mountPath: /run/embeddings"
if helm template homelab-acceptance "$chart" \
  --set controller.enabled=true \
  --set-string controller.image="$pinned_controller" \
  --set controller.embedding.tokenSecretName=acceptance-embedding-secret >/dev/null 2>&1; then
  printf "%s\n" "Embedding credentials were accepted without a provider endpoint." >&2
  exit 1
fi

# Neither a mutable image tag nor an unspecified image may create a
# controller or worker Deployment in the self-hosted release chart.
for image in "ghcr.io/dbontr/eminence/controller:main" ""; do
  if helm template homelab-acceptance "$chart" \
    --set controller.enabled=true \
    --set-string controller.image="$image" >/dev/null 2>&1; then
    printf '%s\n' 'Controller admitted an image without a registry digest.' >&2
    exit 1
  fi
done
for image in "ghcr.io/dbontr/eminence/worker:main" ""; do
  if helm template homelab-acceptance "$chart" \
    --set controller.enabled=false \
    --set worker.enabled=true \
    --set-string worker.image="$image" >/dev/null 2>&1; then
    printf '%s\n' 'Worker admitted an image without a registry digest.' >&2
    exit 1
  fi
done
helm template homelab-acceptance "$chart" \
  --set controller.enabled=false \
  --set worker.enabled=true \
  --set-string worker.image="$pinned_worker" >/dev/null

if ! command -v nix >/dev/null 2>&1; then
  printf '%s\n' 'Nix is required for offline fleet evaluation.' >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  printf '%s\n' 'jq is required to enumerate enrolled NixOS hosts.' >&2
  exit 1
fi

cd "$root"
nix --extra-experimental-features 'nix-command flakes' flake check --offline --no-build
hosts=$(nix --extra-experimental-features 'nix-command flakes' eval --offline --json '.#nixosConfigurations' --apply builtins.attrNames | jq -r '.[]')
for host in $hosts; do
  printf 'Evaluating %s\n' "$host"
  nix --extra-experimental-features 'nix-command flakes' eval --offline --raw ".#nixosConfigurations.$host.config.system.build.toplevel.drvPath"
done
for package in homelab-platform homelab-engineering homelab-security homelab-executor homelab-fleet; do
  nix --extra-experimental-features 'nix-command flakes' build --offline --no-link ".#packages.x86_64-linux.$package"
done
bash "$root/scripts/tests/verify-release-test.sh"
printf '%s\n' 'All local source checks succeeded. No NixOS configuration was activated.'
