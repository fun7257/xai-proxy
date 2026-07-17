# syntax=docker/dockerfile:1.7
#
# Multi-stage production image for xai-proxy.
# Build:  docker build -t xai-proxy:local .
# Serve:  docker run --rm -it -p 127.0.0.1:7257:7257 -v xai-data:/data xai-proxy:local
#         (entrypoint auto-generates client key if missing, then device-login if needed)
# Pass-through CLI (no bootstrap): generate | login | status | logout | version
#   docker run --rm -v xai-data:/data xai-proxy:local status

ARG GO_VERSION=1.26.5

# -----------------------------------------------------------------------------
# Build
# -----------------------------------------------------------------------------
# BUILDPLATFORM: compile on the builder node; TARGETOS/TARGETARCH select output.
# Falls back to linux/amd64 when classic `docker build` leaves TARGETARCH empty.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=0.1.0

WORKDIR /src

# Module first for better layer cache (stdlib-only module still copies cleanly).
COPY go.mod ./
RUN go mod download 2>/dev/null || true

COPY . .

# Static binary; no CGO. Trim path + strip symbols for smaller image.
RUN set -eux; \
    arch="${TARGETARCH}"; \
    if [ -z "${arch}" ]; then arch="$(go env GOARCH)"; fi; \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${arch}" \
      go build -trimpath \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/xai-proxy \
        ./cmd/xai-proxy

# -----------------------------------------------------------------------------
# Runtime (Alpine: CA certs + wget for HEALTHCHECK, non-root)
# -----------------------------------------------------------------------------
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 65532 -S xai \
    && adduser -u 65532 -S -G xai -h /data -s /sbin/nologin xai \
    && mkdir -p /data \
    && chown -R xai:xai /data

COPY --from=builder /out/xai-proxy /usr/local/bin/xai-proxy
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 755 /usr/local/bin/docker-entrypoint.sh

USER xai:xai
WORKDIR /data

# Token store (override with -v host_dir:/data)
ENV XAI_PROXY_HOME=/data \
    TZ=UTC

# Bind 0.0.0.0 so port publish works. Prefer publishing only to host loopback.
# /v1/* still requires the local client API key (see client_key under XAI_PROXY_HOME).
EXPOSE 7257

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://127.0.0.1:7257/health >/dev/null || exit 1

# Bootstrap on serve: generate client key (log once) + login --no-browser if needed.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
# --i-understand-non-loopback-bind: allow 0.0.0.0 so published ports work.
# Client API key auth on /v1/* remains required.
CMD ["serve", "--host", "0.0.0.0", "--port", "7257", "--i-understand-non-loopback-bind"]
