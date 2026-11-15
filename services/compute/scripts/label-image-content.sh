#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s --check IMAGE | --apply IMAGE --confirm IMAGE\n' "$0" >&2
  exit 2
}

mode=${1:-}
reference=${2:-}
case "$mode" in
  --check) (( $# == 2 )) || usage ;;
  --apply) (( $# == 4 )) && [[ $3 == --confirm && $4 == "$reference" ]] || usage ;;
  *) usage ;;
esac
if [[ ! $reference =~ ^images\.dbontr\.internal/([a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)+):([A-Za-z0-9_][A-Za-z0-9._-]*)$ ]]; then
  usage
fi
repository=${BASH_REMATCH[1]}
label=containerd.io/distribution.source.images.dbontr.internal
cli=${CTR_BIN:-k3s}
command -v "$cli" >/dev/null || { printf 'Unavailable containerd client: %s\n' "$cli" >&2; exit 1; }
command -v jq >/dev/null || { echo 'jq is required' >&2; exit 1; }
ctr() { "$cli" ctr -n k8s.io "$@"; }
fail() { printf 'Content source verification failed: %s\n' "$*" >&2; exit 1; }

if [[ $mode == --apply ]]; then
  (( EUID == 0 )) || fail 'root is required for content-label changes'
  exec 9>/run/lock/homelab-peer-content-labels.lock
  flock -n 9 || fail 'a content-label operation is already in progress'
fi

scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
ctr images list > "$scratch/images"
mapfile -t indices < <(awk -v image="$reference" '$1 == image {print $3}' "$scratch/images")
(( ${#indices[@]} == 1 )) || fail 'image reference must resolve to exactly one index'
index=${indices[0]}
[[ $index =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'image index has an invalid digest'
ctr content get "$index" > "$scratch/index.json" || fail 'image index content is missing'

if jq -e '(.manifests // []) | type == "array" and length > 0' "$scratch/index.json" >/dev/null; then
  manifest=$(jq -er '
    [.manifests[] | select(.platform.os == "linux" and .platform.architecture == "amd64") | .digest]
    | if length == 1 then .[0] else error("expected one linux/amd64 manifest") end
  ' "$scratch/index.json") || fail 'image has no unique linux/amd64 manifest'
  ctr content get "$manifest" > "$scratch/manifest.json" || fail 'manifest content is missing'
else
  manifest=$index
  cp "$scratch/index.json" "$scratch/manifest.json"
fi
jq -er '.config.digest, .layers[].digest' "$scratch/manifest.json" > "$scratch/descriptors" || fail 'manifest descriptors are invalid'
printf '%s\n%s\n' "$index" "$manifest" >> "$scratch/descriptors"
sort -u "$scratch/descriptors" > "$scratch/sorted"
mapfile -t digests < "$scratch/sorted"
(( ${#digests[@]} >= 3 )) || fail 'too few OCI content descriptors'
ctr content ls > "$scratch/content"
missing=()
declare -A desired=()
for digest in "${digests[@]}"; do
  [[ $digest =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'invalid content digest'
  mapfile -t rows < <(awk -v digest="$digest" '$1 == digest {print}' "$scratch/content")
  (( ${#rows[@]} == 1 )) || fail "content descriptor is not uniquely present: $digest"
  row=${rows[0]}
  sources=()
  if [[ $row == *"$label="* ]]; then
    raw=${row#*"$label="}
    raw=${raw%%[[:space:]]*}
    IFS=',' read -r -a tokens <<< "$raw"
    for token in "${tokens[@]}"; do
      [[ $token == *=* ]] && break  # Next containerd label, not a repository.
      [[ $token =~ ^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)+$ ]] || fail "invalid existing source repository: $digest"
      sources+=("$token")
    done
    (( ${#sources[@]} > 0 )) || fail "empty source label: $digest"
  fi
  found=0
  for source in "${sources[@]}"; do
    if [[ $source == "$repository" ]]; then found=1; fi
  done
  if (( found == 0 )); then
    missing+=("$digest")
    if (( ${#sources[@]} > 0 )); then
      desired[$digest]="$(IFS=,; echo "${sources[*]}")",$repository
    else
      desired[$digest]=$repository
    fi
    (( ${#desired[$digest]} <= 4096 )) || fail "source label too long: $digest"
    printf 'MISSING_SOURCE %s\n' "$digest"
  fi
done

if [[ $mode == --check ]]; then
  (( ${#missing[@]} == 0 )) || fail "${#missing[@]} descriptors lack the expected registry source"
  printf 'SOURCE_COMPLETE %s (%d descriptors)\n' "$reference" "${#digests[@]}"
  exit 0
fi

# Verify every compressed blob before changing even one containerd metadata record.
for digest in "${digests[@]}"; do
  actual=$(ctr content get "$digest" | sha256sum | awk '{print $1}') || fail "cannot read content: $digest"
  [[ "sha256:$actual" == "$digest" ]] || fail "content SHA-256 mismatch: $digest"
done
for digest in "${missing[@]}"; do
  ctr content label "$digest" "$label=${desired[$digest]}" >/dev/null || fail "cannot label content: $digest"
  printf 'LABELLED %s\n' "$digest"
done
printf 'SOURCE_COMPLETE %s (%d descriptors, %d repaired)\n' "$reference" "${#digests[@]}" "${#missing[@]}"
printf 'Refresh the publisher Spegel index through the approved K3s maintenance procedure.\n'
