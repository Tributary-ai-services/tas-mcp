# Git MCP Server Integration Guide

This guide explains how to integrate the official Git MCP Server with TAS MCP for Git repository operations via federation.

## Overview

The Git MCP Server is the official Model Context Protocol server for Git repository interaction and automation. It provides tools for reading, searching, and manipulating Git repositories through Large Language Models.

## Git MCP Server Details

- **Provider**: Model Context Protocol (Official)
- **Repository**: https://github.com/modelcontextprotocol/servers/tree/main/src/git
- **Language**: Python
- **License**: MIT
- **Category**: Development Tools

## Capabilities

The Git MCP Server provides the following operations:

| Tool | Description |
|------|-------------|
| `git_status` | Shows working tree status |
| `git_diff_unstaged` | Displays unstaged changes |
| `git_diff_staged` | Shows staged changes |
| `git_commit` | Records repository changes |
| `git_add` | Stages file contents |
| `git_reset` | Unstages changes |
| `git_log` | Shows commit history |
| `git_create_branch` | Creates new branches |
| `git_checkout` | Switches branches |

## Installation Options

### Option 1: Using uvx (Recommended)
```bash
# Install uvx if not already installed
pip install uvx

# Install Git MCP server
uvx install mcp-server-git

# Run the server
uvx mcp-server-git --repository /path/to/repo --port 3000
```

### Option 2: Using pip
```bash
# Install the package
pip install mcp-server-git

# Run via Python module
python -m mcp_server_git --repository /path/to/repo --port 3000
```

### Option 3: Using Docker
```bash
# Build or pull the Docker image
docker build -t git-mcp-server .

# Run the container
docker run -p 3000:3000 -v /path/to/repo:/repo git-mcp-server
```

## Federation Configuration

### 1. Server Registration

Add the Git MCP server to your TAS MCP federation:

```go
gitServer := &federation.MCPServer{
    ID:          "git-mcp-server",
    Name:        "Git MCP Server",
    Description: "Official Git repository interaction and automation server",
    Version:     "1.0.0",
    Category:    "development-tools",
    Endpoint:    "http://localhost:3000",
    Protocol:    federation.ProtocolHTTP,
    Auth: federation.AuthConfig{
        Type:   federation.AuthNone,
        Config: map[string]string{},
    },
    Capabilities: []string{
        "git_status", "git_diff_unstaged", "git_diff_staged",
        "git_commit", "git_add", "git_reset", "git_log",
        "git_create_branch", "git_checkout",
    },
    HealthCheck: federation.HealthCheckConfig{
        Enabled:  true,
        Interval: 30 * time.Second,
        Timeout:  10 * time.Second,
        Path:     "/health",
    },
}

// Register with TAS Manager
err := tasManager.RegisterServer(gitServer)
```

### 2. Registry Entry

The Git MCP server has been added to the TAS MCP registry (`registry/mcp-servers.json`):

```json
{
  "id": "git-mcp-server",
  "name": "Git MCP Server",
  "description": "Official Git repository interaction and automation server from Model Context Protocol",
  "version": "1.0.0",
  "category": "development-tools",
  "features": [
    "Git status and diff operations",
    "Branch creation and switching", 
    "Commit and staging operations",
    "Repository history access",
    "File manipulation via Git",
    "Working tree management"
  ]
}
```

## Usage Examples

### 1. Check Repository Status

```go
request := &federation.MCPRequest{
    ID:     "git-status-check",
    Method: "git_status",
    Params: map[string]interface{}{
        "repository": "/path/to/your/repo",
    },
}

response, err := tasManager.InvokeServer(ctx, "git-mcp-server", request)
```

### 2. Create a New Branch

```go
request := &federation.MCPRequest{
    ID:     "create-feature-branch",
    Method: "git_create_branch",
    Params: map[string]interface{}{
        "repository":  "/path/to/your/repo",
        "branch_name": "feature/new-feature",
    },
}

response, err := tasManager.InvokeServer(ctx, "git-mcp-server", request)
```

### 3. Stage and Commit Changes

```go
// Stage files
addRequest := &federation.MCPRequest{
    ID:     "git-add-files",
    Method: "git_add",
    Params: map[string]interface{}{
        "repository": "/path/to/your/repo",
        "files":      []string{"file1.go", "file2.go"},
    },
}

// Commit changes
commitRequest := &federation.MCPRequest{
    ID:     "git-commit-changes",
    Method: "git_commit",
    Params: map[string]interface{}{
        "repository": "/path/to/your/repo",
        "message":    "Add new features",
    },
}
```

## Testing the Integration

### Automated Test Script

Run the provided test script to verify the integration:

```bash
./examples/git-mcp-test.sh
```

This script will:
1. Check if TAS MCP server is running
2. Install and start Git MCP server
3. Register Git MCP server with TAS MCP federation
4. Test federation endpoints
5. Perform sample Git operations
6. Validate integration

### Docker Compose Testing

Use the provided Docker Compose configuration:

```bash
# Start both servers
docker-compose -f examples/docker-compose.git-mcp.yml up -d

# Run tests
docker-compose -f examples/docker-compose.git-mcp.yml --profile test up test-client

# Cleanup
docker-compose -f examples/docker-compose.git-mcp.yml down
```

### Manual Testing

1. **Start TAS MCP Server**:
   ```bash
   make build && ./bin/tas-mcp
   ```

2. **Start Git MCP Server**:
   ```bash
   uvx mcp-server-git --repository /path/to/repo --port 3000
   ```

3. **Register via HTTP API**:
   ```bash
   curl -X POST http://localhost:8080/api/v1/federation/servers \
        -H "Content-Type: application/json" \
        -d @examples/federation/git-server-config.json
   ```

4. **Test Operations**:
   ```bash
   curl -X POST http://localhost:8080/api/v1/federation/servers/git-mcp-server/invoke \
        -H "Content-Type: application/json" \
        -d '{"method": "git_status", "params": {"repository": "/path/to/repo"}}'
   ```

## Production Deployment

### Environment Variables

```bash
# Git MCP Server Configuration
GIT_MCP_ENDPOINT=http://git-mcp-server:3000
GIT_MCP_REPOSITORY_PATH=/repositories
GIT_MCP_HEALTH_CHECK_INTERVAL=30s

# TAS MCP Federation
FEDERATION_ENABLED=true
FEDERATION_AUTO_DISCOVERY=true
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: git-mcp-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: git-mcp-server
  template:
    metadata:
      labels:
        app: git-mcp-server
    spec:
      containers:
      - name: git-mcp
        image: python:3.11-slim
        ports:
        - containerPort: 3000
        env:
        - name: REPOSITORY_PATH
          value: "/repositories"
        volumeMounts:
        - name: repositories
          mountPath: /repositories
        command:
        - bash
        - -c
        - |
          pip install mcp-server-git &&
          python -m mcp_server_git --port 3000 --repository /repositories
      volumes:
      - name: repositories
        persistentVolumeClaim:
          claimName: git-repositories-pvc
```

## Authentication & Security

### Current Status
- **Authentication**: None (development/internal use)
- **Authorization**: Repository-level access control
- **Transport**: HTTP (upgrade to HTTPS in production)

### Production Recommendations
1. **Enable HTTPS/TLS** for secure communication
2. **Implement API authentication** (API keys, JWT tokens)
3. **Repository access control** based on user permissions
4. **Rate limiting** to prevent abuse
5. **Audit logging** for Git operations

## Troubleshooting

### Common Issues

1. **Git MCP Server Not Starting**
   - Check Python installation and dependencies
   - Verify repository path permissions
   - Check port availability

2. **Federation Registration Fails**
   - Verify TAS MCP server is running
   - Check network connectivity between servers
   - Validate JSON configuration format

3. **Git Operations Fail**
   - Ensure repository path is accessible
   - Check Git repository validity
   - Verify file permissions

### Debug Commands

```bash
# Check Git MCP server status
curl http://localhost:3000/health

# List federation servers
curl http://localhost:8080/api/v1/federation/servers

# Check federation health
curl http://localhost:8080/api/v1/federation/servers/git-mcp-server/health
```

## Next Steps

1. **Enhanced Authentication**: Implement secure authentication between TAS MCP and Git MCP
2. **Multi-Repository Support**: Support multiple Git repositories in a single server
3. **Advanced Git Operations**: Add support for merge, rebase, and other advanced Git operations
4. **GitHub Integration**: Extend to GitHub API operations (issues, PRs, etc.)
5. **GitLab Integration**: Add support for GitLab MCP server
6. **Performance Optimization**: Implement caching and connection pooling

## Related Documentation

- [TAS MCP Federation Guide](./FEDERATION.md)
- [MCP Server Registry](../registry/README.md)
- [Development Tools Integration](./DEVELOPMENT_TOOLS.md)
- [Git MCP Server Source](https://github.com/modelcontextprotocol/servers/tree/main/src/git)

---

**Status**: ✅ Complete - Git MCP Server successfully integrated with TAS MCP federation infrastructure.