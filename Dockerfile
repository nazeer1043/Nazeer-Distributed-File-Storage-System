# Stage 1: Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install security certificates and git for module downloading
RUN apk add --no-cache ca-certificates git tzdata

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked minimal binaries
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/nazeerdfs ./cmd/main.go && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/nazeerdfs-api ./cmd/api/main.go && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/nazeerdfs-node ./cmd/node/main.go

# Stage 2: Minimal Non-Root Production Image
FROM alpine:3.20

WORKDIR /app

RUN apk --no-cache add ca-certificates tzdata wget && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

# Copy compiled binaries and web assets
COPY --from=builder --chown=appuser:appgroup /app/bin/nazeerdfs /app/nazeerdfs
COPY --from=builder --chown=appuser:appgroup /app/bin/nazeerdfs-api /app/nazeerdfs-api
COPY --from=builder --chown=appuser:appgroup /app/bin/nazeerdfs-node /app/nazeerdfs-node
COPY --from=builder --chown=appuser:appgroup /app/web /app/web

# Ensure writable data directory for storage node volumes
RUN mkdir -p /app/data && chown -R appuser:appgroup /app

USER appuser:appgroup

EXPOSE 8080 3000 4000 5000

ENV HTTP_PORT=8080 \
    APP_ENV=production

HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/nazeerdfs"]
