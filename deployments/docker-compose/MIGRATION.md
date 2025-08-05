# 🔄 Migration Guide: Legacy to Modular Docker Compose

This guide helps you migrate from the legacy single Docker Compose file to the new modular structure.

## 📋 What Changed

### Old Structure (Legacy)
```
tas-mcp/
├── docker-compose.git-mcp.yml          # Monolithic file
└── deployments/
    └── run-docker-compose.sh            # Legacy script
```

### New Structure (Modular)
```
tas-mcp/
├── docker-compose.git-mcp.yml          # Legacy (deprecated)
└── deployments/
    ├── run-docker-compose.sh            # Legacy script (still works)
    └── docker-compose/                  # New modular approach
        ├── run.sh                       # New orchestration script
        ├── docker-compose.yml           # TAS MCP only
        ├── full-stack.yml              # Complete stack
        ├── git-mcp/
        │   └── docker-compose.yml      # Git MCP only
        ├── github-mcp/                 # Future servers
        └── slack-mcp/                  # Future servers
```

## 🎯 Migration Steps

### Step 1: Update Commands

**Legacy Commands:**
```bash
./deployments/run-docker-compose.sh up
./deployments/run-docker-compose.sh test
./deployments/run-docker-compose.sh down
```

**New Commands:**
```bash
cd deployments/docker-compose
./run.sh up full-stack
./run.sh test
./run.sh down full-stack
```

### Step 2: Environment Variables (Optional)

**Legacy (.env in root):**
```bash
TAS_MCP_VERSION=1.1.0
GIT_MCP_VERSION=1.0.0
```

**New (service-specific .env files):**
```bash
# deployments/docker-compose/.env
TAS_MCP_VERSION=1.1.0
GIT_MCP_VERSION=1.0.0

# deployments/docker-compose/git-mcp/.env  
GIT_MCP_VERSION=1.0.0
MCP_PORT=3000
LOG_LEVEL=info
```

### Step 3: Update Scripts/CI

**Legacy CI/Scripts:**
```yaml
# Old CI pipeline
- name: Deploy
  run: ./deployments/run-docker-compose.sh up
```

**New CI/Scripts:**
```yaml
# New CI pipeline
- name: Deploy
  run: |
    cd deployments/docker-compose
    ./run.sh up full-stack
```

## 🔀 Side-by-Side Comparison

| Feature | Legacy | New Modular |
|---------|--------|-------------|
| **File Organization** | Single monolithic file | Service-specific files |
| **Service Management** | All-or-nothing | Individual service control |
| **Version Management** | Global versions only | Per-service versioning |
| **Configuration** | Single .env file | Service-specific .env files |
| **Scalability** | Hard to add services | Easy service addition |
| **Testing** | Full stack only | Service-specific testing |
| **Dependencies** | Tightly coupled | Loosely coupled |

## 🚀 Benefits of Migration

### ✅ **Improved Developer Experience**
```bash
# Old: Start everything or nothing
./run-docker-compose.sh up

# New: Start what you need
./run.sh up tas-mcp          # Just federation server
./run.sh up git-mcp          # Just Git server  
./run.sh up full-stack       # Everything
```

### ✅ **Better Resource Management**
```bash
# Old: Always start all services (uses more resources)
docker-compose -f docker-compose.git-mcp.yml up

# New: Start only what you need (saves resources)
./run.sh up tas-mcp     # Minimal resource usage
```

### ✅ **Easier Troubleshooting**
```bash
# Old: Combined logs from all services
./run-docker-compose.sh logs

# New: Service-specific logs
./run.sh logs git-mcp
./run.sh logs tas-mcp
```

### ✅ **Simplified Adding New Services**
```bash
# Old: Edit monolithic file (error-prone)
vim docker-compose.git-mcp.yml

# New: Copy template and customize (safe)
cp github-mcp/docker-compose.yml.template github-mcp/docker-compose.yml
```

## 🔧 Compatibility Layer

### Legacy Support Maintained
The legacy approach continues to work:
```bash
# This still works exactly as before
./deployments/run-docker-compose.sh up
./deployments/run-docker-compose.sh test
./deployments/run-docker-compose.sh down
```

### Migration Notices
The legacy script now shows migration hints:
```bash
$ ./deployments/run-docker-compose.sh up
🔄 Starting TAS MCP and Git MCP services...
ℹ️  Note: Consider using the new modular approach in deployments/docker-compose/run.sh
```

## 📊 Feature Comparison

### Service Control

**Legacy (Limited):**
```bash
./run-docker-compose.sh up      # Start all services
./run-docker-compose.sh down    # Stop all services
```

**New (Flexible):**
```bash
./run.sh up full-stack         # Start all services
./run.sh up tas-mcp           # Start TAS MCP only
./run.sh up git-mcp           # Start Git MCP only
./run.sh down tas-mcp         # Stop TAS MCP only
```

### Testing Options

**Legacy (Limited):**
```bash
./run-docker-compose.sh test   # Full integration tests only
```

**New (Flexible):**
```bash
./run.sh test                  # Full integration tests
cd git-mcp && docker-compose up && curl localhost:3001/health  # Individual service testing
```

### Configuration Management

**Legacy (Global):**
```bash
# Single .env file for everything
TAS_MCP_VERSION=1.1.0
GIT_MCP_VERSION=1.0.0
GITHUB_TOKEN=xxx
SLACK_TOKEN=yyy
```

**New (Modular):**
```bash
# Main .env
TAS_MCP_VERSION=1.1.0

# git-mcp/.env
GIT_MCP_VERSION=1.0.0
REPOSITORY_PATH=/repositories

# github-mcp/.env (when implemented)
GITHUB_MCP_VERSION=1.0.0  
GITHUB_TOKEN=xxx

# slack-mcp/.env (when implemented)
SLACK_MCP_VERSION=1.0.0
SLACK_TOKEN=yyy
```

## 🎯 When to Migrate

### ✅ **Migrate Now If:**
- You're actively developing/testing individual MCP servers
- You want to save resources by running only needed services
- You're planning to add new MCP servers
- You prefer organized, maintainable configurations

### ⏸️ **Stay on Legacy If:**
- You have automation that depends on the current paths
- You always need the full stack running
- You prefer minimal changes to working systems
- You're waiting for specific features in the new approach

## 🚦 Migration Timeline

### Phase 1: **Soft Migration** (Current)
- ✅ New modular structure available
- ✅ Legacy approach still fully supported
- ✅ Migration hints in legacy scripts

### Phase 2: **Encouraged Migration** (Future)
- 📋 New features added to modular approach first
- 📋 Legacy approach marked as deprecated
- 📋 Migration guide prominently displayed

### Phase 3: **Legacy Sunset** (Long-term Future)
- 📋 Legacy files moved to `legacy/` directory
- 📋 Legacy scripts redirect to new approach
- 📋 Full migration recommended

## ❓ FAQ

### Q: Will my current setup break?
**A:** No, the legacy approach continues to work exactly as before.

### Q: Do I need to migrate immediately?
**A:** No, migration is optional. Both approaches are fully supported.

### Q: What if I have custom modifications to docker-compose.git-mcp.yml?
**A:** You can either:
1. Keep using the legacy approach
2. Port your modifications to the new modular structure
3. Use both approaches side-by-side

### Q: Can I use both approaches simultaneously?
**A:** Yes, but be careful about port conflicts and network names.

### Q: How do I migrate my CI/CD pipelines?
**A:** Update the working directory and script paths:
```bash
# Old
./deployments/run-docker-compose.sh up

# New  
cd deployments/docker-compose && ./run.sh up full-stack
```

---

**Recommendation**: Try the new modular approach in a development environment first, then migrate production workloads when you're comfortable with the new structure.