# DuckDuckGo MCP Server Dockerfile
# Privacy-focused web search and content extraction server

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

# Install runtime dependencies including Chromium for advanced content extraction
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
RUN addgroup -g 1001 -S duckduckgo-mcp && \
    adduser -S duckduckgo-mcp -u 1001 -G duckduckgo-mcp

# Set working directory
WORKDIR /app

# Copy application files from builder
COPY --from=builder --chown=duckduckgo-mcp:duckduckgo-mcp /app .

# Create directories
RUN mkdir -p logs cache && \
    chown -R duckduckgo-mcp:duckduckgo-mcp /app

# Set environment variables
ENV NODE_ENV=production
ENV DUCKDUCKGO_MCP_VERSION=${VERSION}
ENV SEARCH_REGION=us-en
ENV SAFE_SEARCH=moderate
ENV MAX_RESULTS=10
ENV REQUEST_TIMEOUT=30000
ENV HEALTH_CHECK_ENABLED=true
ENV HEALTH_PORT=3402
ENV LOG_LEVEL=info

# Playwright/Chromium configuration
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true
ENV PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium-browser

# Add labels for container metadata
LABEL maintainer="TAS MCP Team" \
      version="${VERSION}" \
      description="DuckDuckGo Model Context Protocol Server for privacy-focused web search" \
      org.opencontainers.image.title="DuckDuckGo MCP Server" \
      org.opencontainers.image.description="Privacy-focused web search and content extraction server using DuckDuckGo" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="https://github.com/tributary-ai-services/tas-mcp" \
      com.tributary-ai.service="duckduckgo-mcp-server" \
      com.tributary-ai.version="${VERSION}" \
      com.tributary-ai.component="search-server"

# Switch to non-root user
USER duckduckgo-mcp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:3402/health || exit 1

# Expose health check port
EXPOSE 3402

# Set entrypoint
ENTRYPOINT ["./entrypoint.sh"]