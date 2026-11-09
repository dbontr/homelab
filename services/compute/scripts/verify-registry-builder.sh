#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

go_bin="${GO_BIN:-go}"
helm_bin="${HELM_BIN:-helm}"
docker_bin="${DOCKER_BIN:-docker}"

for tool in "$go_bin" "$helm_bin" "$docker_bin"; do
  command -v "$tool" >/dev/null || {
    printf 'Required validation tool is unavailable: %s\n' "$tool" >&2
    exit 1
  }
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

RUN_KUBE_INTEGRATION=0 KUBECONFIG=/dev/null "$go_bin" test ./... -count=1
"$go_bin" vet ./...

digest="$(printf '%064d' 0)"
image="docker.io/moby/buildkit:rootless@sha256:$digest"
options=(
  --set controller.enabled=false
  --set worker.enabled=true
  --set worker.workloadNode=mercury
  --set "worker.builder.image=$image"
  --set worker.builder.registry=ghcr.io/dbontr
  --set worker.builder.authSecretName=registry-auth
)

"$helm_bin" lint deploy/helm/project-workspace "${options[@]}"
"$helm_bin" template pwm-validation deploy/helm/project-workspace "${options[@]}" > "$tmp/worker.yaml"

grep -q 'name: pwm-image-builder-egress' "$tmp/worker.yaml"
grep -q 'name: PWM_KUBE_BUILDER_IMAGE' "$tmp/worker.yaml"
grep -q 'name: PWM_KUBE_BUILDER_REGISTRY' "$tmp/worker.yaml"
grep -q 'name: PWM_KUBE_BUILDER_AUTH_SECRET' "$tmp/worker.yaml"

if "$helm_bin" template pwm-validation deploy/helm/project-workspace \
    --set controller.enabled=false --set worker.enabled=true \
    --set worker.builder.image=moby/buildkit:latest \
    --set worker.builder.registry=ghcr.io/dbontr \
    --set worker.builder.authSecretName=registry-auth > "$tmp/bad.yaml" 2>&1; then
  echo "Helm admitted an unpinned registry builder image" >&2
  exit 1
fi

"$docker_bin" buildx build --check --progress=plain \
  -f deploy/kubernetes/Dockerfile.go-rust-wasm .

if command -v nix-instantiate >/dev/null; then
  nix-instantiate --parse deploy/nixos/module.nix >/dev/null
fi

echo "Registry builder source, Helm policy, toolchain Dockerfile and Nix syntax checks passed."
