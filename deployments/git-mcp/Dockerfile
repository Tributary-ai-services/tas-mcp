# Git MCP Server Dockerfile
FROM python:3.11-slim

# Build arguments
ARG VERSION=1.0.0
ARG BUILD_DATE
ARG VCS_REF

# Labels for metadata
LABEL org.opencontainers.image.title="TAS MCP Git Server"
LABEL org.opencontainers.image.description="Official Git repository interaction and automation server for Model Context Protocol"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${VCS_REF}"
LABEL org.opencontainers.image.vendor="Tributary AI Services"
LABEL org.opencontainers.image.source="https://github.com/tributary-ai-services/tas-mcp"
LABEL com.tributary-ai.service="git-mcp-server"
LABEL com.tributary-ai.version="${VERSION}"
LABEL com.tributary-ai.component="mcp-server"

# Set working directory
WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    git \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
RUN pip install --no-cache-dir \
    mcp-server-git \
    uvicorn \
    fastapi \
    httpx

# Create non-root user
RUN useradd -m -u 1000 mcpuser && \
    chown -R mcpuser:mcpuser /app

# Create directories for repositories and logs
RUN mkdir -p /repositories /app/logs && \
    chown -R mcpuser:mcpuser /repositories /app/logs

# Switch to non-root user
USER mcpuser

# Expose port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/health || exit 1

# Default environment variables
ENV MCP_PORT=3000
ENV MCP_HOST=0.0.0.0
ENV REPOSITORY_PATH=/repositories
ENV LOG_LEVEL=info
ENV SERVICE_VERSION=${VERSION}
ENV SERVICE_NAME=tas-mcp-git-server

# Entry point script
COPY --chown=mcpuser:mcpuser deployments/git-mcp/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Run the server
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["python", "-m", "mcp_server_git"]