#!/usr/bin/env bash
# Prepare a state-bound migration receipt while the security authority is stopped.
# The credential copies are confined to tmpfs and removed before returning.
set -euo pipefail
umask 077

[[ $# -eq 1 && ( $1 == rust || $1 == go ) && $EUID -eq 0 ]] || {
  echo 'usage (as root): saturn-security-state-cutover.sh rust|go' >&2; exit 2;
}
! systemctl is-active --quiet homelab-securityd || {
  echo 'Stop homelab-securityd to release its single-writer state lease.' >&2; exit 1;
}
target=$1
state=/var/lib/homelab-securityd
secret_dir=/srv/homelab/secrets/security
snapshot_parent=/var/lib/homelab-security-cutovers
runtime=$(mktemp -d /run/homelab-security-migration.XXXXXXXX)
trap 'rm -rf -- "$runtime"' EXIT
environment=$(systemctl show -P Environment homelab-securityd)
for entry in $environment; do
  case "$entry" in
    HOMELAB_SECURITY_OWNER=*|HOMELAB_SECURITY_POLICY_FILE=*|HOMELAB_SECURITY_ACCESS_ISSUER=*|HOMELAB_SECURITY_ACCESS_GATEWAY_AUD=*)
      export "$entry"
      ;;
  esac
done
[[ $HOMELAB_SECURITY_OWNER == owner && -f $HOMELAB_SECURITY_POLICY_FILE ]] || {
  echo 'Installed security policy is unavailable.' >&2; exit 1;
}
install -d -o homelab-security -g homelab-security -m 0700 "$runtime"
copy_credential() {
  [[ -f $1 && ! -L $1 ]] || { echo 'Protected credential unavailable.' >&2; exit 1; }
  install -o homelab-security -g homelab-security -m 0600 -- "$1" "$runtime/$2"
}
copy_credential "$secret_dir/owner-verifier.json" password
copy_credential "$secret_dir/owner-totp" totp
copy_credential "$secret_dir/vault.key" vault-key
copy_credential "$secret_dir/access-jwks.json" access-jwks
copy_credential /srv/homelab/secrets/adaptive-access-owner.email access-owner

[[ -d $state && ! -L $state ]] || { echo 'Security state unavailable.' >&2; exit 1; }
bash_bin=$(readlink -f "$(command -v bash)")
if [[ ! -e $state/authenticator.json || ! -e $state/credentials.json ]]; then
  [[ ! -e $state/.authority-selection.json ]] || {
    echo 'Cannot recreate missing state after selection.' >&2; exit 1;
  }
  if [[ ! -e $state/authenticator.json ]]; then
    runuser -u homelab-security -- "$bash_bin" -c \
      'umask 077; set -C; printf "%s" "{\"version\":1,\"last_totp_step\":-1,\"failures\":0,\"locked_until\":\"0001-01-01T00:00:00Z\"}" > "$1/authenticator.json"' \
      _ "$state"
  fi
  if [[ ! -e $state/credentials.json ]]; then
    runuser -u homelab-security -- "$bash_bin" -c \
      'umask 077; set -C; printf "%s" "{}" > "$1/credentials.json"' \
      _ "$state"
  fi
fi
install -d -o homelab-security -g homelab-security -m 0700 "$snapshot_parent"
snapshot="$snapshot_parent/$(date -u +%Y%m%dT%H%M%SZ)-$target-$$"
migrator=/tmp/rust-security-authority/bin/security-migrate
[[ -x $migrator ]] || { echo 'Rust migrator is missing.' >&2; exit 1; }
if [[ $target == go ]]; then
  [[ -v HOMELAB_SECURITY_GO_PREFLIGHT && -x $HOMELAB_SECURITY_GO_PREFLIGHT ]] || {
    echo 'Go rollback requires HOMELAB_SECURITY_GO_PREFLIGHT.' >&2; exit 1;
  }
  receipt="$secret_dir/authority-go-rollback.json"
else
  receipt="$secret_dir/authority-rust-cutover.json"
fi
[[ ! -e $receipt && ! -L $receipt ]] || {
  echo 'Receipt exists: refusing to replace without review.' >&2; exit 1;
}
runuser -u homelab-security -- env \
  HOMELAB_SECURITY_OWNER="$HOMELAB_SECURITY_OWNER" \
  HOMELAB_SECURITY_POLICY_FILE="$HOMELAB_SECURITY_POLICY_FILE" \
  HOMELAB_SECURITY_PASSWORD_FILE="$runtime/password" \
  HOMELAB_SECURITY_TOTP_FILE="$runtime/totp" \
  HOMELAB_SECURITY_VAULT_KEY_FILE="$runtime/vault-key" \
  HOMELAB_SECURITY_ACCESS_JWKS_FILE="$runtime/access-jwks" \
  HOMELAB_SECURITY_ACCESS_OWNER_FILE="$runtime/access-owner" \
  HOMELAB_SECURITY_ACCESS_ISSUER="$HOMELAB_SECURITY_ACCESS_ISSUER" \
  HOMELAB_SECURITY_ACCESS_GATEWAY_AUD="$HOMELAB_SECURITY_ACCESS_GATEWAY_AUD" \
  HOMELAB_SECURITY_GO_PREFLIGHT="$(printenv HOMELAB_SECURITY_GO_PREFLIGHT || :)" \
  "$migrator" prepare --target "$target" --state-dir "$state" --snapshot-dir "$snapshot"
install -o root -g root -m 0600 -- "$snapshot/authority-cutover.json" "$receipt"
echo 'Protected migration receipt installed; active authentication state was not restored.'
