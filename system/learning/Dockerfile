FROM golang:1.27.3-bookworm AS builder
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends libsqlite3-dev && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum* ./
RUN go mod download
COPY internal ./internal
COPY cmd ./cmd
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/adaptive-teaching ./cmd/adaptive-teaching

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libsqlite3-0 && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home --shell /usr/sbin/nologin adaptive \
    && install -d -m 0700 -o adaptive -g adaptive /var/lib/adaptive-teaching
COPY --from=builder /out/adaptive-teaching /usr/local/bin/adaptive-teaching
USER adaptive
ENV ADAPTIVE_TEACHING_DB_PATH=/var/lib/adaptive-teaching/learning.db \
    ADAPTIVE_TEACHING_LISTEN_ADDR=0.0.0.0:8000
EXPOSE 8000
CMD ["/usr/local/bin/adaptive-teaching"]
