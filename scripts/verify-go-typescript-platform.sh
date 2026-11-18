#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 --backend | --full" >&2
  exit 2
}

[[ $# -eq 1 ]] || usage
case "$1" in
  --backend|--full) ;;
  *) usage ;;
esac

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"
if [[ -n "$(git status --porcelain)" ]]; then
  echo "The migration source checkout must be clean and exact" >&2
  exit 1
fi

source_sha="$(git rev-parse --verify HEAD)"
if [[ ! "$source_sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo "An immutable Git commit is required" >&2
  exit 1
fi
printf 'source=%s\n' "$source_sha"

for module in services/core services/compute; do
  printf 'go_test=%s\n' "$module"
  ( cd "$root/$module" && go test ./... )
done

if command -v nix >/dev/null 2>&1; then
  for setting in services.homelabSecurity.implementation services.homelabExecutor.implementation; do
    actual="$(nix eval --raw ".#nixosConfigurations.saturn.config.$setting")"
    if [[ "$actual" != go ]]; then
      printf 'Go migration selection is not active in source: %s=%s\n' "$setting" "$actual" >&2
      exit 1
    fi
  done
  actuator="$(nix eval --json .#nixosConfigurations.saturn.config.services.homelabExecutor.useRustActuator)"
  if [[ "$actuator" != false ]]; then
    echo "Go migration must not depend on the Rust actuator" >&2
    exit 1
  fi
  nix flake check --offline --no-build
else
  echo "NixOS evaluation skipped: nix not installed" >&2
fi

if [[ "$1" == --backend ]]; then
  echo "Go backend source checks passed; browser and live cutover unverified"
  exit 0
fi

web="$root/ui/web"
if [[ ! -s "$web/package-lock.json" ]]; then
  echo "Browser qualification denied: committed npm lockfile missing" >&2
  exit 1
fi
command -v npm >/dev/null 2>&1 || {
  echo "Browser qualification denied: npm unavailable" >&2
  exit 1
}
( cd "$web" && npm ci --ignore-scripts --no-audit --no-fund && npm run typecheck && npm run build )
echo "Go and browser source checks passed; production release is not authorized"
