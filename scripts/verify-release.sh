#!/usr/bin/env bash
set -euo pipefail

export LC_ALL=C
release=${1:?usage: verify-release.sh ABSOLUTE_RELEASE_DIRECTORY}
if [[ "$release" != /* || ! -d "$release" || -L "$release" ]]; then
  printf '%s\n' 'Release path must name an existing, non-symlink absolute directory' >&2
  exit 2
fi
for tool in find sort xargs sha256sum cmp mktemp; do
  command -v "$tool" >/dev/null || {
    printf 'Missing release verification tool: %s\n' "$tool" >&2
    exit 1
  }
done

# Verify the directory envelope before reading any manifest-controlled data.
for item in "$release/RELEASE" "$release/SHA256SUMS" "$release/bin" "$release/console"; do
  if [[ -L "$item" || ! -e "$item" ]]; then
    printf 'Required release entry missing or linked: %s\n' "$item" >&2
    exit 1
  fi
done
if [[ ! -f "$release/RELEASE" || ! -f "$release/SHA256SUMS" ||
      ! -d "$release/bin" || ! -d "$release/console" ]]; then
  printf '%s\n' 'Release envelope has invalid entry types' >&2
  exit 1
fi
while IFS= read -r -d '' entry; do
  case "${entry##*/}" in
    RELEASE|SHA256SUMS|bin|console) ;;
    *) printf '%s\n' 'Unexpected top-level release entry' >&2; exit 1 ;;
  esac
done < <(find "$release" -mindepth 1 -maxdepth 1 -print0)
if [[ -n "$(find "$release/bin" "$release/console" -type l -print -quit)" ||
      -n "$(find "$release/bin" "$release/console" ! -type f ! -type d -print -quit)" ]]; then
  printf '%s\n' 'Release contains a symlink or unsupported file type' >&2
  exit 1
fi
if [[ -n "$(find "$release" -perm /022 -print -quit)" ]]; then
  printf '%s\n' 'Release envelope contains group-writable or world-writable entries' >&2
  exit 1
fi

mapfile -t metadata < "$release/RELEASE"
if [[ ${#metadata[@]} -ne 2 ||
      ! ${metadata[0]} =~ ^source_revision=[a-f0-9]{40}$ ||
      ! ${metadata[1]} =~ ^build_utc=[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
  printf '%s\n' 'Release provenance requires an exact Git commit and UTC build timestamp' >&2
  exit 1
fi

# Only the declared native executable set may reside in the binary directory.
expected=(
  homelab-platform homelab-engineeringd homelab-approve homelab-securityd
  homelab-executord homelab-release homelab-admin homelab-compute-controller
  homelab-compute-worker homelab-learning homelab-health
  homelab-mcp homelab-api
)
for binary in "${expected[@]}"; do
  if [[ ! -f "$release/bin/$binary" || -L "$release/bin/$binary" ||
        ! -x "$release/bin/$binary" ]]; then
    printf 'Missing or non-executable release component: %s\n' "$binary" >&2
    exit 1
  fi
done
count=$(find "$release/bin" -mindepth 1 -type f -print | wc -l)
if [[ "$count" -ne "${#expected[@]}" ||
      -n "$(find "$release/bin" -mindepth 1 -type d -print -quit)" ]]; then
  printf '%s\n' 'Release contains undeclared native components' >&2
  exit 1
fi
if [[ ! -s "$release/console/index.html" ||
      -L "$release/console/index.html" ]]; then
  printf '%s\n' 'Release has no verified console entrypoint' >&2
  exit 1
fi

umask 077
manifest=$(mktemp)
trap 'rm -f -- "$manifest"' EXIT
(
  cd "$release"
  find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum
) > "$manifest"
if [[ ! -s "$manifest" || ! -s "$release/SHA256SUMS" ]] ||
   ! cmp -s -- "$manifest" "$release/SHA256SUMS"; then
  printf '%s\n' 'Release SHA-256 manifest does not match the exact artifact contents' >&2
  exit 1
fi
printf 'Verified release bundle from %s\n' "${metadata[0]#source_revision=}"
