#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin"
export MOCK_TOKEN='fixture-bearer-token-0123456789abcdef0123456789'
printf '%s\n' "$MOCK_TOKEN" > "$scratch/token"

cat > "$scratch/bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
for arg in "$@"; do
  if [[ "$arg" == *"$MOCK_TOKEN"* ]]; then
    printf '%s\n' 'Bearer token exposed in curl arguments' >&2
    exit 90
  fi
done
headers=()
output=''
previous=''
for arg in "$@"; do
  case "$previous" in
    --header) headers+=("$arg") ;;
    --output) output="$arg" ;;
  esac
  previous="$arg"
done
[[ -n "$output" ]] || { echo 'Missing output file' >&2; exit 91; }
request=$(cat)
authorized=false
for header in "${headers[@]}"; do
  if [[ "$header" == @* ]]; then
    header_file=${header#@}
    if [[ "$(<"$header_file")" == "Authorization: Bearer $MOCK_TOKEN" ]]; then
      authorized=true
    fi
  fi
done
if [[ "$authorized" != true ]]; then
  printf '{}\n' > "$output"
  printf '401'
  exit 0
fi
case "$request" in
  *'"method":"initialize"'*)
    printf '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"workspace-mcp"}}}\n' > "$output" ;;
  *'"method":"tools/list"'*)
    jq -n '{jsonrpc:"2.0",id:2,result:{tools:([range(1;9)|{name:("tool" + (.|tostring)),inputSchema:{}}]+[{name:"list_projects",inputSchema:{}},{name:"git_provider_capabilities",inputSchema:{}}])}}' > "$output" ;;
  *'"method":"tools/call"'*)
    printf '{"jsonrpc":"2.0","id":3,"result":{"isError":false,"structuredContent":{"provider":"local-git","issues":"unavailable"}}}\n' > "$output" ;;
  *'"method":"ping"'*)
    printf '{"jsonrpc":"2.0","id":4,"result":{}}\n' > "$output" ;;
  *) printf 'Unexpected RPC: %s\n' "$request" >&2; exit 92 ;;
esac
printf '200'
MOCK
chmod 700 "$scratch/bin/curl"
PATH="$scratch/bin:$PATH" bash "$root/services/compute/scripts/mcp-acceptance.sh" \
  'http://127.0.0.1:8401/mcp' "$scratch/token" > "$scratch/result"
grep -q '^PASS: ' "$scratch/result"
if grep -Fq "$MOCK_TOKEN" "$scratch/result"; then
  printf '%s\n' 'Acceptance output exposed the bearer token' >&2
  exit 1
fi
printf '%s\n' 'MCP acceptance token custody verified'
