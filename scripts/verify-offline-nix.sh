#!/usr/bin/env bash
set -euo pipefail

# Supply the repository-pinned native toolchain for local acceptance checks.
# This script neither activates NixOS configurations nor uses GitHub Actions.
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [[ "$(uname -s)-$(uname -m)" != Linux-x86_64 ]]; then
  printf '%s\n' 'Pinned offline acceptance currently requires x86_64 Linux.' >&2
  exit 2
fi
command -v nix >/dev/null 2>&1 || {
  printf '%s\n' 'Nix is required for offline acceptance.' >&2
  exit 2
}

export HOMELAB_SOURCE_ROOT="$root"
packages='(builtins.getFlake (builtins.getEnv "HOMELAB_SOURCE_ROOT")).inputs.nixpkgs.legacyPackages.x86_64-linux'
pinned_output() {
  local store_path
  store_path=$(nix eval --offline --impure --raw --expr "${packages}.${1}.outPath")
  nix build --offline --impure --no-link --expr "${packages}.${1}" >/dev/null
  printf '%s\n' "$store_path"
}

cc=$(pinned_output stdenv.cc)
rustc=$(pinned_output rustc)
cargo=$(pinned_output cargo)
rustfmt=$(pinned_output rustfmt)
clippy=$(pinned_output clippy)
go=$(pinned_output go_1_27)
helm=$(pinned_output kubernetes-helm)
pkgconfig=$(pinned_output pkg-config)
sqlite_dev=$(pinned_output sqlite.dev)
sqlite_lib=$(pinned_output sqlite.out)
unset HOMELAB_SOURCE_ROOT

for path in "$cc/bin/cc" "$rustc/bin/rustc" "$cargo/bin/cargo" \
            "$clippy/bin/cargo-clippy" "$rustfmt/bin/cargo-fmt" "$rustfmt/bin/rustfmt" "$go/bin/go" "$helm/bin/helm" \
            "$pkgconfig/bin/pkg-config" "$sqlite_dev/include/sqlite3.h" \
            "$sqlite_lib/lib/libsqlite3.so"; do
  [[ -e "$path" ]] || {
    printf 'Pinned toolchain component missing: %s\n' "$path" >&2
    exit 1
  }
done

export PATH="$helm/bin:$go/bin:$cargo/bin:$rustc/bin:$rustfmt/bin:$clippy/bin:$pkgconfig/bin:$cc/bin:$PATH"
export CC="$cc/bin/cc"
export CGO_CFLAGS="-I$sqlite_dev/include${CGO_CFLAGS:+ $CGO_CFLAGS}"
export CGO_LDFLAGS="-L$sqlite_lib/lib -lsqlite3${CGO_LDFLAGS:+ $CGO_LDFLAGS}"
export PKG_CONFIG_PATH="$sqlite_dev/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
export LD_LIBRARY_PATH="$sqlite_lib/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

exec bash "$root/scripts/verify-offline.sh" "$@"
