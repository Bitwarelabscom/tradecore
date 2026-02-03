# TradeCore Dockerfile
# High-performance trading engine in Go

# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install git for go mod download (some dependencies require it)
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /tradecore ./cmd/server

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS calls and tzdata for timezone support
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN adduser -D -g '' tradecore

# Create directories
RUN mkdir -p /app /var/log/tradecore && \
    chown -R tradecore:tradecore /app /var/log/tradecore

# Copy binary from builder
COPY --from=builder /tradecore /app/tradecore

# Switch to non-root user
USER tradecore

WORKDIR /app

# TradeCore listens on port 9090
EXPOSE 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:9090/health || exit 1

# Run TradeCore
ENTRYPOINT ["/app/tradecore"]
