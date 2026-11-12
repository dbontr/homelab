#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
prefix=${1:?usage: build-compute-images.sh REGISTRY_REPOSITORY_PREFIX ABSOLUTE_OUTPUT_DIRECTORY [--load|--push]}
destination=${2:?usage: build-compute-images.sh REGISTRY_REPOSITORY_PREFIX ABSOLUTE_OUTPUT_DIRECTORY [--load|--push]}
mode=${3:---load}

case "$mode" in
  --load|--push) ;;
  *) printf '%s\n' 'Select --load for local builds or --push for operator-approved registry publication' >&2; exit 2 ;;
esac

if [[ ! "$prefix" =~ ^[a-z0-9][a-z0-9.-]*(:[0-9]{2,5})?(/[a-z0-9][a-z0-9._-]*){1,3}$ ]]; then
  printf '%s\n' 'Provide an exact lowercase registry and repository prefix without a tag or digest' >&2
  exit 2
fi
if [[ "$destination" != /* || "$destination" == "/" || "$destination" == */ ||
      -e "$destination" || -L "$destination" ]]; then
  printf '%s\n' 'Output must be a new absolute directory without trailing slash' >&2
  exit 2
fi

for tool in git docker jq mktemp find; do
  command -v "$tool" >/dev/null || { printf 'Missing image release tool: %s\n' "$tool" >&2; exit 1; }
done
docker buildx version >/dev/null
revision=$(git -C "$root" rev-parse --verify 'HEAD^{commit}') || exit 1
if [[ ! "$revision" =~ ^[a-f0-9]{40}$ ||
      -n "$(git -C "$root" status --porcelain --untracked-files=all)" ]]; then
  printf '%s\n' 'Compute image builds require a clean, exact source commit' >&2
  exit 1
fi
parent=$(dirname -- "$destination")
if [[ ! -d "$parent" || -L "$parent" || ! -O "$parent" ||
      -n "$(find "$parent" -maxdepth 0 -perm /022 -print -quit)" ]]; then
  printf '%s\n' 'Image release parent must be operator-owned and not group or world writable' >&2
  exit 2
fi

stage=$(mktemp -d "$parent/.compute-images.XXXXXXXX")
cleanup() { [[ -z "$stage" ]] || rm -rf -- "$stage"; }
trap cleanup EXIT

context="$root/services/compute"
controller_tag="$prefix/controller:$revision"
worker_tag="$prefix/worker:$revision"

build_image() {
  local name=$1 tag=$2 dockerfile=$3
  local metadata="$stage/$name.json"
  local args=(
    buildx build --platform linux/amd64
    --file "$context/deploy/kubernetes/$dockerfile"
    --tag "$tag"
    --label "org.opencontainers.image.source=https://github.com/dbontr/homelab"
    --label "org.opencontainers.image.revision=$revision"
    --metadata-file "$metadata"
    --progress plain
  )
  if [[ "$mode" == --push ]]; then
    args+=(--push)
  else
    args+=(--load)
  fi
  docker "${args[@]}" "$context"
  if [[ "$mode" == --load ]]; then
    docker image inspect "$tag" >/dev/null
  fi
}

build_image controller "$controller_tag" Dockerfile.controller
build_image worker "$worker_tag" Dockerfile.worker

if [[ "$mode" == --push ]]; then
  controller_digest=$(jq -er '."containerimage.digest" // empty' "$stage/controller.json")
  worker_digest=$(jq -er '."containerimage.digest" // empty' "$stage/worker.json")
  if [[ ! "$controller_digest" =~ ^sha256:[a-f0-9]{64}$ ||
        ! "$worker_digest" =~ ^sha256:[a-f0-9]{64}$ ]]; then
    printf '%s\n' 'Registry did not confirm both immutable image digests' >&2
    exit 1
  fi
  jq -n \
    --arg revision "$revision" \
    --arg platform "linux/amd64" \
    --arg controller "$prefix/controller@$controller_digest" \
    --arg worker "$prefix/worker@$worker_digest" \
    '{
      source_revision:$revision,
      platform:$platform,
      controller_image:$controller,
      worker_image:$worker,
      mode:"registry"
    }' > "$stage/IMAGE-RELEASE.json"
else
  jq -n \
    --arg revision "$revision" \
    --arg platform "linux/amd64" \
    --arg controller "$controller_tag" \
    --arg worker "$worker_tag" \
    '{
      source_revision:$revision,
      platform:$platform,
      controller_local_tag:$controller,
      worker_local_tag:$worker,
      mode:"local",
      deployable:false
    }' > "$stage/IMAGE-RELEASE.json"
fi

# Persist only a source identity and exact image references, never Docker
# credentials, build environment variables, or complete Buildx metadata.
rm -f -- "$stage/controller.json" "$stage/worker.json"
chmod 0444 "$stage/IMAGE-RELEASE.json"
chmod 0555 "$stage"
mv -- "$stage" "$destination"
stage=""
printf 'Compute image release recorded at %s\n' "$destination"
printf '%s\n' 'No Kubernetes resources or existing service state were changed.'
