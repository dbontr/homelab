#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
activation="$root/saturn-detached-activation.sh"
bash -n "$activation"
if (( EUID != 0 )); then
  printf '%s\n' 'Saturn credential ownership test requires a root-owned fixture.' >&2
  exit 77
fi
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
unit="$scratch/homelab-executord.service"
secret="$scratch/controller.token"
printf '%s\n' 'test-value-not-a-credential' > "$secret"
chmod 0600 "$secret"
printf '[Service]\nLoadCredential=workspace-token:%s\n' "$secret" > "$unit"

bash "$activation" --check-unit-credentials "$unit" > "$scratch/accepted"
grep -Fxq 'CREDENTIAL_READY homelab-executord.service' "$scratch/accepted"

deny() {
  if bash "$activation" --check-unit-credentials "$unit" > "$scratch/result" 2>&1; then
    printf 'Invalid candidate credential was accepted: %s\n' "$1" >&2
    exit 1
  fi
  if grep -q 'test-value-not-a-credential' "$scratch/result"; then
    printf 'Credential bytes leaked into an error receipt\n' >&2
    exit 1
  fi
}
chmod 0644 "$secret"; deny world_readable
chmod 0660 "$secret"; deny group_writable
chmod 0600 "$secret"
mv "$secret" "$secret.hidden"; deny missing_file
ln -s "$secret.hidden" "$secret"; deny symlink_file
rm "$secret"
mv "$secret.hidden" "$secret"
: > "$secret"; deny empty_file
printf '%s\n' 'test-value-not-a-credential' > "$secret"
printf '[Service]\nLoadCredential=workspace-token:../relative\n' > "$unit"; deny relative_path
printf '[Service]\nLoadCredential=workspace-token:\n' > "$unit"; deny empty_source
printf '[Service]\nLoadCredential=workspace-token:%s\n' "$secret" > "$unit"
bash "$activation" --check-unit-credentials "$unit" >/dev/null

printf '%s\n' 'Saturn release rejects absent, exposed, linked or malformed systemd credential sources before activation.'
