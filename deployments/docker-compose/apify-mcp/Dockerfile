# Apify MCP Server Dockerfile
# Web scraping and automation server for the Apify platform

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
    chromium \
    nss \
    freetype \
    freetype-dev \
    harfbuzz \
    ca-certificates \
    ttf-freefont

# Set working directory
WORKDIR /app

# Copy package.json and install dependencies
COPY package.json .
RUN npm install --production

# Copy server files
COPY server.js .
COPY entrypoint.sh .

# Make entrypoint executable
RUN chmod +x entrypoint.sh

# Create runtime stage
FROM node:${NODE_VERSION}-alpine AS runtime

# Install runtime dependencies including Chromium for Playwright
RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    freetype-dev \
    harfbuzz \
    ca-certificates \
    ttf-freefont \
    curl \
    && rm -rf /var/cache/apk/*

# Add non-root user for security
RUN addgroup -g 1001 -S apify-mcp && \
    adduser -S apify-mcp -u 1001 -G apify-mcp

# Set working directory
WORKDIR /app

# Copy application files from builder
COPY --from=builder --chown=apify-mcp:apify-mcp /app .

# Create directories
RUN mkdir -p logs cache datasets && \
    chown -R apify-mcp:apify-mcp /app

# Set environment variables
ENV NODE_ENV=production
ENV APIFY_MCP_VERSION=${VERSION}
ENV DEFAULT_MEMORY_MBYTES=512
ENV DEFAULT_TIMEOUT_SECS=300
ENV HEALTH_CHECK_ENABLED=true
ENV HEALTH_PORT=3403
ENV LOG_LEVEL=info

# Playwright/Chromium configuration
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true
ENV PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium-browser

# Add labels for container metadata
LABEL maintainer="TAS MCP Team" \
      version="${VERSION}" \
      description="Apify Model Context Protocol Server for web scraping and automation" \
      org.opencontainers.image.title="Apify MCP Server" \
      org.opencontainers.image.description="Web scraping and automation server using Apify platform" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="https://github.com/tributary-ai-services/tas-mcp" \
      com.tributary-ai.service="apify-mcp-server" \
      com.tributary-ai.version="${VERSION}" \
      com.tributary-ai.component="scraping-server"

# Switch to non-root user
USER apify-mcp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD curl -f http://localhost:3403/health || exit 1

# Expose health check port
EXPOSE 3403

# Set entrypoint
ENTRYPOINT ["./entrypoint.sh"]