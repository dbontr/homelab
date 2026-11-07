FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/personal-health-mcp ./cmd/personal-health-mcp

FROM alpine:3.21
RUN addgroup -S app && adduser -S -G app app && mkdir -p /data && chown app:app /data && chmod 0700 /data
COPY --from=build /out/personal-health-mcp /usr/local/bin/personal-health-mcp
USER app
VOLUME /data
ENV MCP_TRANSPORT=http MCP_LISTEN=0.0.0.0:8080 PERSONAL_HEALTH_DB_PATH=/data/health.db
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/personal-health-mcp"]
