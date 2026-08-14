# =============================================================================
# MAFAFD backend — multi-stage build
# Produces a single image containing all four service binaries:
#   ingestion, worker-rules, worker-ai, gateway
# The target process is chosen at runtime via the container `command`.
# =============================================================================

FROM golang:1.26-alpine AS builder

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-mod=mod

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN set -eux; \
    for svc in ingestion worker-rules worker-ai gateway; do \
      go build -trimpath -ldflags="-s -w" -o /out/bin/$svc ./cmd/$svc; \
    done

# --- runtime stage (non-root, minimal) ---
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 1001 mafafd && \
    adduser -S -D -H -u 1001 -G mafafd mafafd

WORKDIR /app
COPY --from=builder /out/bin/ /app/bin/

RUN chown -R mafafd:mafafd /app
USER mafafd

EXPOSE 8080 8081
# Run a specific binary via `command:` in docker-compose, e.g. `command: ["/app/bin/gateway"]`
