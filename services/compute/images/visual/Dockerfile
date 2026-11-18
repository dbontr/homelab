ARG BASE_IMAGE
FROM ${BASE_IMAGE}

LABEL org.opencontainers.image.title="Homelab Visual Engineering"
LABEL org.opencontainers.image.description="Isolated graphical inspection, circuit previews and nondestructive photo processing"

# The operator pins BASE_IMAGE to its verified immutable OCI digest and must
# record exact installed package versions before approving an environment.
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
       ca-certificates git darktable ffmpeg xvfb x11-utils \
       chromium fonts-dejavu-core graphviz ngspice imagemagick \
    && rm -rf /var/lib/apt/lists/*

RUN command -v darktable-cli \
    && command -v chromium \
    && command -v Xvfb \
    && command -v xdpyinfo \
    && command -v ffmpeg \
    && command -v ngspice \
    && darktable-cli --help >/dev/null

RUN mkdir -p /workspace/.config /workspace/.cache /workspace/.local/share \
    && chown -R 65534:65534 /workspace

ENV HOME=/workspace \
    XDG_CONFIG_HOME=/workspace/.config \
    XDG_CACHE_HOME=/workspace/.cache \
    XDG_DATA_HOME=/workspace/.local/share

WORKDIR /workspace
USER 65534:65534
CMD ["sleep", "infinity"]
