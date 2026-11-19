ARG RUST_BASE_IMAGE
ARG GO_BASE_IMAGE
FROM ${RUST_BASE_IMAGE} AS toolchain
FROM ${GO_BASE_IMAGE} AS build
WORKDIR /src
COPY services/sandbox/go.mod ./go.mod
COPY services/sandbox/main.go ./main.go
RUN mkdir -p /out \
    && GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local CGO_ENABLED=0 \
       go build -trimpath -buildvcs=false -o /out/homelab-engineering-sandbox .

FROM ${GO_BASE_IMAGE}
COPY --from=toolchain /usr/local/cargo /usr/local/cargo
COPY --from=toolchain /usr/local/rustup /usr/local/rustup
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/homelab-engineering-sandbox /usr/local/bin/homelab-engineering-sandbox
USER 65534:65534
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/homelab-engineering-sandbox"]
