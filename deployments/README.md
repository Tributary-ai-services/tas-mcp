# 🚀 TAS MCP + Git MCP Local Deployment Guide

This guide provides instructions for deploying TAS MCP with Git MCP server integration locally using Docker Compose or Kubernetes.

## 📋 Prerequisites

### For Docker Compose
- Docker Engine 20.10+
- Docker Compose v2.0+
- 4GB RAM available
- 2GB disk space

### For Kubernetes
- Kubernetes cluster (Docker Desktop, minikube, kind, etc.)
- kubectl configured
- kustomize (auto-installed by script)
- 4GB RAM available
- 2GB disk space

## 🐳 Docker Compose Deployment

### Quick Start (New Modular Approach)

```bash
# 1. Start complete stack (TAS MCP + Git MCP + federation)
cd deployments/docker-compose
./run.sh up full-stack

# 2. Run integration tests
./run.sh test

# 3. Check status
./run.sh status full-stack

# 4. View logs
./run.sh logs full-stack

# 5. Stop services
./run.sh down full-stack
```

### Legacy Approach (Still Supported)

```bash
# 1. Start both TAS MCP and Git MCP servers
./deployments/run-docker-compose.sh up

# 2. Run integration tests
./deployments/run-docker-compose.sh test

# 3. Check status
./deployments/run-docker-compose.sh status

# 4. View logs
./deployments/run-docker-compose.sh logs

# 5. Stop services
./deployments/run-docker-compose.sh down
```

### Available Commands

| Command | Description |
|---------|-------------|
| `up` | Start all services (default) |
| `down` | Stop all services |
| `test` | Run integration tests |
| `logs` | Show logs from all services |
| `status` | Show status and health checks |
| `clean` | Stop services and remove volumes |
| `build` | Build images without starting |
| `help` | Show help message |

### Access Points (Docker Compose)

- **TAS MCP HTTP API**: http://localhost:8080
- **TAS MCP gRPC API**: localhost:50051
- **TAS MCP Health**: http://localhost:8082/health
- **Git MCP API**: http://localhost:3000
- **Git MCP Health**: http://localhost:3001/health
- **Federation API**: http://localhost:8080/api/v1/federation

## ☸️ Kubernetes Deployment

### Quick Start

```bash
# 1. Deploy to Kubernetes
cd deployments/k8s
./deploy.sh deploy

# 2. Check deployment status
./deploy.sh status

# 3. Run integration tests
./deploy.sh test

# 4. View logs
./deploy.sh logs

# 5. Delete deployment
./deploy.sh delete
```

### Available Commands

| Command | Description |
|---------|-------------|
| `deploy` | Deploy all resources (default) |
| `delete` | Delete all resources |
| `status` | Show deployment status |
| `logs` | Show logs from pods |
| `test` | Run integration tests |
| `build` | Build and load Docker images |
| `help` | Show help message |

### Access Points (Kubernetes)

- **TAS MCP HTTP API**: http://localhost:30080
- **TAS MCP gRPC API**: localhost:30051
- **TAS MCP Health**: http://localhost:30082/health
- **Git MCP API**: http://localhost:30300
- **Git MCP Health**: http://localhost:30301/health

## 🧪 Testing the Integration

### Automated Tests

Both deployment methods include automated integration tests:

```bash
# Docker Compose
./deployments/run-docker-compose.sh test

# Kubernetes  
cd deployments/k8s && ./deploy.sh test
```

### Manual Testing

1. **Check Service Health**:
   ```bash
   # TAS MCP Health
   curl http://localhost:8082/health  # Docker Compose
   curl http://localhost:30082/health # Kubernetes
   
   # Git MCP Health
   curl http://localhost:3001/health  # Docker Compose
   curl http://localhost:30301/health # Kubernetes
   ```

2. **List Federated Servers**:
   ```bash
   # Docker Compose
   curl http://localhost:8080/api/v1/federation/servers
   
   # Kubernetes
   curl http://localhost:30080/api/v1/federation/servers
   ```

3. **Test Git Operations**:
   ```bash
   # Git Status (Docker Compose)
   curl -X POST http://localhost:8080/api/v1/federation/servers/git-mcp-server/invoke \
        -H "Content-Type: application/json" \
        -d '{"id":"test","method":"git_status","params":{"repository":"/repositories"}}'
   
   # Git Status (Kubernetes)
   curl -X POST http://localhost:30080/api/v1/federation/servers/git-mcp-server/invoke \
        -H "Content-Type: application/json" \
        -d '{"id":"test","method":"git_status","params":{"repository":"/repositories"}}'
   ```

## 📁 Repository Structure

```
deployments/
├── README.md                    # This file
├── run-docker-compose.sh        # Docker Compose runner script
├── git-mcp/
│   ├── Dockerfile              # Git MCP server Docker image
│   └── entrypoint.sh           # Git MCP server entrypoint
└── k8s/
    ├── deploy.sh               # Kubernetes deployment script
    ├── kustomization.yaml      # Kustomize configuration
    ├── namespace.yaml          # Kubernetes namespace
    ├── configmap.yaml          # Configuration maps
    ├── pvc.yaml                # Persistent volume claims
    ├── git-mcp-deployment.yaml # Git MCP deployment
    ├── git-mcp-service.yaml    # Git MCP services
    ├── tas-mcp-deployment.yaml # TAS MCP deployment
    ├── tas-mcp-service.yaml    # TAS MCP services
    └── federation-init-job.yaml # Federation initialization job
```

## 🎯 What Gets Deployed

### Services

1. **TAS MCP Server**
   - Federation manager and orchestrator
   - HTTP/gRPC APIs for event processing
   - Health monitoring and metrics
   - Service discovery and registry

2. **Git MCP Server**
   - Official Git repository operations server
   - Python-based MCP server
   - Test repository with sample files
   - Health check endpoints

3. **Federation Initializer**
   - Automatically registers Git MCP with TAS MCP
   - Validates federation setup
   - One-time initialization job

### Test Repository

Both deployments include a test Git repository with:
- Sample files (README.md, hello.py, hello.go)
- Multiple branches (main, development)
- Initial commits for testing Git operations

## 🔧 Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_PORT` | Git MCP server port | 3000 |
| `MCP_HOST` | Git MCP server host | 0.0.0.0 |
| `REPOSITORY_PATH` | Git repository path | /repositories |
| `LOG_LEVEL` | Logging level | info |
| `FEDERATION_ENABLED` | Enable federation | true |
| `GIT_MCP_ENDPOINT` | Git MCP endpoint URL | http://git-mcp:3000 |

### Persistent Storage

- **Git Repositories**: Stored in persistent volumes/bind mounts
- **Logs**: Centralized logging with persistent storage
- **Configuration**: ConfigMaps for environment-specific settings

## 🐛 Troubleshooting

### Common Issues

1. **Services Won't Start**
   ```bash
   # Check Docker/Kubernetes status
   docker ps                    # Docker Compose
   kubectl get pods -n tas-mcp  # Kubernetes
   
   # Check logs
   ./deployments/run-docker-compose.sh logs  # Docker Compose
   cd deployments/k8s && ./deploy.sh logs    # Kubernetes
   ```

2. **Git MCP Server Not Registering**
   ```bash
   # Check federation logs
   docker logs federation-initializer         # Docker Compose
   kubectl logs job/federation-init -n tas-mcp # Kubernetes
   ```

3. **Port Conflicts**
   - Default ports: 8080, 50051, 8082 (TAS MCP), 3000, 3001 (Git MCP)
   - Modify docker-compose.yml or use different NodePort values

4. **Insufficient Resources**
   - Ensure 4GB RAM available
   - Check disk space (2GB minimum)
   - Adjust resource limits in manifests if needed

### Debug Commands

```bash
# Docker Compose
docker-compose -f docker-compose.git-mcp.yml ps
docker-compose -f docker-compose.git-mcp.yml logs -f

# Kubernetes
kubectl get all -n tas-mcp
kubectl describe pod <pod-name> -n tas-mcp
kubectl logs <pod-name> -n tas-mcp
```

## 🚀 Next Steps

1. **Test Git Operations**: Use the federation API to perform Git operations
2. **Add More Servers**: Register additional MCP servers with the federation
3. **Production Deployment**: Configure for production with proper secrets, ingress, etc.
4. **Monitoring**: Add Prometheus, Grafana, and observability stack
5. **Security**: Implement authentication, TLS, and network policies

## 📚 Related Documentation

- [Git MCP Integration Guide](../docs/GIT_MCP_INTEGRATION.md)
- [TAS MCP Federation Guide](../docs/FEDERATION.md)
- [Development Guide](../DEVELOPER.md)
- [API Documentation](../docs/API.md)

---

**Status**: ✅ Ready for local deployment and testing