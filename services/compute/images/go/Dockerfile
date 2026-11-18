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

RUN groupadd --gid 10001 builder \
    && useradd --uid 10001 --gid builder --create-home --shell /usr/sbin/nologin builder \
    && mkdir -p /workspace \
    && chown builder:builder /workspace

ENV HOME=/home/builder \
    GOPATH=/home/builder/go \
    GOCACHE=/home/builder/.cache/go-build

WORKDIR /workspace
USER 10001:10001
RUN test "$(go env GOVERSION)" = "go1.27.3"
CMD ["sleep", "infinity"]
