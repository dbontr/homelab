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
  )
}

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
  cargo test --offline
  cargo check --offline --target wasm32-unknown-unknown
)

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
nix --extra-experimental-features 'nix-command flakes' build --offline --no-link '.#packages.x86_64-linux.homelab-platform'
printf '%s\n' 'All local source checks succeeded. No NixOS configuration was activated.'
