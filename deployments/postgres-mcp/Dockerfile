# PostgreSQL MCP Server Dockerfile
# Based on the Model Context Protocol specification for database interaction

ARG NODE_VERSION=20
ARG VERSION=1.0.0
ARG BUILD_DATE
ARG VCS_REF

FROM node:${NODE_VERSION}-alpine AS builder

# Install build dependencies
RUN apk add --no-cache \
    python3 \
    make \
    g++ \
    postgresql-dev

# Set working directory
WORKDIR /app

# Copy package.json and install dependencies
COPY deployments/postgres-mcp/package.json .
RUN npm install --production

# Copy server files
COPY deployments/postgres-mcp/server.js .
COPY deployments/postgres-mcp/entrypoint.sh .

# Make entrypoint executable
RUN chmod +x entrypoint.sh

# Create runtime stage
FROM node:${NODE_VERSION}-alpine AS runtime

# Install runtime dependencies
RUN apk add --no-cache \
    postgresql-client \
    curl \
    && rm -rf /var/cache/apk/*

# Add non-root user for security
RUN addgroup -g 1001 -S postgres-mcp && \
    adduser -S postgres-mcp -u 1001 -G postgres-mcp

# Set working directory
WORKDIR /app

# Copy application files from builder
COPY --from=builder --chown=postgres-mcp:postgres-mcp /app .

# Create directories
RUN mkdir -p logs data && \
    chown -R postgres-mcp:postgres-mcp /app

# Set environment variables
ENV NODE_ENV=production
ENV POSTGRES_MCP_VERSION=${VERSION}
ENV DATABASE_URL=postgresql://postgres:password@postgres-db:5432/postgres
ENV READ_ONLY=true
ENV MAX_CONNECTIONS=10
ENV QUERY_TIMEOUT=30000
ENV HEALTH_CHECK_ENABLED=true
ENV HEALTH_PORT=3401
ENV LOG_LEVEL=info

# Add labels for container metadata
LABEL maintainer="TAS MCP Team" \
      version="${VERSION}" \
      description="PostgreSQL Model Context Protocol Server for TAS MCP Federation" \
      org.opencontainers.image.title="PostgreSQL MCP Server" \
      org.opencontainers.image.description="PostgreSQL Model Context Protocol Server with read-only database access" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="https://github.com/tributary-ai-services/tas-mcp" \
      com.tributary-ai.service="postgres-mcp-server" \
      com.tributary-ai.version="${VERSION}" \
      com.tributary-ai.component="mcp-server"

# Switch to non-root user
USER postgres-mcp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:3401/health || exit 1

# Expose health check port
EXPOSE 3401

# Set entrypoint
ENTRYPOINT ["./entrypoint.sh"]