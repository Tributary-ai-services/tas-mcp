# Multi-stage Dockerfile for TAS MCP Server with BuildKit optimizations
#
# NOTE: build context is the TAS monorepo ROOT (not tas-mcp/), because tas-mcp's
# go.mod uses `replace github.com/Tributary-ai-services/Gatekeeper => ../Gatekeeper`
# for the reduce-at-source extractor. Build with:
#   docker build -f tas-mcp/Dockerfile -t <img> .        # from the TAS root
# Only Gatekeeper's pure-Go pkg/extract is imported (no Hyperscan/CGO), so no
# extra system deps or build tags are needed.
#
# Build stage
FROM golang:1.24-alpine AS builder

# Build arguments
ARG VERSION=1.1.0
ARG BUILD_DATE
ARG VCS_REF

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy the local-replace dependency (Gatekeeper) as a sibling of the app dir so
# the `../Gatekeeper` replace resolves inside the container.
WORKDIR /build
COPY Gatekeeper/ /build/Gatekeeper/

# App module
WORKDIR /build/app

# Copy go mod files for better layer caching
COPY tas-mcp/go.mod tas-mcp/go.sum ./

# Download dependencies (cached unless go.mod/go.sum change)
RUN go mod download

# Copy source code
COPY tas-mcp/ .

# Build the application with version info
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -extldflags '-static' -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.gitCommit=${VCS_REF}" \
    -a -installsuffix cgo \
    -o bin/tas-mcp-server \
    ./cmd/server

# Runtime stage  
FROM alpine:3.18

# Build arguments (for labels)
ARG VERSION=1.1.0
ARG BUILD_DATE
ARG VCS_REF

# Labels for metadata
LABEL org.opencontainers.image.title="TAS MCP Server"
LABEL org.opencontainers.image.description="Tributary AI Services Model Context Protocol server for event ingestion and federation"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${VCS_REF}"
LABEL org.opencontainers.image.vendor="Tributary AI Services"
LABEL org.opencontainers.image.source="https://github.com/tributary-ai-services/tas-mcp"
LABEL com.tributary-ai.service="tas-mcp-server"
LABEL com.tributary-ai.version="${VERSION}"
LABEL com.tributary-ai.component="federation-server"

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget curl

# Copy the binary
COPY --from=builder /build/app/bin/tas-mcp-server /tas-mcp-server

# Copy configuration files
COPY --from=builder /build/app/configs /configs

# Copy healthcheck script
COPY --from=builder /build/app/scripts/healthcheck.sh /healthcheck.sh
RUN chmod +x /healthcheck.sh

# Create non-root user
RUN adduser -D -s /bin/sh appuser
USER appuser

# Expose ports
EXPOSE 8082 50052 8083

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/healthcheck.sh"]

# Set default environment variables
ENV LOG_LEVEL=info
ENV HTTP_PORT=8080
ENV GRPC_PORT=50051
ENV HEALTH_CHECK_PORT=8082
ENV SERVICE_VERSION=${VERSION}
ENV SERVICE_NAME=tas-mcp-federation-server

# Run the server
ENTRYPOINT ["/tas-mcp-server"]
CMD []