#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
cd "$root/services/core"
go test -count=1 ./cmd/homelab-executord ./cmd/homelab-release
go vet ./cmd/homelab-executord ./cmd/homelab-release
go build ./cmd/homelab-executord ./cmd/homelab-release
