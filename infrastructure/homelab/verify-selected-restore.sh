#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  printf 'Usage: verify-selected-restore.sh [--require-learning] [--require-legacy] ARCHIVE.tar.age\n' >&2
  exit 2
}

require_learning=false
require_legacy=false
archive=
for arg in "$@"; do
  case "$arg" in
    --require-learning) require_learning=true ;;
    --require-legacy) require_legacy=true ;;
    --*) usage ;;
    *) [[ -z $archive ]] || usage; archive=$arg ;;
  esac
done
[[ -n $archive ]] || usage
[[ -f $archive && ! -L $archive && $archive == *.tar.age ]] ||
  { echo 'Archive must be a regular, non-symlink age archive' >&2; exit 2; }

age_bin=${RECOVERY_REMOTE_AGE:-/root/.config/saturn-recovery/age-pkg/bin/age}
identity=${RECOVERY_REMOTE_IDENTITY:-/root/.config/saturn-recovery/identity.txt}
for tool in tar sha256sum gzip git sqlite3 xxd mktemp grep stat df awk; do
  command -v "$tool" >/dev/null ||
    { printf 'Missing restore tool: %s\n' "$tool" >&2; exit 2; }
done
[[ -x $age_bin && -s $identity ]] ||
  { echo 'Decryption executable or identity is unavailable' >&2; exit 2; }

scratch_root=${RECOVERY_RESTORE_SCRATCH:-/srv/saturn-encrypted-backups/selected}
[[ -d $scratch_root && ! -L $scratch_root ]] ||
  { echo 'Recovery scratch root is unavailable' >&2; exit 2; }
archive_bytes=$(stat -c %s "$archive")
free_kb=$(df -Pk "$scratch_root" | awk 'NR==2 { print $4 }')
[[ $free_kb =~ ^[0-9]+$ ]] || { echo 'Cannot measure recovery free space' >&2; exit 2; }
if (( free_kb * 1024 < archive_bytes + 1073741824 )); then
  echo 'Insufficient free space for recovery rehearsal' >&2
  exit 2
fi
scratch=$(mktemp -d "$scratch_root/.selected-restore.XXXXXXXX")
trap 'rm -rf -- "$scratch"' EXIT

"$age_bin" -d -i "$identity" "$archive" | tar -C "$scratch" --no-same-owner -xf -
cd "$scratch"
[[ -f MANIFEST && -f SHA256SUMS && -f flake.bundle &&
   -f flake-working-tree.tar && -f recipesage.sql.gz ]] ||
  { echo 'Required recovery members are missing' >&2; exit 1; }
grep -qx 'schema=1' MANIFEST
sha256sum --check --quiet SHA256SUMS
gzip -t recipesage.sql.gz
tar -tf flake-working-tree.tar >/dev/null
mkdir -m 0700 git-check
git -C git-check init -q
git -C git-check bundle verify "$scratch/flake.bundle" >/dev/null

if [[ $require_learning == true && ! -s learning.sqlite3 ]]; then
  echo 'Learning database was required but was not archived' >&2
  exit 1
fi
if [[ $require_legacy == true && ! -s legacy-knowledge.tar.gz ]]; then
  echo 'Legacy knowledge was required but was not archived' >&2
  exit 1
fi
if [[ -f legacy-knowledge.tar.gz ]]; then
  gzip -t legacy-knowledge.tar.gz
  tar -tzf legacy-knowledge.tar.gz >/dev/null
fi

if [[ -f learning.sqlite3 ]]; then
  [[ ! -L learning.sqlite3 ]] ||
    { echo 'Restored learning database is a symbolic link' >&2; exit 1; }
  [[ $(sqlite3 -readonly learning.sqlite3 'PRAGMA integrity_check;') == ok ]] ||
    { echo 'Restored learning database failed SQLite integrity' >&2; exit 1; }
  attachment_count=0
  attachment_table=$(sqlite3 -readonly learning.sqlite3 \
    "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='attachments';")
  if [[ $attachment_table == 1 ]]; then
    # Hex encoding keeps SQLite blob boundaries intact while the SHA-256
    # verification remains external to the database engine.
    attachment_rows=$(mktemp "$scratch/.attachments.XXXXXXXX")
    sqlite3 -readonly -separator '|' learning.sqlite3 \
      'SELECT hex(data), lower(sha256), size_bytes FROM attachments;' > "$attachment_rows" ||
      { echo 'Cannot read learning attachments' >&2; exit 1; }
    while IFS='|' read -r encoded expected_digest expected_bytes; do
      [[ $expected_digest =~ ^[0-9a-f]{64}$ && $expected_bytes =~ ^[0-9]+$ ]] ||
        { echo 'Invalid attachment metadata' >&2; exit 1; }
      [[ $encoded =~ ^([0-9A-F][0-9A-F])*$ ]] ||
        { echo 'Invalid attachment hex encoding' >&2; exit 1; }
      (( ${#encoded} / 2 == expected_bytes )) ||
        { echo 'Attachment size mismatch' >&2; exit 1; }
      computed_digest=$(printf '%s' "$encoded" | xxd -r -p | sha256sum)
      computed_digest=${computed_digest%% *}
      [[ $computed_digest == "$expected_digest" ]] ||
        { echo 'Attachment SHA-256 mismatch' >&2; exit 1; }
      attachment_count=$((attachment_count + 1))
    done < "$attachment_rows"
    rm -f -- "$attachment_rows"
  fi
  printf 'Learning SQLite verified; original attachments checked: %d\n' "$attachment_count"
fi

printf 'Independent recovery content verification passed: %s\n' "$(basename "$archive")"
