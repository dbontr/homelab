ARG RUST_BASE_IMAGE
ARG GO_BASE_IMAGE
FROM ${RUST_BASE_IMAGE} AS build
WORKDIR /src
COPY services/sandbox/src ./src
RUN mkdir -p /out \
    && rustc --edition=2021 -D warnings -C opt-level=2 src/main.rs -o /out/homelab-engineering-sandbox

FROM ${GO_BASE_IMAGE}
COPY --from=build /usr/local/cargo /usr/local/cargo
COPY --from=build /usr/local/rustup /usr/local/rustup
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
