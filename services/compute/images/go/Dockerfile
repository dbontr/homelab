ARG BASE_IMAGE
FROM ${BASE_IMAGE}

LABEL org.opencontainers.image.title="Homelab Go Engineering"
LABEL org.opencontainers.image.description="Project-isolated Go 1.27.3 development toolchain"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

RUN test "$(go env GOVERSION)" = "go1.27.3" \
    && test "$(go env GOOS)" = "linux" \
    && test "$(go env GOARCH)" = "amd64"

# The Workspaces controller enforces uid/gid 65534 and mounts a writable
# project volume at /workspace while keeping the image filesystem read-only.
RUN mkdir -p /workspace/go /workspace/.cache/go-build \
    && chown -R 65534:65534 /workspace

ENV HOME=/workspace \
    GOPATH=/workspace/go \
    GOCACHE=/workspace/.cache/go-build \
    XDG_CACHE_HOME=/workspace/.cache

WORKDIR /workspace
USER 65534:65534
RUN test "$(id -u)" = 65534 \
    && test -w "$HOME" \
    && test "$(go env GOVERSION)" = "go1.27.3"
CMD ["sleep", "infinity"]
