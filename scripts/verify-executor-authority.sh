#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
crate="$root/services/executor"

if ! command -v cargo >/dev/null 2>&1; then
  printf 'Rust Cargo is required for executor acceptance.\n' >&2
  exit 1
fi
if [[ ! -f "$crate/Cargo.lock" ]]; then
  printf 'Rust executor Cargo.lock must be resolved and reviewed before acceptance.\n' >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  printf 'Go is required for executor wire compatibility and release checks.\n' >&2
  exit 1
fi

(
  cd "$crate"
  cargo fmt --all -- --check
  cargo test --locked --all-targets
  cargo clippy --locked --all-targets -- -D warnings
  cargo build --locked --release --bins
)

(
  cd "$root/services/core"
  go test -race ./security ./privileged ./release ./statelease
)

if command -v nix >/dev/null 2>&1; then
  nix build "$root#homelab-executor-authority" --no-link
else
  printf 'NixOS packaging is part of the privileged executor acceptance.\n' >&2
  exit 1
fi

printf 'Rust executor authority source and release checks passed.\n'
