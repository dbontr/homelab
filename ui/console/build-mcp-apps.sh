#!/usr/bin/env bash
set -euo pipefail
export CARGO_NET_OFFLINE=true

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [[ $# -ne 1 || "$1" != /* || "$1" == "/" || "$1" == */ ]]; then
  printf '%s\n' 'Usage: build-mcp-apps.sh UNUSED_ABSOLUTE_OUTPUT_DIRECTORY' >&2
  exit 2
fi

output=$1
parent=$(dirname -- "$output")
if [[ -e "$output" || -L "$output" || ! -d "$parent" || -L "$parent" ]]; then
  printf '%s\n' 'The output must be an unused path beneath an existing directory' >&2
  exit 2
fi

for tool in cargo dx find grep sed mktemp sort sha256sum xargs; do
  command -v "$tool" >/dev/null || {
    printf 'Missing required bundle tool: %s\n' "$tool" >&2
    exit 1
  }
done

# Packaging is not allowed to resolve fresh or floating dependencies.
# A source-only preview with no lockfile is not an eligible release artifact.
if [[ ! -s "$root/Cargo.lock" ]]; then
  printf '%s\n' 'The Rust MCP Apps bundle requires a committed Cargo.lock' >&2
  exit 1
fi
(
  cd "$root"
  cargo metadata --locked --offline --features embedded-app --no-deps --format-version 1 >/dev/null
)

umask 077
stage=$(mktemp -d "$parent/.homelab-mcp-apps.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT

(
  cd "$root"
  dx bundle --web --release --features embedded-app --out-dir "$stage/bundle"
)
bundled="$stage/bundle/public"
if [[ ! -f "$bundled/index.html" ||
      -z "$(find "$bundled" -name '*.wasm' -type f -print -quit)" ]]; then
  printf '%s\n' 'Dioxus did not produce a complete WebAssembly web bundle' >&2
  exit 1
fi
if ! grep -Fq '<head>' "$bundled/index.html"; then
  printf '%s\n' 'Dioxus did not produce an HTML head for view selection' >&2
  exit 1
fi

mkdir -- "$stage/export"
cp -a -- "$bundled/." "$stage/export/"
# The verified asset host serves original files; precompressed variants are
# not distributed and must not be included in the immutable manifest.
find "$stage/export" -type f \( -name '*.br' -o -name '*.gz' \) -delete
for view in cognitive dashboard workout-editor groceries workout-companion; do
  sed "s|<head>|<head><meta name=\"homelab-mcp-view\" content=\"$view\">|" \
    "$bundled/index.html" > "$stage/export/health-$view.html"
  if ! grep -Fq "content=\"$view\"" "$stage/export/health-$view.html"; then
    printf 'Missing view marker for %s\n' "$view" >&2
    exit 1
  fi
done
# Learning uses the same verified Rust/WASM binary, but its MCP host
# publishes a different fixed resource URI and a separately scoped tool set.
sed 's|<head>|<head><meta name="homelab-mcp-view" content="learning">|' \
  "$bundled/index.html" > "$stage/export/learning.html"
if ! grep -Fq 'content="learning"' "$stage/export/learning.html"; then
  printf '%s\n' 'Missing embedded Learning view marker' >&2
  exit 1
fi

(
  cd "$stage/export"
  find . -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
)
mv -- "$stage/export" "$output"
printf 'Built six Rust MCP Apps views and shared WebAssembly assets in %s\n' "$output"
printf '%s\n' 'Asset hosting and MCP resource publication require separate operator configuration.'
