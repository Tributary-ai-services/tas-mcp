# Playwright MCP Server Dockerfile
# Browser automation server using Playwright

ARG NODE_VERSION=20
ARG VERSION=1.0.0
ARG BUILD_DATE
ARG VCS_REF

# Use official Playwright image as base (includes browsers and dependencies)
FROM mcr.microsoft.com/playwright:v1.40.0-jammy AS base

# Set working directory
WORKDIR /app

# Copy package.json and install dependencies
COPY deployments/docker-compose/playwright-mcp/package.json .
RUN npm install --production

# Copy server files
COPY deployments/docker-compose/playwright-mcp/server.js .
COPY deployments/docker-compose/playwright-mcp/entrypoint.sh .

# Fix line endings and make entrypoint executable
RUN sed -i 's/\r$//' entrypoint.sh && chmod +x entrypoint.sh

# Create runtime stage
FROM mcr.microsoft.com/playwright:v1.40.0-jammy AS runtime

# Install curl for health checks
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# Add non-root user for security
RUN groupadd -g 1001 playwright-mcp && \
    useradd -u 1001 -g playwright-mcp -m playwright-mcp

# Set working directory
WORKDIR /app

# Copy application files from base
COPY --from=base --chown=playwright-mcp:playwright-mcp /app .

# Create directories
RUN mkdir -p logs screenshots && \
    chown -R playwright-mcp:playwright-mcp /app

# Set environment variables
ENV NODE_ENV=production
ENV PLAYWRIGHT_MCP_VERSION=${VERSION}
ENV HEADLESS=true
ENV DEFAULT_TIMEOUT=30000
ENV VIEWPORT_WIDTH=1280
ENV VIEWPORT_HEIGHT=720
ENV HEALTH_CHECK_ENABLED=true
ENV HEALTH_PORT=3404
ENV LOG_LEVEL=info

# Playwright environment
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1

# Add labels for container metadata
LABEL maintainer="TAS MCP Team" \
      version="${VERSION}" \
      description="Playwright Model Context Protocol Server for browser automation" \
      org.opencontainers.image.title="Playwright MCP Server" \
      org.opencontainers.image.description="Browser automation and web testing server using Playwright" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="https://github.com/tributary-ai-services/tas-mcp" \
      com.tributary-ai.service="playwright-mcp-server" \
      com.tributary-ai.version="${VERSION}" \
      com.tributary-ai.component="browser-automation"

# Switch to non-root user
USER playwright-mcp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD curl -f http://localhost:3404/health || exit 1

# Expose health check port
EXPOSE 3404

# Set entrypoint
ENTRYPOINT ["./entrypoint.sh"]
