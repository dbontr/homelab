#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
scratch=$(mktemp -d)
trap 'chmod -R u+w -- "$scratch"; rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin"

# These local stubs exercise the stage/verify transaction without compiling
# code, contacting a registry, or changing host configuration.
cat > "$scratch/bin/git" <<'GIT'
#!/bin/sh
case " $* " in
  *" rev-parse "*) printf '%s\n' '0123456789abcdef0123456789abcdef01234567' ;;
  *" status "*) exit 0 ;;
  *) exit 2 ;;
esac
GIT
cat > "$scratch/bin/go" <<'GO'
#!/bin/sh
case "${1-}" in
  test|vet) exit 0 ;;
  build)
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
    [ -n "${CARGO_TARGET_DIR:-}" ] || exit 2
    mkdir -p "$CARGO_TARGET_DIR/release"
    printf '#!/bin/sh\nexit 0\n' > "$CARGO_TARGET_DIR/release/homelab-motivation"
    chmod 0700 "$CARGO_TARGET_DIR/release/homelab-motivation"
    exit 0 ;;
  *) exit 2 ;;
esac
CARGO
chmod 0700 "$scratch/bin/git" "$scratch/bin/go" "$scratch/bin/cargo"

PATH="$scratch/bin:$PATH" bash "$root/scripts/stage-release.sh" \
  --component homelab-api "$scratch/api-release" >/dev/null
bash "$root/scripts/verify-release.sh" "$scratch/api-release" >/dev/null

PATH="$scratch/bin:$PATH" bash "$root/scripts/stage-release.sh" \
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
    bash "$root/scripts/stage-release.sh" --component homelab-mcp \
    "$scratch/rejected-release" >/dev/null 2>&1; then
  printf '%s\n' 'Stage admitted a release with an undeclared executable' >&2
  exit 1
fi
if [[ -e "$scratch/rejected-release" ||
      -n "$(find "$scratch" -maxdepth 1 -name '.homelab-release.*' -print -quit)" ]]; then
  printf '%s\n' 'Failed stage left an incomplete release directory' >&2
  exit 1
fi

printf '%s\n' 'Component staging and failure cleanup checks passed.'
