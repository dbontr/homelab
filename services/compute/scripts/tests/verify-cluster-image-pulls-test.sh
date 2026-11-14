#!/usr/bin/env bash
set -euo pipefail

script=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)/verify-cluster-image-pulls.sh
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
export MOCK_STATE=$tmp
export KUBECTL_BIN=$tmp/kubectl
export IMAGE_PULL_NAMESPACE=pwm-system IMAGE_PULL_TIMEOUT_SECONDS=5

cat > "$KUBECTL_BIN" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$MOCK_STATE/calls"
args=" $* "
if [[ "$args" == *' get node '* ]]; then
  printf '%s' "${MOCK_NODE_READY:-True}"
elif [[ "$args" == *' create -f - '* ]]; then
  num=$(cat "$MOCK_STATE/count" 2>/dev/null || printf 0)
  num=$((num+1))
  printf '%s' "$num" > "$MOCK_STATE/count"
  name="oci-pull-check-$num"
  cat > "$MOCK_STATE/$name.yaml"
  grep -q 'automountServiceAccountToken: false' "$MOCK_STATE/$name.yaml"
  grep -q 'readOnlyRootFilesystem: true' "$MOCK_STATE/$name.yaml"
  grep -q 'imagePullPolicy: Always' "$MOCK_STATE/$name.yaml"
  grep -q 'runAsNonRoot: true' "$MOCK_STATE/$name.yaml"
  grep -q 'allowPrivilegeEscalation: false' "$MOCK_STATE/$name.yaml"
  printf '%s' "$name"
elif [[ "$args" == *' wait '* ]]; then
  [[ "${MOCK_WAIT_FAIL:-0}" != 1 ]] || exit 1
elif [[ "$args" =~ get[[:space:]]pod/([^[:space:]]+) ]]; then
  name=${BASH_REMATCH[1]}
  image=$(sed -n 's/^[[:space:]]*image: "\(.*\)"$/\1/p' "$MOCK_STATE/$name.yaml")
  jq -nc --arg id "$image" '{status:{phase:"Succeeded",containerStatuses:[{imageID:$id,state:{terminated:{exitCode:0}}}]}}'
elif [[ "$args" == *' logs '* ]]; then
  echo registry-pull-verified
elif [[ "$args" == *' get events '* ]]; then
  printf '%s\n' '{"items":[{"reason":"Pulled"}]}'
elif [[ "$args" == *' delete pods '* ]]; then
  echo deleted >> "$MOCK_STATE/cleanups"
elif [[ "$args" == *' describe '* ]]; then
  echo 'Mock pod failure' >&2
else
  printf 'Unexpected kubectl command: %s\n' "$*" >&2
  exit 1
fi
MOCK
chmod 0700 "$KUBECTL_BIN"
controller=ghcr.io/example/controller@sha256:$(printf 'a%.0s' {1..64})
worker=ghcr.io/example/worker@sha256:$(printf 'b%.0s' {1..64})

bash "$script" "$controller" "$worker" janus jupiter > "$tmp/receipt.jsonl"
[[ $(jq -s 'length' "$tmp/receipt.jsonl") == 4 ]]
[[ $(jq -s '[.[] | select(.result=="passed" and .pull_policy=="Always")] | length' "$tmp/receipt.jsonl") == 4 ]]
[[ $(cat "$tmp/cleanups") == deleted ]]
[[ $(cat "$tmp/count") == 4 ]]

if bash "$script" "$controller" ghcr.io/example/worker:latest janus >/dev/null 2>&1; then
  echo 'Mutable tag was accepted' >&2
  exit 1
fi
[[ $(cat "$tmp/count") == 4 ]]

if MOCK_NODE_READY=False bash "$script" "$controller" "$worker" janus >/dev/null 2>&1; then
  echo 'Unready node was accepted' >&2
  exit 1
fi
[[ $(cat "$tmp/count") == 4 ]]

if MOCK_WAIT_FAIL=1 bash "$script" "$controller" "$worker" janus >/dev/null 2>&1; then
  echo 'Failed pod was accepted' >&2
  exit 1
fi
[[ $(cat "$tmp/cleanups" | wc -l) == 2 ]]

echo 'Image-pull verifier validation, access policy, and cleanup tests passed.'
