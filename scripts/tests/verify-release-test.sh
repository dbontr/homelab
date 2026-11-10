#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
verify="$root/scripts/verify-release.sh"
temporary=$(mktemp -d)
trap 'chmod -R u+w -- "$temporary"; rm -rf -- "$temporary"' EXIT
counter=0

make_release() {
  counter=$((counter + 1))
  release="$temporary/release-$counter"
  mkdir -p "$release/bin" "$release/console/assets"
  for binary in \
    homelab-platform homelab-calendard homelab-engineeringd homelab-securityd \
    homelab-executord homelab-release homelab-admin homelab-compute-controller \
    homelab-compute-worker homelab-learning homelab-health homelab-motivation \
    homelab-mcp homelab-api; do
    printf '#!/bin/sh\nprintf "version"\n' > "$release/bin/$binary"
    chmod 0700 "$release/bin/$binary"
  done
  printf '<!doctype html><title>Homelab</title>\n' > "$release/console/index.html"
  printf 'asset\n' > "$release/console/assets/site.css"
  printf 'source_revision=%040d\nbuild_utc=2026-11-09T18:00:00Z\n' 0 > "$release/RELEASE"
  (
    cd "$release"
    find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
  )
}

make_component_release() {
  counter=$((counter + 1))
  release="$temporary/release-$counter"
  mkdir -p "$release/bin" "$release/console"
  component=$1
  if [[ "$component" == homelab-console ]]; then
    printf '<!doctype html><title>Console</title>\n' > "$release/console/index.html"
  else
    printf '#!/bin/sh\nexit 0\n' > "$release/bin/$component"
    chmod 0700 "$release/bin/$component"
  fi
  printf 'source_revision=%040d\nbuild_utc=2026-11-09T18:00:00Z\ncomponent=%s\n' 0 "$component" > "$release/RELEASE"
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

for component in homelab-platform homelab-calendard homelab-api homelab-compute-worker homelab-motivation homelab-console; do
  make_component_release "$component"
  bash "$verify" "$release" >/dev/null
done

make_component_release homelab-api
printf 'injected\n' > "$release/bin/homelab-mcp"
chmod 0700 "$release/bin/homelab-mcp"
(
  cd "$release"
  find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
)
should_reject 'extra executable in component release'

make_component_release homelab-api
printf 'asset\n' > "$release/console/index.html"
(
  cd "$release"
  find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
)
should_reject 'console payload in native component release'

make_component_release homelab-console
printf '#!/bin/sh\n' > "$release/bin/homelab-api"
chmod 0700 "$release/bin/homelab-api"
(
  cd "$release"
  find bin console -type f -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
)
should_reject 'native executable in console release'

make_component_release homelab-mystery
should_reject 'unknown component identity'

make_component_release homelab-api
printf 'component=homelab-console\n' >> "$release/RELEASE"
should_reject 'duplicate metadata field'

make_component_release homelab-api
printf 'tampered\n' >> "$release/bin/homelab-api"
should_reject 'tampered component executable'

printf '%s\n' 'Release bundle integrity checks passed.'
