# Multi-stage Dockerfile for TAS MCP Server.
#
# NOTE: build context is the TAS monorepo ROOT (not tas-mcp/), because tas-mcp's
# go.mod uses `replace github.com/Tributary-ai-services/Gatekeeper => ../Gatekeeper`.
# Build with:
#   docker build -f tas-mcp/Dockerfile -t <img> .        # from the TAS root
#
# G2 boundary scanning imports Gatekeeper's pkg/scan, whose DEFAULT match engine
# is Intel Hyperscan (github.com/flier/gohs, //go:build !nohs). This image builds
# that engine — CGO on, libhyperscan-dev in the builder, debian runtime — to
# match Gatekeeper's own service and tas-llm-router. Hyperscan is x86_64-only, so
# the target is pinned to linux/amd64.
#
# Build stage
FROM golang:1.24-bookworm AS builder

# Build arguments
ARG VERSION=1.1.0
ARG BUILD_DATE
ARG VCS_REF

# Install build dependencies. libhyperscan-dev + pkg-config compile the Intel
# Hyperscan engine (gohs); git/ca-certificates/tzdata for the build.
RUN apt-get update && apt-get install -y --no-install-recommends \
        git ca-certificates tzdata \
        libhyperscan-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

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

# Build with Intel Hyperscan enabled. CGO must be on for the gohs bindings; no
# `nohs` tag, so the default Hyperscan engine (pkg/scan/engine_hyperscan.go) is
# compiled in. The binary links libhs.so.5 (and glibc) dynamically — verified
# via ldd — so the runtime stage is debian with libhyperscan5, not alpine.
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.gitCommit=${VCS_REF}" \
    -o bin/tas-mcp-server \
    ./cmd/server

# Runtime stage
FROM debian:bookworm-slim

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

# Install runtime dependencies. libhyperscan5 is REQUIRED, not optional: the
# CGO binary links libhs.so.5 dynamically (confirmed with ldd), so without it
# the server fails at startup with "libhs.so.5: cannot open shared object file".
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates tzdata libhyperscan5 wget curl \
    && rm -rf /var/lib/apt/lists/*

# Copy the binary
COPY --from=builder /build/app/bin/tas-mcp-server /tas-mcp-server

# Copy configuration files
COPY --from=builder /build/app/configs /configs

# Copy healthcheck script
COPY --from=builder /build/app/scripts/healthcheck.sh /healthcheck.sh
RUN chmod +x /healthcheck.sh

# Create non-root user
RUN groupadd -g 1000 appuser && \
    useradd -u 1000 -g appuser -s /bin/sh -M appuser
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