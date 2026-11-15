#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
crate="$root/services/security-authority"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

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

# A minimal NixOS shell may have Cargo without a C linker or Clippy.
# Resolve missing tools from the repository-pinned Nixpkgs input.
if ! command -v cc >/dev/null 2>&1 || ! command -v cargo-clippy >/dev/null 2>&1; then
  command -v nix >/dev/null 2>&1 || {
    printf 'Nix is required for the pinned offline verification toolchain.\n' >&2
    exit 1
  }
  export HOMELAB_SOURCE_ROOT="$root"
  host_system=$(nix eval --offline --impure --raw --expr builtins.currentSystem)
  packages='(builtins.getFlake (builtins.getEnv "HOMELAB_SOURCE_ROOT")).inputs.nixpkgs.legacyPackages.'"$host_system"
  cc_dir=$(nix build --offline --impure --no-link --print-out-paths --expr "${packages}.stdenv.cc.out")
  clippy_dir=$(nix build --offline --impure --no-link --print-out-paths --expr "${packages}.clippy")
  unset HOMELAB_SOURCE_ROOT
  [[ -x "$cc_dir/bin/cc" && -x "$clippy_dir/bin/cargo-clippy" ]] || {
    printf 'The pinned offline verification toolchain is incomplete.\n' >&2
    exit 1
  }
  export PATH="${cc_dir}/bin:${clippy_dir}/bin:$PATH"
fi

(
  cd "$crate"
  cargo fmt --all -- --check
  CARGO_NET_OFFLINE=true cargo test --locked --offline --all-targets
  CARGO_NET_OFFLINE=true cargo clippy --locked --offline --all-targets -- -D warnings
  CARGO_NET_OFFLINE=true cargo build --locked --offline --release --bins
  test -x target/release/securityd
  test -x target/release/security-preflight
  test -x target/release/security-migrate
)

(
  cd "$root/services/core"
  GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test -race -count=5 ./security -run 'TestSecurityAuthority'
  GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test -race -count=1 ./security ./privileged ./statelease ./cmd/homelab-securityd ./cmd/homelab-security-preflight
  GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go build -o "$temporary_dir/homelab-security-preflight" ./cmd/homelab-security-preflight
)

if command -v nix >/dev/null 2>&1; then
  nix build --offline "$root#homelab-security-authority" --no-link
else
  printf 'Nix is unavailable; NixOS package acceptance is not complete.\n' >&2
  exit 1
fi

if [[ -z "${SECURITY_AUTHORITY_STATE_COPY:-}" ]]; then
  printf 'Security acceptance requires SECURITY_AUTHORITY_STATE_COPY pointing to a protected, independent offline state copy.\n' >&2
  exit 1
fi
"$crate/target/release/security-preflight" --state-dir "$SECURITY_AUTHORITY_STATE_COPY"

printf 'Rust security authority source, package and state-copy checks passed.\n'
