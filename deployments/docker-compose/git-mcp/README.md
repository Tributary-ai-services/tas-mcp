# Git MCP Server Docker Compose

This directory contains the Docker Compose configuration for deploying the Git MCP Server as a standalone service.

## 🚀 Quick Start

```bash
# Navigate to this directory
cd deployments/docker-compose/git-mcp

# Create network (if not exists)
docker network create mcp-network

# Start Git MCP server
docker-compose up -d

# Check logs
docker-compose logs -f

# Stop server
docker-compose down
```

## 🔧 Configuration

### Environment Variables

Copy `.env.example` to `.env` and customize:

```bash
cp .env.example .env
# Edit .env with your configuration
```

### Version Override

```bash
# Set custom version
export GIT_MCP_VERSION=1.1.0
docker-compose up -d
```

## 🔗 Access Points

- **Git MCP API**: http://localhost:3000
- **Health Check**: http://localhost:3001/health

## 🧪 Testing

```bash
# Test health
curl http://localhost:3001/health

# Test git status (requires federation integration)
curl -X POST http://localhost:3000/git_status \
  -H "Content-Type: application/json" \
  -d '{"repository": "/repositories"}'
```

## 📁 Repository Setup

The server expects a Git repository at `/repositories`. The Docker Compose setup mounts `../../../examples/repositories` which contains a test repository.

## 🏷️ Service Details

- **Service Name**: git-mcp
- **Container Name**: tas-mcp-git-server-v{VERSION}
- **Image**: tas-mcp/git-mcp-server:{VERSION}
- **Network**: mcp-network (external)
- **Volumes**: git_mcp_logs

## 🔍 Troubleshooting

```bash
# Check container status
docker-compose ps

# View logs
docker-compose logs git-mcp

# Check network
docker network ls | grep mcp-network

# Inspect service
docker-compose config
```