#!/usr/bin/env bash
set -euo pipefail
umask 077

backup_root=/srv/homelab/backups
recipient_file=/srv/homelab/secrets/offhost-age-recipient.txt
ssh_key=/root/.ssh/saturn-homelab-ed25519
target=root@10.77.30.232
remote_dir=/srv/saturn-encrypted-backups
remote_age=/root/.config/saturn-recovery/age-pkg/bin/age
remote_identity=/root/.config/saturn-recovery/identity.txt

shopt -s nullglob
latest=
for candidate in "$backup_root"/20??????T??????Z; do
  if [[ -f "$candidate/complete" ]]; then
    latest=$candidate
  fi
done
if [[ -z "$latest" ]]; then
  echo "No completed Saturn backup is available" >&2
  exit 1
fi

stamp=${latest##*/}
if [[ ! $stamp =~ ^20[0-9]{6}T[0-9]{6}Z$ ]]; then
  echo "Backup name is invalid" >&2
  exit 1
fi

(cd "$latest" && sha256sum -c --quiet SHA256SUMS)

ssh_args=(-i "$ssh_key" -o BatchMode=yes -o IdentitiesOnly=yes
          -o StrictHostKeyChecking=yes -o ConnectTimeout=10
          -o ServerAliveInterval=15 -o ServerAliveCountMax=3
          -o Compression=no)
remote_file="$remote_dir/$stamp.tar.age"
if ! ssh "${ssh_args[@]}" "$target" "test -s '$remote_file'"; then
  tar -C "$backup_root" -cf - "$stamp" \
    | age -R "$recipient_file" \
    | ssh -T "${ssh_args[@]}" "$target" \
      "set -e; umask 077; mkdir -p '$remote_dir'; chmod 0700 '$remote_dir'; cat > '$remote_file.partial'; mv '$remote_file.partial' '$remote_file'"
fi

ssh "${ssh_args[@]}" "$target" \
  "set -o pipefail; '$remote_age' -d -i '$remote_identity' '$remote_file' | tar -tf - > /dev/null"
echo "Verified encrypted off-host backup: $stamp"
