#!/usr/bin/env bash
set -euo pipefail
umask 077

die() { printf '%s\n' "$*" >&2; exit 1; }
[[ $# == 3 ]] || die 'Usage: knowledge-index-backup.sh snapshot|verify-restore SOURCE DESTINATION'
mode=$1
source_root=$2
destination=$3
[[ ! -L $source_root && ! -L $destination ]] || die 'Symbolic links are not allowed as backup roots'
for tool in sqlite3 jq sha256sum mktemp stat install; do
  command -v "$tool" >/dev/null || die "Missing backup tool: $tool"
done
case "$mode" in snapshot|verify-restore) ;; *) die 'Unsupported backup mode';; esac

if [[ $mode == snapshot ]]; then
  mkdir -p -- "$destination"
  chmod 0700 "$destination"
  records=$(mktemp "$destination/.knowledge-manifest.XXXXXXXX")
  trap 'rm -f -- "$records"' EXIT
  : > "$records"
  shopt -s nullglob
  for owner in "$source_root"/owners/*; do
    [[ -d $owner && ! -L $owner ]] || continue
    identity=${owner##*/}
    [[ $identity =~ ^[0-9a-f]{64}$ ]] || continue
    source_db="$owner/index.sqlite"
    [[ -e $source_db ]] || continue
    [[ -f $source_db && ! -L $source_db ]] || die 'Unsafe owner knowledge database'
    bytes=$(stat -c %s "$source_db")
    [[ $bytes =~ ^[0-9]+$ ]] && (( bytes <= 34359738368 )) ||
      die 'Oversized owner knowledge database'
    output="$destination/$identity.sqlite"
    [[ ! -e $output && ! -L $output ]] || die 'Knowledge snapshot already exists'
    [[ $output != *"'"* ]] || die 'Unsupported apostrophe in SQLite backup path'
    sqlite3 -readonly "$source_db" ".backup '$output'"
    [[ $(sqlite3 -readonly "$output" 'PRAGMA integrity_check;') == ok ]] ||
      die 'Knowledge snapshot failed SQLite integrity check'
    chmod 0600 "$output"
    digest=$(sha256sum "$output")
    digest=${digest%% *}
    jq -nc --arg owner "$identity" --arg db "$identity.sqlite" --arg sha "$digest" \
      '{owner_directory:$owner, database:$db, sha256:$sha}' >> "$records"
  done
  jq -s '.' "$records" > "$destination/manifest.json"
  chmod 0600 "$destination/manifest.json"
  jq -r 'length | "Verified knowledge databases: \(.)"' "$destination/manifest.json"
  exit
fi

manifest="$source_root/manifest.json"
[[ -f $manifest && ! -L $manifest ]] || die 'Missing knowledge snapshot manifest'
jq -e 'type == "array" and all(.[]; (.owner_directory | type == "string") and (.database | type == "string") and (.sha256 | type == "string"))' \
  "$manifest" >/dev/null || die 'Malformed knowledge snapshot manifest'
mkdir -p -- "$destination"
[[ ! -L $destination/owners ]] || die 'Unsafe restored knowledge directory'
mkdir -p -- "$destination/owners"
records=$(mktemp)
trap 'rm -f -- "$records"' EXIT
jq -c '.[]' "$manifest" > "$records"
count=0
while IFS= read -r record; do
  identity=$(jq -r '.owner_directory' <<<"$record")
  database=$(jq -r '.database' <<<"$record")
  expected=$(jq -r '.sha256' <<<"$record")
  [[ $identity =~ ^[0-9a-f]{64}$ && $database == "$identity.sqlite" &&
     $expected =~ ^[0-9a-f]{64}$ ]] || die 'Invalid owner or snapshot identifier'
  snapshot="$source_root/$database"
  [[ -f $snapshot && ! -L $snapshot ]] || die 'Unsafe knowledge snapshot path'
  actual=$(sha256sum "$snapshot")
  [[ ${actual%% *} == "$expected" ]] || die 'Knowledge snapshot checksum mismatch'
  owner="$destination/owners/$identity"
  [[ ! -L $owner ]] || die 'Unsafe restored owner path'
  mkdir -p -m 0700 -- "$owner"
  target="$owner/index.sqlite"
  [[ ! -L $target && ! -L $target-wal && ! -L $target-shm ]] ||
    die 'Unsafe restored SQLite database or journal'
  scratch=$(mktemp "$owner/.knowledge-restore.XXXXXXXX")
  install -m 0600 -- "$snapshot" "$scratch"
  mv -f -- "$scratch" "$target"
  rm -f -- "$target-wal" "$target-shm"
  [[ $(sqlite3 -readonly "$target" 'PRAGMA integrity_check;') == ok ]] ||
    die 'Restored knowledge database is inconsistent'
  sqlite3 -readonly "$target" 'SELECT COUNT(*) FROM sources;' >/dev/null ||
    die 'Restored knowledge source table is unreadable'
  count=$((count + 1))
done < "$records"
printf 'Verified knowledge databases: %d\n' "$count"
