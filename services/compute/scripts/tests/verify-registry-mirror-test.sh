#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/verify-registry-mirror.sh"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
export MOCK_STATE="$tmp" KUBECTL_BIN="$tmp/kubectl"

cat > "$KUBECTL_BIN" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$MOCK_STATE/calls"
case "$*" in
  'get node '*)
    if [[ ${MOCK_UNREADY:-} == "$3" ]]; then printf False; else printf True; fi
    ;;
  'get --raw /api/v1/nodes/'*'/proxy/metrics')
    if [[ ${MOCK_METRICS_FAIL:-0} == 1 ]]; then exit 1; fi
    node=${3#/api/v1/nodes/}
    node=${node%%/*}
    if [[ ${MOCK_UNADVERTISED:-} != "$node" ]]; then
      printf 'spegel_advertised_image_digests{registry="images.dbontr.internal"} 13\n'
    fi
    printf 'spegel_advertised_image_digests{registry="ghcr.io"} 10\n'
    if [[ ${MOCK_HITLESS:-0} != 1 && "$node" == jupiter ]]; then
      printf 'spegel_mirror_requests_total{cache="hit",registry="images.dbontr.internal"} %s\n' "${MOCK_HITS:-2}"
    fi
    printf 'spegel_mirror_requests_total{registry="images.dbontr.internal",cache="miss"} 1\n'
    ;;
  *)
    printf 'Unexpected kubectl command: %s\n' "$*" >&2
    exit 1
    ;;
esac
MOCK
chmod 0700 "$KUBECTL_BIN"

bash "$script" --require-hit jupiter venus mercury janus > "$tmp/receipt"
jq -e -s '
  length == 4
  and all(.[]; .advertised_image_digests == 13 and .mirror_cache_misses == 1 and .result == "passed")
  and ([.[].mirror_cache_hits] | add) == 2
' "$tmp/receipt" >/dev/null

if MOCK_HITLESS=1 bash "$script" --require-hit jupiter venus > /dev/null 2>&1; then
  echo 'Unobserved peer retrieval was incorrectly accepted' >&2; exit 1
fi
MOCK_HITLESS=1 bash "$script" jupiter venus > "$tmp/no-hits"
[[ $(jq -s 'length' "$tmp/no-hits") == 2 ]]

MOCK_HITS=1 bash "$script" jupiter venus > "$tmp/baseline"
bash "$script" --require-hit-since "$tmp/baseline" jupiter venus > "$tmp/delta"
jq -e -s '
  length == 2 and ([.[].new_mirror_cache_hits] | add) == 1
  and (.[0].mirror_cache_hits == 2)
' "$tmp/delta" >/dev/null
for hits in 0 1; do
  if MOCK_HITS=$hits bash "$script" --require-hit-since "$tmp/baseline" jupiter venus > /dev/null 2>&1; then
    printf 'Historical or reset hit counter was accepted: %s\n' "$hits" >&2; exit 1
  fi
done
cat "$tmp/baseline" "$tmp/baseline" > "$tmp/duplicate"
if bash "$script" --require-hit-since "$tmp/duplicate" jupiter >/dev/null 2>&1; then
  echo 'Duplicate baseline node was accepted' >&2; exit 1
fi
printf 'invalid json\n' > "$tmp/invalid"
if bash "$script" --require-hit-since "$tmp/invalid" jupiter >/dev/null 2>&1; then
  echo 'Malformed baseline was accepted' >&2; exit 1
fi
if bash "$script" --require-hit-since "$tmp/missing" jupiter >/dev/null 2>&1; then
  echo 'Missing baseline was accepted' >&2; exit 1
fi

if MOCK_UNADVERTISED=janus bash "$script" janus >/dev/null 2>&1; then
  echo 'Node without image advertisements was accepted' >&2; exit 1
fi
if MOCK_METRICS_FAIL=1 bash "$script" janus >/dev/null 2>&1; then
  echo 'Unavailable metrics were accepted' >&2; exit 1
fi
if MOCK_UNREADY=jupiter bash "$script" jupiter >/dev/null 2>&1; then
  echo 'Unready node was accepted' >&2; exit 1
fi
for arguments in 'jupiter jupiter' 'jupiter ../../host' '--require-hit'; do
  if bash "$script" $arguments >/dev/null 2>&1; then
    printf 'Invalid node list was accepted: %s\n' "$arguments" >&2; exit 1
  fi
done

# Only Kubernetes read operations are allowed by this verifier.
if grep -Ev '^get (node |--raw )' "$MOCK_STATE/calls"; then
  echo 'Verifier used a mutation request' >&2; exit 1
fi
echo 'Spegel peer-mirror verification and fail-closed tests passed.'
