#!/usr/bin/env bash
set -euo pipefail
set +x
umask 077

if [[ $# -lt 2 || $# -gt 3 ]]; then
  printf 'Usage: %s MCP_URL BEARER_TOKEN_FILE [REQUIRED_TOOL]\n' "$0" >&2
  exit 2
fi

endpoint=${1%/}
token_file=$2
required_tool=${3:-task_add}

case "$endpoint" in
  https://*/mcp|http://127.0.0.1:*/mcp|http://localhost:*/mcp) ;;
  *) printf 'MCP_URL must be HTTPS or loopback HTTP and end in /mcp.\n' >&2; exit 2 ;;
esac
case "$endpoint" in
  *'@'*|*'?'*|*'#'*|*'\'*)
    printf 'MCP_URL cannot contain user credentials, a query, or a fragment.\n' >&2
    exit 2 ;;
esac
if [[ ! "$required_tool" =~ ^[a-z][a-z0-9_]{0,127}$ ]]; then
  printf 'REQUIRED_TOOL must be a single MCP tool name.\n' >&2
  exit 2
fi
if [[ ! -f "$token_file" || ! -r "$token_file" || -L "$token_file" ]]; then
  printf 'Bearer token file must be a readable regular file, not a symlink.\n' >&2
  exit 2
fi
for program in curl jq; do
  command -v "$program" >/dev/null || { printf 'Missing required command: %s\n' "$program" >&2; exit 2; }
done

token=$(<"$token_file")
if [[ ${#token} -lt 32 || ${#token} -gt 4096 ||
      "$token" == *$'\n'* || "$token" == *$'\r'* ||
      "$token" == *$'\t'* || "$token" == *' '* ]]; then
  printf 'Bearer token file is not a valid protected MCP credential.\n' >&2
  exit 2
fi
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"; unset token' EXIT
chmod 700 "$scratch"
printf 'Authorization: Bearer %s\n' "$token" > "$scratch/auth.header"
unset token

rpc() {
  local payload=$1 response_name=$2 authenticated=$3
  local -a args=(
    --silent --show-error --max-time 25
    --header 'Content-Type: application/json'
    --header 'Accept: application/json'
    --header 'MCP-Protocol-Version: 2025-06-18'
  )
  if [[ "$authenticated" == yes ]]; then
    args+=(--header "@$scratch/auth.header")
  fi
  printf '%s' "$payload" |
    curl "${args[@]}" --data-binary @- --output "$scratch/$response_name.json" \
      --write-out '%{http_code}' "$endpoint"
}

initialize='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"homelab-mcp-acceptance","version":"1.0"}}}'
unauthorized=$(rpc "$initialize" denied no)
if [[ "$unauthorized" != 401 ]]; then
  printf 'FAIL: anonymous MCP initialization returned HTTP %s (expected 401).\n' "$unauthorized" >&2
  exit 1
fi

status=$(rpc "$initialize" initialized yes)
if [[ "$status" != 200 ]] ||
   ! jq -e '.jsonrpc == "2.0" and .id == 1 and
             .result.protocolVersion == "2025-06-18" and
             (.result.serverInfo.name | type) == "string"' \
       "$scratch/initialized.json" >/dev/null; then
  printf 'FAIL: authenticated MCP initialization returned HTTP %s or invalid JSON-RPC.\n' "$status" >&2
  exit 1
fi

status=$(rpc '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' discovered yes)
if [[ "$status" != 200 ]] ||
   ! jq -e --arg required "$required_tool" '
       .jsonrpc == "2.0" and .id == 2 and
       (.result.tools | type) == "array" and
       (.result.tools | length) > 0 and
       all(.result.tools[]; (.name | type) == "string" and (.inputSchema | type) == "object") and
       ([.result.tools[].name] | unique | length) == (.result.tools | length) and
       ([.result.tools[].name] | index($required) != null)
     ' "$scratch/discovered.json" >/dev/null; then
  printf 'FAIL: authenticated tools/list returned HTTP %s or lacks required tool %s.\n' "$status" "$required_tool" >&2
  exit 1
fi

status=$(rpc '{"jsonrpc":"2.0","id":3,"method":"ping","params":{}}' ping yes)
if [[ "$status" != 200 ]] ||
   ! jq -e '.jsonrpc == "2.0" and .id == 3 and .result == {}' \
       "$scratch/ping.json" >/dev/null; then
  printf 'FAIL: authenticated MCP ping returned HTTP %s or invalid JSON-RPC.\n' "$status" >&2
  exit 1
fi


# Read-only and negative tests against the running Streamable HTTP listener.
rpc_custom() {
  local payload=$1 name=$2 accept=$3 version=$4 origin=$5
  if [[ -n "$origin" ]]; then
    printf '%s' "$payload" | curl -sS --max-time 25 -H 'Content-Type: application/json' -H "Accept: $accept" -H "MCP-Protocol-Version: $version" -H "@$scratch/auth.header" -H "Origin: $origin" --data-binary @- -o "$scratch/$name.json" -w '%{http_code}' "$endpoint"
  else
    printf '%s' "$payload" | curl -sS --max-time 25 -H 'Content-Type: application/json' -H "Accept: $accept" -H "MCP-Protocol-Version: $version" -H "@$scratch/auth.header" --data-binary @- -o "$scratch/$name.json" -w '%{http_code}' "$endpoint"
  fi
}
status=$(rpc '{"jsonrpc":"2.0","method":"notifications/initialized"}' notification yes)
if [[ "$status" != 202 || -s "$scratch/notification.json" ]]; then
  echo "FAIL: notification was not acknowledged without a body (HTTP $status)" >&2
  exit 1
fi
status=$(rpc_custom '{"jsonrpc":"2.0","id":4,"method":"ping","params":{}}' stream 'text/event-stream' '2025-06-18' '')
if [[ "$status" != 200 || "$(grep -c '^event: message\n'
printf 'Discovered tools: %s\n' "$(jq -r '.result.tools | length' "$scratch/discovered.json")"
 "$scratch/stream.json")" != 1 || "$(grep -c '^data: ' "$scratch/stream.json")" != 1 ]]; then
  echo "FAIL: SSE ping did not return exactly one message (HTTP $status)" >&2
  exit 1
fi
data=$(sed -n 's/^data: //p' "$scratch/stream.json")
if ! jq -e '.jsonrpc == "2.0" and .id == 4 and .result == {}' <<< "$data" >/dev/null; then
  echo 'FAIL: SSE ping lost its JSON-RPC identity or result' >&2
  exit 1
fi
unset data
status=$(rpc_custom '{"jsonrpc":"2.0","id":5,"method":"ping"}' unsupported 'application/json' '2030-01-01' '')
if [[ "$status" != 400 ]]; then
  echo "FAIL: unsupported MCP version accepted (HTTP $status)" >&2
  exit 1
fi
status=$(rpc_custom '{"jsonrpc":"2.0","id":6,"method":"ping"}' origin 'application/json' '2025-06-18' 'https://untrusted.invalid')
if [[ "$status" != 403 ]]; then
  echo "FAIL: cross-origin request accepted (HTTP $status)" >&2
  exit 1
fi
status=$(rpc '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"task_add","arguments":{"title":"must never persist"}}}' anonymous_mutation no)
if [[ "$status" != 401 ]]; then
  echo "FAIL: anonymous tool invocation was accepted (HTTP $status)" >&2
  exit 1
fi

printf 'PASS: anonymous requests denied; authenticated initialization, discovery, notifications, SSE and transport denials accepted.\n'
printf 'Discovered tools: %s\n' "$(jq -r '.result.tools | length' "$scratch/discovered.json")"
