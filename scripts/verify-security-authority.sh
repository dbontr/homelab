#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
crate="$root/services/security-authority"

if ! command -v cargo >/dev/null 2>&1; then
  printf 'Rust Cargo is required for authority acceptance.\n' >&2
  exit 1
fi
if [[ ! -f "$crate/Cargo.lock" ]]; then
  printf 'The pinned Rust security Cargo.lock is missing; generate and review it before acceptance.\n' >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  printf 'Go is required for the authority compatibility suite.\n' >&2
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
  go test -race -count=5 ./security -run 'TestSecurityAuthority'
  go test -race -count=1 ./security ./privileged
)

if command -v nix >/dev/null 2>&1; then
  nix build "$root#homelab-security-authority" --no-link
else
  printf 'Nix is unavailable; NixOS package acceptance is not complete.\n' >&2
  exit 1
fi

if [[ -n "${SECURITY_AUTHORITY_STATE_COPY:-}" ]]; then
  "$crate/target/release/security-preflight" --state-dir "$SECURITY_AUTHORITY_STATE_COPY"
fi

printf 'Rust security authority source and package checks passed.\n'
