#!/usr/bin/env bash
set -euo pipefail
umask 077

die() { printf '%s\n' "$*" >&2; exit 1; }
usage() { die 'Usage: configure_moonbase.sh CONFIG.xml [--apply]'; }
[[ $# == 1 || $# == 2 ]] || usage
config=$1
apply=false
if [[ $# == 2 ]]; then [[ $2 == --apply ]] || usage; apply=true; fi
[[ -f $config && ! -L $config ]] || die 'Plugin configuration must be a regular file'
command -v xmlstarlet >/dev/null || die 'xmlstarlet is required'
command -v mktemp >/dev/null || die 'mktemp is required'

[[ $(xmlstarlet sel -t -v 'name(/*)' "$config") == PluginConfiguration ]] ||
  die 'Expected Moonbase PluginConfiguration XML'
[[ $(xmlstarlet sel -t -v 'count(/PluginConfiguration/EnableSettingsSync)' "$config") == 1 &&
   $(xmlstarlet sel -t -v 'normalize-space(/PluginConfiguration/EnableSettingsSync)' "$config") == true ]] ||
  die 'Moonbase Settings Sync must be enabled'
[[ $(xmlstarlet sel -t -v 'count(/PluginConfiguration/SeerrEnabled)' "$config") == 1 ]] ||
  die 'Expected one SeerrEnabled field'
flag=$(xmlstarlet sel -t -v 'normalize-space(/PluginConfiguration/SeerrEnabled)' "$config")
[[ $flag == true || $flag == false ]] || die 'Unrecognized SeerrEnabled field'
url_count=$(xmlstarlet sel -t -v 'count(/PluginConfiguration/SeerrUrl)' "$config")
[[ $url_count == 0 || $url_count == 1 ]] || die 'Duplicate SeerrUrl field'
seerr_url=http://saturn-seerr:5055
if [[ $url_count == 1 ]]; then
  current=$(xmlstarlet sel -t -v 'normalize-space(/PluginConfiguration/SeerrUrl)' "$config")
  [[ -z $current || $current == "$seerr_url" ]] ||
    die 'Existing Seerr URL is customized; no changes made'
fi

directory=$(dirname -- "$config")
scratch=$(mktemp "$directory/.moonbase-config.XXXXXXXX")
trap 'rm -f -- "$scratch"' EXIT
if [[ $url_count == 1 ]]; then
  xmlstarlet ed -u '/PluginConfiguration/SeerrEnabled' -v true \
    -u '/PluginConfiguration/SeerrUrl' -v "$seerr_url" "$config" > "$scratch"
else
  xmlstarlet ed -u '/PluginConfiguration/SeerrEnabled' -v true \
    -s '/PluginConfiguration' -t elem -n SeerrUrl -v "$seerr_url" \
    "$config" > "$scratch"
fi
[[ $(xmlstarlet sel -t -v 'normalize-space(/PluginConfiguration/SeerrEnabled)' "$scratch") == true &&
   $(xmlstarlet sel -t -v 'normalize-space(/PluginConfiguration/SeerrUrl)' "$scratch") == "$seerr_url" ]] ||
  die 'Moonbase verification failed'
if cmp -s "$config" "$scratch"; then
  printf 'Configuration already correct\n'
elif [[ $apply == false ]]; then
  printf 'Configuration requires update\n'
else
  # Preserve the original owner and access mode, including secret fields.
  chmod --reference="$config" "$scratch"
  chown --reference="$config" "$scratch"
  mv -f -- "$scratch" "$config"
  printf 'Configuration changed\n'
fi
