# Build Stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy dependency files first
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Build clean production binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /nazeerdfs ./cmd/main.go

# Production Execution Stage
FROM alpine:latest

WORKDIR /app

RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /nazeerdfs /app/nazeerdfs
COPY --from=builder /app/web /app/web
COPY credentials.json* /app/
COPY token.json* /app/

EXPOSE 8080 3000 4000 5000

ENV HTTP_PORT=8080

CMD ["/app/nazeerdfs"]
