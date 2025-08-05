# 🐳 TAS MCP Docker Compose Organization

This directory contains modular Docker Compose configurations for TAS MCP and all federated MCP servers, organized by service type for better maintainability and scalability.

## 📁 Directory Structure

```
deployments/docker-compose/
├── README.md                    # This file
├── run.sh                       # Main orchestration script
├── docker-compose.yml           # TAS MCP federation server only
├── full-stack.yml              # Complete stack (TAS MCP + Git MCP + tests)
├── .env.example                # Environment template
├── git-mcp/                    # Git MCP server
│   ├── docker-compose.yml      # Git MCP standalone
│   ├── .env.example           # Git MCP environment
│   └── README.md              # Git MCP documentation
├── github-mcp/                 # GitHub MCP server (template)
│   └── docker-compose.yml.template
├── slack-mcp/                  # Slack MCP server (template)
│   └── docker-compose.yml.template
└── [future-mcp-servers]/       # Additional MCP servers
```

## 🚀 Quick Start

### Full Stack Deployment (Recommended)
```bash
# Navigate to docker-compose directory
cd deployments/docker-compose

# Start complete stack (TAS MCP + Git MCP + federation)
./run.sh up full-stack

# Run integration tests
./run.sh test

# Check status
./run.sh status full-stack

# Stop everything
./run.sh down full-stack
```

### Individual Service Deployment
```bash
# Start only TAS MCP federation server
./run.sh up tas-mcp

# Start only Git MCP server
./run.sh up git-mcp

# Build specific service
./run.sh build git-mcp

# View logs for specific service
./run.sh logs tas-mcp
```

## 🎯 Available Services

### Core Services

| Service | Description | Ports | Status |
|---------|-------------|-------|--------|
| `tas-mcp` | TAS MCP federation server | 8080, 50051, 8082 | ✅ Available |
| `git-mcp` | Git repository operations | 3000, 3001 | ✅ Available |
| `full-stack` | Complete integrated stack | All above | ✅ Available |

### Future MCP Servers (Templates Ready)

| Service | Description | Ports | Status |
|---------|-------------|-------|--------|
| `github-mcp` | GitHub API integration | 3100, 3101 | 📋 Template |
| `slack-mcp` | Slack team communication | 3200, 3201 | 📋 Template |
| `aws-mcp` | AWS cloud services | 3300, 3301 | 🔮 Planned |
| `database-mcp` | Database operations | 3400, 3401 | 🔮 Planned |

## 🔧 Configuration

### Environment Variables

#### Global Configuration (.env)
```bash
# Service versions
TAS_MCP_VERSION=1.1.0
GIT_MCP_VERSION=1.0.0

# Build metadata (auto-generated)
BUILD_DATE=2025-08-04T00:00:00Z
VCS_REF=abc1234

# Network
DOCKER_NETWORK_NAME=mcp-network
```

#### Service-Specific Configuration
Each MCP server has its own `.env.example`:
- `git-mcp/.env.example` - Git server configuration
- `github-mcp/.env.example` - GitHub API tokens (when implemented)
- `slack-mcp/.env.example` - Slack bot tokens (when implemented)

### Version Override
```bash
# Set custom versions
export TAS_MCP_VERSION=1.2.0
export GIT_MCP_VERSION=1.1.0

# Deploy with custom versions
./run.sh up full-stack
```

## 📊 Service Management

### Health Monitoring
```bash
# Check all service health
./run.sh status full-stack

# Individual health checks
curl http://localhost:8082/health  # TAS MCP
curl http://localhost:3001/health  # Git MCP
```

### Container Management
```bash
# View running containers
docker ps --format "table {{.Names}}\t{{.Image}}\t{{.Status}}"

# Unique container names with versions
# tas-mcp-federation-server-v1.1.0
# tas-mcp-git-server-v1.0.0
# tas-mcp-federation-init-v1.1.0
```

### Network Configuration
All services use the shared `mcp-network`:
```bash
# View network
docker network inspect mcp-network

# Services can communicate via hostnames:
# - tas-mcp-server (TAS MCP federation server)
# - git-mcp-server (Git MCP server)
```

## 🧪 Testing

### Integration Tests
```bash
# Run complete integration test suite
./run.sh test

# Manual federation testing
curl http://localhost:8080/api/v1/federation/servers
curl -X POST http://localhost:8080/api/v1/federation/servers/git-mcp-server-v1.0.0/invoke \
  -H "Content-Type: application/json" \
  -d '{"method":"git_status","params":{"repository":"/repositories"}}'
```

### Development Testing
```bash
# Git MCP standalone testing
cd git-mcp
docker-compose up -d
curl http://localhost:3001/health

# TAS MCP standalone testing  
docker-compose -f docker-compose.yml up -d
curl http://localhost:8082/health
```

## 🔄 Adding New MCP Servers

### 1. Create Service Directory
```bash
mkdir -p deployments/docker-compose/new-mcp-server
```

### 2. Copy Template
```bash
# Use existing template as base
cp github-mcp/docker-compose.yml.template new-mcp-server/docker-compose.yml
```

### 3. Customize Configuration
- Update service name, ports, environment
- Create Dockerfile in `deployments/new-mcp-server/`
- Add service-specific environment variables

### 4. Update Orchestration
Add service to `full-stack.yml`:
```yaml
new-mcp:
  extends:
    file: new-mcp-server/docker-compose.yml
    service: new-mcp
```

### 5. Update Run Script
Add service option to `run.sh`:
```bash
case $SERVICE in
    # ... existing services ...
    "new-mcp")
        COMPOSE_FILE="new-mcp-server/docker-compose.yml"
        ;;
esac
```

## 🎯 Port Allocation Strategy

To avoid conflicts, MCP servers use port ranges:

| Service Type | Port Range | Example |
|--------------|------------|---------|
| TAS MCP Core | 8080-8099 | 8080 (HTTP), 8082 (health) |
| Git MCP | 3000-3099 | 3000 (API), 3001 (health) |
| GitHub MCP | 3100-3199 | 3100 (API), 3101 (health) |
| Slack MCP | 3200-3299 | 3200 (API), 3201 (health) |
| AWS MCP | 3300-3399 | 3300 (API), 3301 (health) |
| Database MCP | 3400-3499 | 3400 (API), 3401 (health) |

## 🏷️ Container Naming Convention

All containers follow the pattern:
```
tas-mcp-<service-type>-v<version>
```

Examples:
- `tas-mcp-federation-server-v1.1.0`
- `tas-mcp-git-server-v1.0.0`
- `tas-mcp-github-server-v1.0.0`
- `tas-mcp-slack-server-v1.0.0`

## 🔍 Troubleshooting

### Common Issues

1. **Port Conflicts**
   ```bash
   # Check what's using ports
   lsof -i :8080
   netstat -tulpn | grep :8080
   ```

2. **Network Issues**
   ```bash
   # Recreate network
   docker network rm mcp-network
   docker network create mcp-network
   ```

3. **Volume Issues**
   ```bash
   # Clean up volumes
   ./run.sh clean full-stack
   docker volume prune
   ```

4. **Build Issues**
   ```bash
   # Force rebuild
   ./run.sh build full-stack --no-cache
   ```

### Debug Commands
```bash
# View all MCP containers
docker ps --filter "label=com.tributary-ai.service"

# Check logs
./run.sh logs full-stack

# Inspect configuration
docker-compose -f full-stack.yml config

# Network connectivity test
docker run --rm --network mcp-network curlimages/curl:8.8.0 \
  curl -f http://tas-mcp-server:8080/health
```

## 📈 Migration from Legacy

### Old Structure (Deprecated)
```bash
# Legacy command (still works)
../run-docker-compose.sh up
```

### New Structure (Recommended)  
```bash
# New modular approach
./run.sh up full-stack
```

### Benefits of New Structure
- ✅ **Modular**: Each MCP server is independently deployable
- ✅ **Scalable**: Easy to add new MCP servers
- ✅ **Maintainable**: Clear separation of concerns
- ✅ **Testable**: Individual service testing
- ✅ **Versioned**: Proper version management per service
- ✅ **Organized**: Clear directory structure

---

**Next Steps**: When new MCP servers are ready (GitHub, Slack, AWS, etc.), follow the template pattern to add them to this organized structure.