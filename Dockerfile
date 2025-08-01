# Multi-stage Dockerfile for TAS MCP Server
# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set the working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o bin/tas-mcp-server \
    ./cmd/server

# Runtime stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget

# Copy the binary
COPY --from=builder /app/bin/tas-mcp-server /tas-mcp-server

# Copy configuration files
COPY --from=builder /app/configs /configs

# Copy healthcheck script
COPY --from=builder /app/scripts/healthcheck.sh /healthcheck.sh
RUN chmod +x /healthcheck.sh

# Create non-root user
RUN adduser -D -s /bin/sh -u 65534 appuser
USER appuser

# Expose ports
EXPOSE 8080 50051 8082

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/healthcheck.sh"]

# Set default environment variables
ENV LOG_LEVEL=info
ENV HTTP_PORT=8080
ENV GRPC_PORT=50051
ENV HEALTH_CHECK_PORT=8082

# Run the server
ENTRYPOINT ["/tas-mcp-server"]
CMD []