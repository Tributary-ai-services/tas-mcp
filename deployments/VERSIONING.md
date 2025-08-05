# 🏷️ TAS MCP Versioning & Container Naming Guide

This document explains the versioning strategy and unique container naming scheme for TAS MCP and all federated MCP servers.

## 📋 Version Configuration

### Default Versions
- **TAS MCP Server**: `1.1.0` (Federation Foundation release)
- **Git MCP Server**: `1.0.0` (Official Git server integration)

### Environment Variables
```bash
# Primary version configuration
TAS_MCP_VERSION=1.1.0      # TAS MCP federation server version
GIT_MCP_VERSION=1.0.0      # Git MCP server version

# Build metadata (auto-generated)
BUILD_DATE=2025-08-04T00:00:00Z
VCS_REF=abc1234
```

## 🐳 Docker Container Naming

### Unique Container Names
All MCP server containers use a standardized naming pattern to ensure uniqueness:

```
Pattern: tas-mcp-<service-type>-v<version>
```

**Current Container Names:**
- `tas-mcp-federation-server-v1.1.0` - Main TAS MCP federation server
- `tas-mcp-git-server-v1.0.0` - Git MCP server  
- `tas-mcp-federation-init-v1.1.0` - Federation initialization job
- `tas-mcp-test-client-v1.1.0` - Integration test client

**Future MCP Servers will follow:**
- `tas-mcp-github-server-v1.0.0` - GitHub MCP server
- `tas-mcp-slack-server-v1.0.0` - Slack MCP server
- `tas-mcp-aws-server-v1.0.0` - AWS MCP server
- etc.

### Docker Images
```bash
# TAS MCP Server
tas-mcp/server:1.1.0

# Git MCP Server  
tas-mcp/git-mcp-server:1.0.0

# Future MCP servers
tas-mcp/github-mcp-server:1.0.0
tas-mcp/slack-mcp-server:1.0.0
```

## 🏷️ Docker Image Labels

All images include standardized OCI labels:

```dockerfile
# Standard OCI labels
LABEL org.opencontainers.image.title="TAS MCP Git Server"
LABEL org.opencontainers.image.version="1.0.0" 
LABEL org.opencontainers.image.created="2025-08-04T00:00:00Z"
LABEL org.opencontainers.image.revision="abc1234"
LABEL org.opencontainers.image.vendor="Tributary AI Services"
LABEL org.opencontainers.image.source="https://github.com/tributary-ai-services/tas-mcp"

# TAS MCP specific labels
LABEL com.tributary-ai.service="git-mcp-server"
LABEL com.tributary-ai.version="1.0.0"
LABEL com.tributary-ai.component="mcp-server"
```

## ☸️ Kubernetes Versioning

### Deployment Labels
```yaml
metadata:
  labels:
    app.kubernetes.io/name: git-mcp-server
    app.kubernetes.io/version: "1.0.0"
    app.kubernetes.io/component: mcp-server
    app.kubernetes.io/part-of: mcp-ecosystem
    app.kubernetes.io/managed-by: kustomize
```

### Image References
```yaml
spec:
  containers:
  - name: git-mcp
    image: tas-mcp/git-mcp-server:1.0.0
```

## 🚀 Version Override

### Docker Compose
```bash
# Set versions via environment variables
export TAS_MCP_VERSION=1.2.0
export GIT_MCP_VERSION=1.1.0

# Or use .env file
cp .env.example .env
# Edit .env with your versions

# Deploy with custom versions
./deployments/run-docker-compose.sh up
```

### Kubernetes  
```bash
# Set versions and deploy
export TAS_MCP_VERSION=1.2.0
export GIT_MCP_VERSION=1.1.0
cd deployments/k8s && ./deploy.sh deploy
```

### Direct Docker Commands
```bash
# Build with specific version
docker build -t tas-mcp/server:1.2.0 \
  --build-arg VERSION=1.2.0 \
  --build-arg BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ") \
  --build-arg VCS_REF=$(git rev-parse --short HEAD) \
  .
```

## 🔄 Version Lifecycle

### Development Versions
- Format: `1.1.0-dev`, `1.1.0-alpha1`, `1.1.0-beta1`
- Container: `tas-mcp-federation-server-v1.1.0-dev`
- Image: `tas-mcp/server:1.1.0-dev`

### Release Versions
- Format: `1.1.0`, `1.2.0`, `2.0.0`
- Container: `tas-mcp-federation-server-v1.1.0`
- Image: `tas-mcp/server:1.1.0`

### Version Tagging Strategy
```bash
# Semantic versioning
MAJOR.MINOR.PATCH

# Examples
1.0.0  - Initial release
1.1.0  - Federation foundation (current)
1.2.0  - Critical services wave
2.0.0  - Universal MCP hub
```

## 📊 Federation Server Registration

### Versioned Server IDs
Federation servers are registered with version-aware IDs:

```bash
# Server registration includes version
git-mcp-server-v1.0.0
github-mcp-server-v1.0.0  
slack-mcp-server-v1.0.0
```

### API Endpoints
```bash
# Federation API includes versioned server IDs
POST /api/v1/federation/servers/git-mcp-server-v1.0.0/invoke
GET  /api/v1/federation/servers/git-mcp-server-v1.0.0/health
```

## 🔍 Version Discovery

### Runtime Version Information
All containers expose version info via environment variables:
```bash
SERVICE_NAME=tas-mcp-git-server
SERVICE_VERSION=1.0.0
```

### Health Check Responses
```json
{
  "status": "healthy",
  "service": "git-mcp-server", 
  "version": "1.0.0",
  "build_date": "2025-08-04T00:00:00Z",
  "git_commit": "abc1234"
}
```

### Container Inspection
```bash
# View image labels
docker inspect tas-mcp/git-mcp-server:1.0.0

# View running container version
docker ps --format "table {{.Names}}\t{{.Image}}\t{{.Status}}"
```

## 🎯 Benefits

### Unique Naming
- **No conflicts** between different MCP server containers
- **Clear identification** of which version is running
- **Easy troubleshooting** with descriptive container names

### Version Management
- **Explicit versioning** for all components
- **Rollback capability** to previous versions
- **A/B testing** with multiple versions
- **Production stability** with pinned versions

### Federation Clarity
- **Server identification** includes version info
- **API compatibility** tracking per version
- **Health monitoring** per version
- **Metrics collection** segmented by version

---

**Next MCP Server Integration**: When adding new MCP servers (GitHub, Slack, AWS, etc.), follow this same versioning and naming pattern for consistency across the federation ecosystem.