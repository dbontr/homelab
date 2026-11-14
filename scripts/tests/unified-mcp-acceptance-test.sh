#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin"
export MOCK_TOKEN='fixture-unified-mcp-token-0123456789abcdef0123456789'
printf '%s\n' "$MOCK_TOKEN" > "$scratch/token"

cat > "$scratch/bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
output=''
header_file=''
previous=''
for arg in "$@"; do
  if [[ "$arg" == *"$MOCK_TOKEN"* ]]; then
    printf 'Bearer token present in curl arguments\n' >&2
    exit 90
  fi
  case "$previous" in
    --header)
      if [[ "$arg" == @* ]]; then header_file=${arg#@}; fi ;;
    --output) output="$arg" ;;
  esac
  previous="$arg"
done
[[ -n "$output" ]] || exit 91
request=$(cat)
if [[ -z "$header_file" ||
      "$(<"$header_file")" != "Authorization: Bearer $MOCK_TOKEN" ]]; then
  printf '{}\n' > "$output"
  printf 401
  exit 0
fi

case "$request" in
  *'"method":"initialize"'*)
    printf '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"homelab"}}}\n' > "$output" ;;
  *'"method":"tools/list"'*)
    printf '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"task_add","inputSchema":{}},{"name":"resource_resolve","inputSchema":{}}]}}\n' > "$output" ;;
  *'"method":"ping"'*)
    printf '{"jsonrpc":"2.0","id":3,"result":{}}\n' > "$output" ;;
  *) printf 'Unexpected MCP request\n' >&2; exit 92 ;;
esac
printf 200
MOCK
chmod 700 "$scratch/bin/curl"

PATH="$scratch/bin:$PATH" bash "$root/scripts/accept-unified-mcp.sh" \
  'http://127.0.0.1:8401/mcp' "$scratch/token" task_add > "$scratch/accepted"
grep -q '^PASS: ' "$scratch/accepted"
if grep -Fq "$MOCK_TOKEN" "$scratch/accepted"; then
  printf 'Acceptance output exposed the bearer token\n' >&2
  exit 1
fi

if PATH="$scratch/bin:$PATH" bash "$root/scripts/accept-unified-mcp.sh" \
    'http://127.0.0.1:8401/mcp' "$scratch/token" nonexistent_tool \
    > "$scratch/denied" 2>&1; then
  printf 'Unknown required MCP tool was accepted\n' >&2
  exit 1
fi
printf 'Unified MCP acceptance contract verified\n'
