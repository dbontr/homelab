#!/usr/bin/env bash
set -euo pipefail
umask 077

readonly credential_dir=/srv/homelab/secrets
readonly credential_file="$credential_dir/compute-controller-admin.token"

fail() {
  printf 'Compute credential provisioning failed: %s\n' "$1" >&2
  exit 1
}

if (( $# != 0 )); then
  fail 'no arguments are accepted'
fi
if (( EUID != 0 )); then
  fail 'run as root on Saturn'
fi
if [[ $(hostname -s) != saturn ]]; then
  fail 'run on the Saturn controller only'
fi

for directory in /srv /srv/homelab "$credential_dir"; do
  if [[ ! -d "$directory" || -L "$directory" ]]; then
    fail 'a protected credential directory is absent or is a symbolic link'
  fi
  owner=$(stat -c %u -- "$directory") || fail 'cannot inspect directory owner'
  permissions=$(stat -c %a -- "$directory") || fail 'cannot inspect directory permissions'
  if [[ $owner != 0 ]] || (( (8#$permissions & 0022) != 0 )); then
    fail 'credential directory ownership or permissions are unsafe'
  fi
done
if [[ -e "$credential_file" || -L "$credential_file" ]]; then
  fail 'the protected credential already exists; refusing to replace it'
fi

command -v kubectl >/dev/null || fail 'kubectl is unavailable'
command -v base64 >/dev/null || fail 'base64 is unavailable'

temporary=$(mktemp "$credential_dir/.compute-controller-admin.token.XXXXXXXX") ||
  fail 'cannot create the protected temporary file'
trap 'rm -f -- "$temporary"' EXIT

if ! kubectl --kubeconfig=/etc/rancher/k3s/k3s.yaml --request-timeout=20s -n pwm-system get secret pwm-controller-credentials \
    -o 'jsonpath={.data.admin_token}' | base64 --decode > "$temporary"; then
  fail 'the Kubernetes controller credential could not be read'
fi

if ! LC_ALL=C awk '
  NR > 1 { invalid = 1 }
  NR == 1 && ($0 !~ /^[0-9a-fA-F]+$/ || length($0) < 32 || length($0) > 8192) { invalid = 1 }
  END { exit(invalid || NR != 1) }
' "$temporary"; then
  fail 'the Kubernetes credential does not match the expected token format'
fi

chown root:root -- "$temporary"
chmod 0600 -- "$temporary"
if ! ln -- "$temporary" "$credential_file"; then
  fail 'the protected destination could not be created'
fi
rm -- "$temporary"
trap - EXIT
printf 'Protected Compute controller credential installed on Saturn. No secret was printed.\n'
