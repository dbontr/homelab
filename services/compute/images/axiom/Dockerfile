ARG BASE_IMAGE
FROM ${BASE_IMAGE}

LABEL org.opencontainers.image.title="Homelab Mathematical Research"
LABEL org.opencontainers.image.description="Pinned Rust 1.91.1, Lean 4.34.0, Lake and WebAssembly development toolchain"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git zstd libgmp10 \
    && rm -rf /var/lib/apt/lists/* \
    && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' libgmp10)" ge 2:6.3.0

RUN rustup target add wasm32-unknown-unknown --toolchain 1.91.1 \
    && rustup component add rustfmt clippy --toolchain 1.91.1

ARG LEAN_VERSION=4.34.0
ARG LEAN_LINUX_AMD64_SHA256=caaa98356098c85dc0fcbbd28e1ec66f39eb6551829972b752ff20e1286b646b

RUN set -eu; \
    curl --fail --location --silent --show-error --retry 5 \
      "https://github.com/leanprover/lean4/releases/download/v${LEAN_VERSION}/lean-${LEAN_VERSION}-linux.tar.zst" \
      --output /tmp/lean.tar.zst; \
    echo "${LEAN_LINUX_AMD64_SHA256}  /tmp/lean.tar.zst" | sha256sum --check --status; \
    mkdir -p /opt/lean; \
    tar -I zstd -xf /tmp/lean.tar.zst --strip-components=1 -C /opt/lean; \
    rm /tmp/lean.tar.zst

ENV PATH="/opt/lean/bin:/usr/local/cargo/bin:${PATH}" \
    RUSTUP_HOME=/usr/local/rustup \
    LD_LIBRARY_PATH=/opt/lean/lib

COPY services/compute/scripts/verify-research-toolchains.sh /usr/local/bin/verify-research-toolchains
RUN chmod 0555 /usr/local/bin/verify-research-toolchains \
    && /usr/local/bin/verify-research-toolchains 1.91.1 4.34.0

RUN groupadd --gid 10001 builder \
    && useradd --uid 10001 --gid builder --create-home --shell /usr/sbin/nologin builder \
    && mkdir -p /workspace \
    && chown builder:builder /workspace

ENV HOME=/home/builder \
    CARGO_HOME=/home/builder/.cargo
WORKDIR /workspace
USER 10001:10001

RUN /usr/local/bin/verify-research-toolchains 1.91.1 4.34.0
CMD ["sleep", "infinity"]
