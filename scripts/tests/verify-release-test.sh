#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
verify="$root/scripts/verify-release.sh"
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
counter=0

make_release() {
  counter=$((counter + 1))
  release="$temporary/release-$counter"
  mkdir -p "$release/bin" "$release/console/assets"
  for binary in \
    homelab-platform homelab-engineeringd homelab-approve homelab-securityd \
    homelab-executord homelab-release homelab-admin homelab-compute-controller \
    homelab-compute-worker homelab-learning homelab-health; do
    printf '#!/bin/sh\nprintf "version"\n' > "$release/bin/$binary"
    chmod 0700 "$release/bin/$binary"
  done
  printf '<!doctype html><title>Eminence</title>\n' > "$release/console/index.html"
  printf 'asset\n' > "$release/console/assets/site.css"
  printf 'source_revision=%040d\nbuild_utc=2026-11-09T18:00:00Z\n' 0 > "$release/RELEASE"
  (
    cd "$release"
    find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
  )
}

should_reject() {
  if bash "$verify" "$release" >/dev/null 2>&1; then
    printf 'Rejected release bundle passed verification: %s\n' "$1" >&2
    exit 1
  fi
}

make_release
bash "$verify" "$release" >/dev/null

make_release
printf 'tampered\n' >> "$release/bin/homelab-platform"
should_reject 'altered executable'

make_release
printf 'unexpected\n' > "$release/console/unknown.txt"
should_reject 'unlisted console asset'

make_release
printf 'source_revision=unknown\nbuild_utc=2026-11-09T18:00:00Z\n' > "$release/RELEASE"
should_reject 'unknown source revision'

make_release
ln -s /etc/passwd "$release/console/private"
should_reject 'linked asset'

make_release
printf 'extra' > "$release/bin/extra-tool"
chmod 0700 "$release/bin/extra-tool"
(
  cd "$release"
  find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
)
should_reject 'undeclared native executable'

make_release
rm -f "$release/bin/homelab-health"
should_reject 'missing declared executable'

make_release
: > "$release/SHA256SUMS"
should_reject 'empty checksum manifest'

make_release
chmod 0666 "$release/console/assets/site.css"
(
  cd "$release"
  find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
)
should_reject 'writable release asset despite matching checksum'

make_release
find "$release/bin" "$release/console" -type f -exec chmod 0444 {} +
find "$release/bin" -type f -exec chmod 0555 {} +
find "$release/bin" "$release/console" -type d -exec chmod 0555 {} +
chmod 0444 "$release/RELEASE" "$release/SHA256SUMS"
chmod 0555 "$release"
bash "$verify" "$release" >/dev/null

printf '%s\n' 'Release bundle integrity checks passed.'
