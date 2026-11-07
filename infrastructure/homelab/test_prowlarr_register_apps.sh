#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source "$script_dir/prowlarr-register-apps.sh"
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
cat > "$scratch/config.xml" <<'XML'
<Config><ApiKey>abc123</ApiKey></Config>
XML
[[ $(api_key "$scratch/config.xml") == abc123 ]]
sed -i s/abc123/not-valid!/ "$scratch/config.xml"
if ( api_key "$scratch/config.xml" >/dev/null 2>&1 ); then
  echo 'Malformed API key was accepted' >&2
  exit 1
fi
printf 'Prowlarr native-key tests passed\n'
