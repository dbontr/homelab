#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || ! -x "$1" ]]; then
  echo "Usage: $0 /path/to/adaptive-teaching" >&2
  exit 2
fi
for tool in curl jq openssl base64 realpath; do
  command -v "$tool" >/dev/null || { echo "Missing $tool" >&2; exit 2; }
done

binary="$(realpath "$1")"
work="$(mktemp -d)"
port="$(printenv ADAPTIVE_TEACHING_ACCEPT_PORT || echo 28731)"
base="http://127.0.0.1:$port"
pid=""
cleanup() {
  if [[ -n "$pid" ]]; then
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

if curl --silent --max-time 2 "$base/healthz" >/dev/null 2>&1; then
  echo "Acceptance port $port is already in use" >&2
  exit 2
fi
umask 077
openssl rand -hex 32 > "$work/token"
token="$(cat "$work/token")"

start() {
  local database="$1"
  ADAPTIVE_TEACHING_DB_PATH="$database" \
  ADAPTIVE_TEACHING_LISTEN_ADDR="127.0.0.1:$port" \
  ADAPTIVE_TEACHING_ALLOWED_HOSTS="127.0.0.1:*" \
  ADAPTIVE_TEACHING_BEARER_TOKEN_FILE="$work/token" \
  "$binary" > "$work/service.log" 2>&1 &
  pid="$!"
  local status=""
  for attempt in $(seq 1 100); do
    status="$(curl --silent --max-time 2 -o /dev/null -w '%{http_code}' "$base/healthz" || true)"
    if [[ "$status" == "200" ]]; then
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      cat "$work/service.log" >&2
      echo "Service terminated before readiness" >&2
      exit 1
    fi
    sleep 0.1
  done
  cat "$work/service.log" >&2
  echo "Service health check failed" >&2
  exit 1
}
stop() {
  kill -TERM "$pid"
  wait "$pid"
  pid=""
}
post() {
  curl --fail --silent --show-error --max-time 30 \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H "Authorization: Bearer $token" \
    --data-binary "$1" "$base/mcp"
}
rpc() {
  jq -nc --arg name "$1" --argjson arguments "$2" \
    '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:$name,arguments:$arguments}}'
}
unpack() {
  jq -e '.result.content[0].text | fromjson'
}

start "$work/live.db"
code="$(curl --silent --max-time 5 -o /dev/null -w '%{http_code}' "$base/mcp")"
[[ "$code" == "401" ]] || { echo "Anonymous MCP request accepted ($code)" >&2; exit 1; }
code="$(curl --silent --max-time 5 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer incorrect' "$base/mcp")"
[[ "$code" == "401" ]] || { echo "Invalid bearer accepted ($code)" >&2; exit 1; }

post '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-acceptance","version":"1"}}}' \
  | jq -e '.result.protocolVersion' >/dev/null
post '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | jq -e '.result.tools | any(.name == "upload_attachment")' >/dev/null

post "$(rpc add_concept '{"identifier":"release-acceptance","title":"Release acceptance","summary":"Preservation of owner-controlled learning evidence","domain":"engineering"}')" \
  | unpack | jq -e '.id == "release-acceptance"' >/dev/null
session="$(post "$(rpc start_learning_session '{"goal":"Verify persistence","target_concept_id":"release-acceptance"}')" \
  | unpack | jq -r '.id')"
[[ -n "$session" && "$session" != "null" ]] || { echo "Session was not created" >&2; exit 1; }

printf '\211PNG\r\n\032\noriginal-evidence' > "$work/evidence.png"
original="$(base64 < "$work/evidence.png" | tr -d '\r\n')"
args="$(jq -nc --arg owner "$session" --arg payload "$original" \
  '{owner_kind:"session",owner_id:$owner,filename:"evidence.png",mime_type:"image/png",base64:$payload,note:"Synthetic acceptance fixture"}')"
attachment="$(post "$(rpc upload_attachment "$args")" | unpack | jq -r '.id')"
[[ -n "$attachment" && "$attachment" != "null" ]] || { echo "Attachment was not stored" >&2; exit 1; }

"$binary" backup --db "$work/live.db" --output "$work/snapshot.db" >/dev/null
[[ -s "$work/snapshot.db" ]] || { echo "SQLite backup is empty" >&2; exit 1; }
stop
start "$work/snapshot.db"

post "$(rpc get_graph '{"focus":"release-acceptance"}')" | unpack \
  | jq -e '.nodes | any(.id == "release-acceptance")' >/dev/null
args="$(jq -nc --arg id "$attachment" '{attachment_id:$id,include_original:true}')"
restored="$(post "$(rpc get_attachment "$args")" | unpack)"
[[ "$(jq -r '.base64' <<< "$restored")" == "$original" ]] || {
  echo "Original attachment bytes changed across backup and restore" >&2
  exit 1
}
jq -e '.integrity_verified == true' <<< "$restored" >/dev/null
stop

echo "PASS: authenticated MCP, concepts, session, attachment, graceful stop and SQLite restore"
