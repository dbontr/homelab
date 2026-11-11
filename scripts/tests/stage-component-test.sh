#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
scratch=$(mktemp -d)
trap 'chmod -R u+w -- "$scratch"; rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin"

fixture="$scratch/repository"
mkdir -p "$fixture/scripts" "$fixture/services/core" "$fixture/services/motivation"
cp "$root/scripts/stage-release.sh" "$fixture/scripts/stage-release.sh"
cp "$root/scripts/verify-release.sh" "$fixture/scripts/verify-release.sh"
printf 'pinned source\n' > "$fixture/fixture.txt"
printf 'core\n' > "$fixture/services/core/.fixture"
printf 'motivation\n' > "$fixture/services/motivation/.fixture"
git -C "$fixture" init -q
git -C "$fixture" add .
git -C "$fixture" -c user.name="Release fixture" -c user.email="fixture@example.invalid" \
  commit -qm "Release test fixture"
revision=$(git -C "$fixture" rev-parse HEAD)
export HOMELAB_RELEASE_TEST_SOURCE_DIR="$fixture"

# These local stubs exercise the stage/verify transaction without compiling
# code, contacting a registry, or changing host configuration.
cat > "$scratch/bin/go" <<'GO'
#!/bin/sh
case "${1-}" in
  test|vet) exit 0 ;;
  build)
    if [ "$PWD" = "$HOMELAB_RELEASE_TEST_SOURCE_DIR" ]; then
      printf '%s\n' 'Release build used the mutable checkout' >&2
      exit 43
    fi
    if [ "${HOMELAB_RELEASE_TEST_MUTATE_SOURCE:-0}" = 1 ]; then
      printf 'mutated\n' >> fixture.txt
    fi
    output=""
    while [ "$#" -gt 0 ]; do
      if [ "$1" = -o ]; then
        shift
        output=${1-}
        break
      fi
      shift
    done
    [ -n "$output" ] || exit 2
    printf '#!/bin/sh\nexit 0\n' > "$output"
    chmod 0700 "$output"
    if [ "${HOMELAB_RELEASE_TEST_EXTRA:-0}" = 1 ]; then
      printf '#!/bin/sh\nexit 0\n' > "$(dirname "$output")/rogue"
      chmod 0700 "$(dirname "$output")/rogue"
    fi
    exit 0 ;;
  *) exit 2 ;;
esac
GO
cat > "$scratch/bin/cargo" <<'CARGO'
#!/bin/sh
case "${1-}" in
  fmt|test|clippy) exit 0 ;;
  build)
    if [ "$PWD" = "$HOMELAB_RELEASE_TEST_SOURCE_DIR/services/motivation" ]; then
      printf '%s\n' 'Rust build used the mutable checkout' >&2
      exit 43
    fi
    [ -n "${CARGO_TARGET_DIR:-}" ] || exit 2
    mkdir -p "$CARGO_TARGET_DIR/release"
    printf '#!/bin/sh\nexit 0\n' > "$CARGO_TARGET_DIR/release/homelab-motivation"
    chmod 0700 "$CARGO_TARGET_DIR/release/homelab-motivation"
    exit 0 ;;
  *) exit 2 ;;
esac
CARGO
chmod 0700 "$scratch/bin/go" "$scratch/bin/cargo"

PATH="$scratch/bin:$PATH" bash "$fixture/scripts/stage-release.sh" \
  --component homelab-api "$scratch/api-release" >/dev/null
bash "$root/scripts/verify-release.sh" "$scratch/api-release" >/dev/null
if ! grep -qx "source_revision=$revision" "$scratch/api-release/RELEASE"; then
  printf '%s\n' 'Staged release does not identify the exact tested Git commit' >&2
  exit 1
fi

PATH="$scratch/bin:$PATH" bash "$fixture/scripts/stage-release.sh" \
  --component homelab-motivation "$scratch/motivation-release" >/dev/null
bash "$root/scripts/verify-release.sh" "$scratch/motivation-release" >/dev/null
if [[ ! -x "$scratch/motivation-release/bin/homelab-motivation" ||
      -n "$(find "$scratch/motivation-release/bin" -mindepth 1 -type f ! -name homelab-motivation -print -quit)" ]]; then
  printf '%s\n' 'Rust component stage did not publish the declared executable' >&2
  exit 1
fi
if [[ ! -x "$scratch/api-release/bin/homelab-api" ||
      -n "$(find "$scratch/api-release/bin" -mindepth 1 -type f ! -name homelab-api -print -quit)" ]] ||
    ! grep -qx 'component=homelab-api' "$scratch/api-release/RELEASE"; then
  printf '%s\n' 'Component stage did not publish the declared executable' >&2
  exit 1
fi

# An unexpected executable must fail the checksum-envelope admission after
# permissions have become read-only, without leaving a partial stage.
if HOMELAB_RELEASE_TEST_EXTRA=1 PATH="$scratch/bin:$PATH" \
    bash "$fixture/scripts/stage-release.sh" --component homelab-mcp \
    "$scratch/rejected-release" >/dev/null 2>&1; then
  printf '%s\n' 'Stage admitted a release with an undeclared executable' >&2
  exit 1
fi
if [[ -e "$scratch/rejected-release" ||
      -n "$(find "$scratch" -maxdepth 1 -name '.homelab-release.*' -print -quit)" ]]; then
  printf '%s\n' 'Failed stage left an incomplete release directory' >&2
  exit 1
fi

# A builder that edits a tracked source file cannot publish an artifact.
if HOMELAB_RELEASE_TEST_MUTATE_SOURCE=1 PATH="$scratch/bin:$PATH" \
    bash "$fixture/scripts/stage-release.sh" --component homelab-api \
    "$scratch/mutated-release" >/dev/null 2>&1; then
  printf '%s\n' 'Stage accepted a build that mutated its pinned source' >&2
  exit 1
fi
if [[ -e "$scratch/mutated-release" ]]; then
  printf '%s\n' 'Rejected source mutation left a published artifact' >&2
  exit 1
fi

# Every successful and failed stage must release its detached Git worktree.
if [[ -n "$(find "$scratch" -maxdepth 1 \
    \( -name '.homelab-release.*' -o -name '.homelab-source.*' \) -print -quit)" ||
      "$(git -C "$fixture" worktree list --porcelain | grep -c '^worktree ')" != 1 ]]; then
  printf '%s\n' 'Release staging left a scratch directory or registered worktree' >&2
  exit 1
fi
printf '%s\n' 'Component staging and failure cleanup checks passed.'
