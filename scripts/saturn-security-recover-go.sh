#!/usr/bin/env bash
# Recover the Go security authority after a Rust selection marker was committed.
# This preserves current replay/vault state and requires independent Go preflight.
set -euo pipefail
umask 077
[[ "$EUID" -eq 0 && "$#" -eq 0 ]] || {
  echo 'Run as root without arguments.' >&2; exit 2;
}
repo=/srv/homelab-migration-src
state=/var/lib/homelab-securityd
marker="$state/.authority-selection.json"
[[ -f "$marker" && ! -L "$marker" ]] || {
  echo 'No protected authority selection marker.' >&2; exit 1;
}
grep -q '"target":"rust"' "$marker" || {
  echo 'The active authority is not selected as Rust.' >&2; exit 1;
}
go_preflight=$(readlink -f -- /run/homelab-go-preflight)
go_system=$(readlink -f -- /tmp/saturn-security-system)
[[ -x "$go_preflight" && -x "$go_system/bin/switch-to-configuration" ]] || {
  echo 'Reviewed Go preflight and rollback system are required.' >&2; exit 1;
}
systemctl stop homelab-securityd || {
  echo 'Cannot obtain exclusive security state ownership.' >&2; exit 1;
}
export HOMELAB_SECURITY_GO_PREFLIGHT="$go_preflight"
bash "$repo/scripts/saturn-security-state-cutover.sh" go

# Install the approved receipt before the previous Go unit is started.
dropin=/run/systemd/system/homelab-securityd.service.d
install -d -o root -g root -m 0700 "$dropin"
cat > "$dropin/rollback-receipt.conf" <<'CONFIG'
[Service]
LoadCredential=migration-receipt:/srv/homelab/secrets/security/authority-go-rollback.json
Environment=HOMELAB_SECURITY_MIGRATION_RECEIPT_FILE=/run/credentials/homelab-securityd.service/migration-receipt
CONFIG
chmod 0600 "$dropin/rollback-receipt.conf"
systemctl daemon-reload
timeout 420 "$go_system/bin/switch-to-configuration" test
systemctl daemon-reload
systemctl restart homelab-securityd
systemctl is-active --quiet homelab-securityd || {
  echo 'Go security authority failed to recover.' >&2; exit 1;
}
for socket in owner.sock client.sock; do
  [[ -S "/run/homelab-securityd/$socket" ]] || {
    echo 'Recovered authority socket missing.' >&2; exit 1;
  }
done
echo 'Go rollback active with protected receipt. Persistent Go generation requires a declarative receipt selection.'
