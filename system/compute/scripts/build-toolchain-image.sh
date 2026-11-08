#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
image=${1:?usage: build-toolchain-image.sh IMAGE [--load|--push]}
mode=${2:---load}

case "$mode" in
  --load|--push) ;;
  *) printf 'Mode must be --load or --push\n' >&2; exit 2 ;;
esac

if [[ ! "$image" =~ ^[a-z0-9][a-z0-9./:_-]+$ ]]; then
  printf 'Image reference contains invalid characters\n' >&2
  exit 2
fi

command -v docker >/dev/null || { printf 'Docker Buildx is required\n' >&2; exit 1; }
docker buildx version >/dev/null

metadata=$(mktemp)
trap 'rm -f -- "$metadata"' EXIT

args=(
  buildx build --platform linux/amd64
  --file "$root/deploy/kubernetes/Dockerfile.go-rust-wasm"
  --tag "$image"
  --metadata-file "$metadata"
  --progress plain
)
if [[ "$mode" == --push ]]; then
  args+=(--push)
else
  args+=(--load)
fi

docker "${args[@]}" "$root"
printf 'Image: %s\n' "$image"
if command -v jq >/dev/null; then
  digest=$(jq -er '."containerimage.digest" // empty' "$metadata" 2>/dev/null || true)
  if [[ -n "$digest" ]]; then printf 'Digest: %s\n' "$digest"; fi
fi
