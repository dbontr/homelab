#!/usr/bin/env bash
set -euo pipefail
umask 077

flake_repo=${RECOVERY_FLAKE_REPO:-/etc/nixos}
backup_root=${RECOVERY_BACKUP_ROOT:-/srv/homelab/selected-backups}
recipient_file=${RECOVERY_AGE_RECIPIENT:-/srv/homelab/secrets/offhost-age-recipient.txt}
remote_target=${RECOVERY_REMOTE_TARGET:-root@10.77.30.232}
remote_root=${RECOVERY_REMOTE_ROOT:-/srv/saturn-encrypted-backups/selected}
remote_age=${RECOVERY_REMOTE_AGE:-/root/.config/saturn-recovery/age-pkg/bin/age}
remote_identity=${RECOVERY_REMOTE_IDENTITY:-/root/.config/saturn-recovery/identity.txt}
ssh_key=${RECOVERY_SSH_KEY:-/root/.ssh/saturn-homelab-ed25519}
learning_db=${RECOVERY_LEARNING_DB:-}
legacy_knowledge=${RECOVERY_LEGACY_KNOWLEDGE:-}

for command_name in age git docker gzip tar sha256sum ssh flock sqlite3; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Missing recovery tool: %s\n' "$command_name" >&2
    exit 1
  }
done

[[ -s $recipient_file ]] || { echo 'Missing age recipient file' >&2; exit 1; }
[[ -f $ssh_key ]] || { echo 'Missing SSH identity' >&2; exit 1; }
[[ $(git -C "$flake_repo" rev-parse --is-inside-work-tree 2>/dev/null) == true ]] || { echo 'Flake repository is not a Git checkout' >&2; exit 1; }
[[ $(git -C "$flake_repo" rev-parse --is-shallow-repository) == false ]] || { echo 'Cannot make complete bundle from shallow clone' >&2; exit 1; }
# Preserve the working tree separately from the complete Git history. This includes
# tracked modifications and untracked, nonignored files without copying ignored secrets.
# A Git bundle alone cannot recover uncommitted configuration.


mkdir -p "$backup_root"
chmod 0700 "$backup_root"
exec 9>"$backup_root/.lock"
flock -n 9 || { echo 'Another selected backup is running' >&2; exit 1; }
stamp=$(date -u +%Y%m%dT%H%M%SZ)
archive="$backup_root/$stamp.tar.age"
[[ ! -e $archive ]] || { echo 'Archive already exists' >&2; exit 1; }
staging=$(mktemp -d "$backup_root/.staging.XXXXXXXX")
partial="$backup_root/.$stamp.partial"
trap 'rm -rf -- "$staging"; rm -f -- "$partial"' EXIT

git -C "$flake_repo" bundle create "$staging/flake.bundle" --all
git -C "$flake_repo" bundle verify "$staging/flake.bundle" >/dev/null
git -C "$flake_repo" status --porcelain --untracked-files=normal > "$staging/flake-status.txt"
git -C "$flake_repo" ls-files -z --cached --others --exclude-standard |
  tar -C "$flake_repo" --null -T - -cf "$staging/flake-working-tree.tar"
tar -tf "$staging/flake-working-tree.tar" >/dev/null
commit=$(git -C "$flake_repo" rev-parse HEAD)
printf 'schema=1\nflake_commit=%s\ncreated_utc=%s\n' "$commit" "$stamp" > "$staging/MANIFEST"

docker exec recipesage_postgres pg_dumpall -U recipesage_selfhost | gzip -c > "$staging/recipesage.sql.gz"
gzip -t "$staging/recipesage.sql.gz"
printf 'recipesage=postgresql\n' >> "$staging/MANIFEST"

if [[ -n $learning_db ]]; then
  [[ -f $learning_db && ! -L $learning_db ]] ||
    { echo 'Learning database must be a regular file' >&2; exit 1; }
  sqlite3 -readonly "$learning_db" ".backup '$staging/learning.sqlite3'"
  [[ $(sqlite3 -readonly "$staging/learning.sqlite3" 'PRAGMA integrity_check;') == ok ]] ||
    { echo 'Learning database integrity check failed' >&2; exit 1; }
  printf 'learning=sqlite3\n' >> "$staging/MANIFEST"
else
  printf 'learning=not_configured\n' >> "$staging/MANIFEST"
fi

if [[ -n $legacy_knowledge ]]; then
  [[ -d $legacy_knowledge && ! -L $legacy_knowledge ]] || { echo 'Missing legacy knowledge directory' >&2; exit 1; }
  tar -C "$(dirname "$legacy_knowledge")" -czf "$staging/legacy-knowledge.tar.gz" "$(basename "$legacy_knowledge")"
  gzip -t "$staging/legacy-knowledge.tar.gz"
  printf 'legacy_project_knowledge=directory\n' >> "$staging/MANIFEST"
else
  printf 'legacy_project_knowledge=not_included\n' >> "$staging/MANIFEST"
fi

(
  cd "$staging"
  sha256sum MANIFEST flake.bundle flake-working-tree.tar flake-status.txt recipesage.sql.gz > SHA256SUMS
  if [[ -f learning.sqlite3 ]]; then sha256sum learning.sqlite3 >> SHA256SUMS; fi
  if [[ -f legacy-knowledge.tar.gz ]]; then sha256sum legacy-knowledge.tar.gz >> SHA256SUMS; fi
  sha256sum -c --quiet SHA256SUMS
)
tar -C "$staging" -cf - . | age -R "$recipient_file" > "$partial"
chmod 0600 "$partial"
mv -- "$partial" "$archive"
local_hash=$(sha256sum "$archive")
local_hash=${local_hash%% *}
ssh_args=(-i "$ssh_key" -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=3)
remote_archive="$remote_root/$stamp.tar.age"
ssh -T "${ssh_args[@]}" "$remote_target" \
  "set -eu; umask 077; mkdir -p '$remote_root'; chmod 0700 '$remote_root'; cat > '$remote_archive.partial'; mv '$remote_archive.partial' '$remote_archive'" < "$archive"
remote_hash=$(ssh "${ssh_args[@]}" "$remote_target" "sha256sum '$remote_archive' | cut -d ' ' -f1")
[[ $remote_hash == "$local_hash" ]] || { echo 'Encrypted archive hash mismatch on recovery host' >&2; exit 1; }
ssh "${ssh_args[@]}" "$remote_target" \
  "set -o pipefail; '$remote_age' -d -i '$remote_identity' '$remote_archive' | tar -tf - >/dev/null"
printf 'Encrypted recovery archive verified on recovery host: %s\n' "$stamp"
