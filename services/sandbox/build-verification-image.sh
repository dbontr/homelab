#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
if [[ $# -ne 1 ]]; then
  printf '%s\n' 'Usage: build-verification-image.sh REGISTRY_IMAGE:TAG' >&2
  exit 2
fi
image=$1
rust_base=${RUST_VERIFIER_BUILD_IMAGE:?Provide digest-pinned RUST_VERIFIER_BUILD_IMAGE}
go_base=${GO_VERIFIER_RUNTIME_IMAGE:?Provide digest-pinned GO_VERIFIER_RUNTIME_IMAGE}
pinned='^[a-z0-9][a-z0-9./:_-]+@sha256:[a-f0-9]{64}$'
for reference in "$rust_base" "$go_base"; do
  if [[ ! "$reference" =~ $pinned || "$reference" == *..* || "$reference" == *//* ]]; then
    printf '%s\n' 'Toolchain base images must be exact immutable digest references' >&2
    exit 2
  fi
done
if [[ ! "$image" =~ ^[a-z0-9][a-z0-9./:_-]+:[a-z0-9_.-]+$ || "$image" == *..* ]]; then
  printf '%s\n' 'Provide a registry image tag for publishing the verified build' >&2
  exit 2
fi

command -v docker >/dev/null
docker buildx version >/dev/null
command -v jq >/dev/null

metadata=$(mktemp)
trap 'rm -f -- "$metadata"' EXIT
docker buildx build \
  --platform linux/amd64 \
  --file "$root/services/sandbox/Dockerfile" \
  --build-arg "RUST_BASE_IMAGE=$rust_base" \
  --build-arg "GO_BASE_IMAGE=$go_base" \
  --tag "$image" --push --metadata-file "$metadata" "$root"

digest=$(jq -er '."containerimage.digest"' "$metadata")
if [[ ! "$digest" =~ ^sha256:[a-f0-9]{64}$ ]]; then
  printf '%s\n' 'The registry build returned no immutable OCI digest' >&2
  exit 1
fi
printf 'Pinned verification image: %s\n' "${image%:*}@$digest"
printf '%s\n' 'Publishing an image does not authorize a cluster activation.'
