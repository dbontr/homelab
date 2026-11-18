#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 || -z "$1" || -z "$2" ]]; then
  printf 'Usage: %s RUST_VERSION LEAN_VERSION\n' "$0" >&2
  exit 2
fi

rust_version="$1"
lean_version="$2"
if [[ ! "$rust_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ||
      ! "$lean_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Toolchain versions must be exact numeric major.minor.patch values" >&2
  exit 2
fi

for program in rustc cargo rustup lean lake; do
  if ! command -v "$program" >/dev/null 2>&1; then
    printf 'Required toolchain executable missing: %s\n' "$program" >&2
    exit 1
  fi
done

rust_output="$(rustc --version)"
cargo_output="$(cargo --version)"
lean_output="$(lean --version)"
lake_output="$(lake --version)"
if [[ "$rust_output" != "rustc $rust_version "* ||
      "$cargo_output" != "cargo $rust_version "* ||
      "$lean_output" != *"version $lean_version"* ]]; then
  printf 'Toolchain version mismatch\nRust: %s\nCargo: %s\nLean: %s\n' \
    "$rust_output" "$cargo_output" "$lean_output" >&2
  exit 1
fi
if ! rustup target list --installed | grep -Fxq 'wasm32-unknown-unknown'; then
  echo "Required Rust WebAssembly target unavailable" >&2
  exit 1
fi
if ! cargo fmt --version >/dev/null || ! cargo clippy --version >/dev/null; then
  echo "Rust formatting and Clippy components unavailable" >&2
  exit 1
fi

printf 'rust: %s\ncargo: %s\nlean: %s\nlake: %s\nwasm: wasm32-unknown-unknown\n' \
  "$rust_output" "$cargo_output" "$lean_output" "$lake_output"
