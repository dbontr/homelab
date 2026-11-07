#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
config="$scratch/Moonfin.Server.xml"
cat > "$config" <<'XML'
<?xml version="1.0"?>
<PluginConfiguration>
  <EnableSettingsSync>true</EnableSettingsSync>
  <SeerrEnabled>false</SeerrEnabled>
  <SeerrWebhookSecret>private-existing-secret</SeerrWebhookSecret>
  <GamesEnabled>true</GamesEnabled>
</PluginConfiguration>
XML
cp -- "$config" "$scratch/original.xml"
[[ $(bash "$script_dir/configure_moonbase.sh" "$config") == 'Configuration requires update' ]]
cmp "$config" "$scratch/original.xml"
[[ $(bash "$script_dir/configure_moonbase.sh" "$config" --apply) == 'Configuration changed' ]]
[[ $(bash "$script_dir/configure_moonbase.sh" "$config" --apply) == 'Configuration already correct' ]]
[[ $(xmlstarlet sel -t -v '/PluginConfiguration/SeerrUrl' "$config") == 'http://saturn-seerr:5055' ]]
[[ $(xmlstarlet sel -t -v '/PluginConfiguration/SeerrWebhookSecret' "$config") == 'private-existing-secret' ]]
sed -i 's|http://saturn-seerr:5055|https://unapproved.example|' "$config"
if bash "$script_dir/configure_moonbase.sh" "$config" --apply >/dev/null 2>&1; then
  echo 'Custom Seerr URL was overwritten' >&2
  exit 1
fi
printf 'Moonbase configuration tests passed\n'
