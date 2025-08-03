# 👨‍💻 TAS MCP Server Developer Guide

Welcome to the TAS MCP Server development guide! This document provides comprehensive information for developers who want to contribute to or extend the project.

## 📋 Table of Contents

- [Development Environment Setup](#development-environment-setup)
- [Project Structure](#project-structure)
- [Development Workflow](#development-workflow)
- [Code Style and Standards](#code-style-and-standards)
- [Testing Guidelines](#testing-guidelines)
- [Debugging](#debugging)
- [Performance Optimization](#performance-optimization)
- [Contributing](#contributing)
- [Release Process](#release-process)

## 🛠️ Development Environment Setup

### Prerequisites

- **Go 1.22+** - [Install Go](https://golang.org/doc/install)
- **Docker** - [Install Docker](https://docs.docker.com/get-docker/)
- **Make** - Usually pre-installed on Unix systems
- **Git** - [Install Git](https://git-scm.com/downloads)

### Optional Tools

- **Kubernetes** - For testing K8s deployments locally
- **Kind/Minikube** - Local Kubernetes clusters
- **Postman/Insomnia** - API testing
- **grpcurl** - gRPC testing CLI
- **k9s** - Kubernetes TUI

### Initial Setup

```bash
# Clone the repository
git clone https://github.com/tributary-ai-services/tas-mcp.git
cd tas-mcp

# Install development dependencies
make init

# Verify setup
make test
```

### IDE Configuration

#### VS Code

1. Install recommended extensions:
   ```json
   {
     "recommendations": [
       "golang.go",
       "zxh404.vscode-proto3",
       "ms-azuretools.vscode-docker",
       "ms-kubernetes-tools.vscode-kubernetes-tools",
       "streetsidesoftware.code-spell-checker"
     ]
   }
   ```

2. Use workspace settings:
   ```json
   {
     "go.lintTool": "golangci-lint",
     "go.lintFlags": ["--fast"],
     "go.formatTool": "goimports",
     "go.useLanguageServer": true,
     "[proto3]": {
       "editor.formatOnSave": true
     }
   }
   ```

#### GoLand/IntelliJ

1. Enable Go modules support
2. Configure golangci-lint as external tool
3. Set up file watchers for proto generation

## 🏗️ Project Structure

```
tas-mcp/
├── cmd/                    # Application entry points
│   ├── server/            # Main server binary
│   └── cli/               # CLI tools (future)
├── internal/              # Private application code
│   ├── config/           # Configuration management
│   ├── forwarding/       # Event forwarding logic
│   ├── grpc/            # gRPC server implementation
│   ├── http/            # HTTP server implementation
│   └── logger/          # Structured logging
├── pkg/                   # Public libraries (future)
├── proto/                # Protocol buffer definitions
│   └── mcp.proto        # MCP service definition
├── gen/                  # Generated code (git-ignored)
│   └── mcp/v1/          # Generated proto code
├── examples/             # Integration examples
│   └── triggers/        # Argo Events triggers
├── k8s/                  # Kubernetes manifests
│   ├── base/            # Base Kustomization
│   └── overlays/        # Environment overlays
├── scripts/              # Utility scripts
├── docs/                 # Documentation
├── test/                 # Integration tests
└── tools/                # Build tools
```

### Package Guidelines

- `internal/`: Private packages not meant for external use
- `pkg/`: Public packages that can be imported by other projects
- `cmd/`: Main applications for this project
- `gen/`: Auto-generated code (do not edit manually)

## 🔄 Development Workflow

### 1. Feature Development

```bash
# Create feature branch
git checkout -b feature/my-new-feature

# Make changes
vim internal/...

# Run tests frequently
make test

# Format and lint
make fmt lint

# Commit changes
git add .
git commit -m "feat: add new feature"
```

### 2. Running Locally

```bash
# Build and run
make run

# Run with hot reload (requires air)
make dev

# Run with specific config
./bin/tas-mcp -config configs/dev.json

# Run with Docker
make docker-run
```

### 3. Testing Changes

```bash
# Unit tests
make test

# Coverage report
make coverage

# Integration tests
make test-integration

# Specific package
go test -v ./internal/forwarding/...

# With race detection
go test -race ./...
```

### 4. Protocol Buffer Changes

```bash
# Edit proto file
vim proto/mcp.proto

# Generate code
make proto

# Verify generated code
ls -la gen/mcp/v1/
```

## 📝 Code Style and Standards

### Go Code Standards

1. **Follow [Effective Go](https://golang.org/doc/effective_go)**
2. **Use meaningful variable names**
   ```go
   // Bad
   var d int // elapsed time in days
   
   // Good
   var elapsedDays int
   ```

3. **Error handling**
   ```go
   // Always handle errors explicitly
   if err != nil {
       return fmt.Errorf("failed to process event: %w", err)
   }
   ```

4. **Comments and documentation**
   ```go
   // EventForwarder manages the forwarding of events to configured targets.
   // It implements retry logic, circuit breaking, and metric collection.
   type EventForwarder struct {
       // ...
   }
   ```

### Linting Rules

The project uses `golangci-lint` with custom configuration:

```yaml
linters:
  enable:
    - gofmt
    - golint
    - govet
    - ineffassign
    - misspell
    - unconvert
    - gocyclo
    - gosec
```

Run linting:
```bash
make lint

# Auto-fix some issues
golangci-lint run --fix
```

### Commit Message Convention

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Test additions/changes
- `chore`: Build process or auxiliary tool changes

Examples:
```bash
git commit -m "feat(forwarding): add kafka target support"
git commit -m "fix(grpc): handle connection timeout properly"
git commit -m "docs: update API examples in README"
```

## 🧪 Testing Guidelines

### Unit Tests

1. **Test file naming**: `*_test.go`
2. **Test function naming**: `Test<FunctionName>`
3. **Use table-driven tests**:
   ```go
   func TestEventValidation(t *testing.T) {
       tests := []struct {
           name    string
           event   *Event
           wantErr bool
       }{
           {
               name:    "valid event",
               event:   &Event{ID: "123", Type: "test"},
               wantErr: false,
           },
           {
               name:    "missing ID",
               event:   &Event{Type: "test"},
               wantErr: true,
           },
       }
       
       for _, tt := range tests {
           t.Run(tt.name, func(t *testing.T) {
               err := ValidateEvent(tt.event)
               if (err != nil) != tt.wantErr {
                   t.Errorf("ValidateEvent() error = %v, wantErr %v", err, tt.wantErr)
               }
           })
       }
   }
   ```

### Integration Tests

Integration tests are located in `test/integration/` and use build tags:

```go
// test/integration/server_integration_test.go
//go:build integration

func TestEventForwardingIntegration(t *testing.T) {
    // Create mock target server
    targetCapture := testutil.NewMockEventCapture()
    mockServer := testutil.CreateMockHTTPServer(t, targetCapture.Handler())
    defer mockServer.Close()
    
    // Test end-to-end event forwarding
    // ... test implementation
}
```

Run integration tests:
```bash
make test-integration
# or directly with go
go test -tags=integration ./test/integration/...
```

### Benchmarks

Benchmark tests are in `test/benchmark/` and measure performance:

```go
// test/benchmark/benchmark_test.go
func BenchmarkGRPCEventIngestion(b *testing.B) {
    server := setupTestServer(b)
    defer server.Stop()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Benchmark gRPC event ingestion
    }
}
```

Run benchmarks:
```bash
make test-benchmark
# or directly with go
go test -bench=. ./test/benchmark/...
```

### Test Utilities

The project includes comprehensive test utilities in `internal/testutil/`:

```go
// Create test events
event := testutil.CreateTestEvent("test-123", "user.created", "auth-service")

// Create mock HTTP servers
server := testutil.CreateMockHTTPServer(t, nil)
defer server.Close()

// Event matching
matcher := testutil.MatchEventType("user.created")
if matcher.Matches(event) {
    // Event matches criteria
}
```

### Test Coverage

Check test coverage:
```bash
make test-coverage
# View coverage report
go tool cover -html=coverage.out
```

Current coverage:
- **Config Package**: 77.6% statement coverage
- **Forwarding Package**: 60.1% statement coverage
- **Overall**: Target is 70%+ for production packages

## 🐛 Debugging

### Local Debugging

1. **Using Delve**:
   ```bash
   # Install delve
   go install github.com/go-delve/delve/cmd/dlv@latest
   
   # Debug the server
   dlv debug ./cmd/server/main.go
   
   # Set breakpoint
   (dlv) break main.main
   (dlv) continue
   ```

2. **VS Code debugging**:
   ```json
   {
     "version": "0.2.0",
     "configurations": [
       {
         "name": "Debug Server",
         "type": "go",
         "request": "launch",
         "mode": "debug",
         "program": "${workspaceFolder}/cmd/server",
         "env": {
           "LOG_LEVEL": "debug"
         }
       }
     ]
   }
   ```

### Remote Debugging

```yaml
# In docker-compose.override.yml
services:
  tas-mcp-server:
    ports:
      - "40000:40000"  # Delve port
    command: ["dlv", "debug", "--headless", "--listen=:40000", "--api-version=2", "./cmd/server"]
```

### Logging

```go
// Use structured logging
logger.Info("Processing event",
    zap.String("event_id", event.ID),
    zap.String("event_type", event.Type),
    zap.Duration("latency", latency),
)

// Debug logging
logger.Debug("Forwarding rule evaluation",
    zap.Any("rule", rule),
    zap.Bool("matched", matched),
)
```

## ⚡ Performance Optimization

### Profiling

1. **CPU Profiling**:
   ```go
   import _ "net/http/pprof"
   
   // In main()
   go func() {
       log.Println(http.ListenAndServe("localhost:6060", nil))
   }()
   ```
   
   ```bash
   # Capture CPU profile
   go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
   
   # Analyze
   (pprof) top
   (pprof) list main.processEvent
   ```

2. **Memory Profiling**:
   ```bash
   go tool pprof http://localhost:6060/debug/pprof/heap
   ```

3. **Trace Analysis**:
   ```bash
   curl http://localhost:6060/debug/pprof/trace?seconds=5 > trace.out
   go tool trace trace.out
   ```

### Best Practices

1. **Use sync.Pool for frequently allocated objects**
2. **Implement proper context cancellation**
3. **Use buffered channels appropriately**
4. **Profile before optimizing**
5. **Benchmark critical paths**

## 🤝 Contributing

### Pull Request Process

1. **Fork and clone the repository**
2. **Create a feature branch**
3. **Make your changes**
4. **Add/update tests**
5. **Update documentation**
6. **Run full test suite**
7. **Submit PR with clear description**

### Code Review Checklist

- [ ] Tests pass locally
- [ ] Code follows style guidelines
- [ ] Documentation is updated
- [ ] No security vulnerabilities
- [ ] Performance impact considered
- [ ] Backward compatibility maintained

### Getting Help

- 💬 **Discord**: [Join our community](https://discord.gg/tas-mcp)
- 📧 **Email**: dev@tributary-ai-services.com
- 🐛 **Issues**: [GitHub Issues](https://github.com/tributary-ai-services/tas-mcp/issues)

## 🚀 Release Process

### Version Numbering

We follow [Semantic Versioning](https://semver.org/):
- **MAJOR**: Incompatible API changes
- **MINOR**: Backward-compatible functionality
- **PATCH**: Backward-compatible bug fixes

### Release Steps

1. **Update version**:
   ```bash
   # Update version in code
   VERSION=1.2.0 make release-prep
   ```

2. **Create changelog**:
   ```bash
   git log --oneline v1.1.0..HEAD > CHANGELOG-1.2.0.md
   ```

3. **Tag release**:
   ```bash
   git tag -a v1.2.0 -m "Release v1.2.0"
   git push origin v1.2.0
   ```

4. **Build and push Docker images**:
   ```bash
   VERSION=1.2.0 make docker-push
   ```

### CI/CD Pipeline

The project uses GitHub Actions for:
- Running tests on every PR
- Building Docker images on merge to main
- Creating releases on tags
- Security scanning with Trivy
- Code coverage reporting

## 📚 Additional Resources

- [Go Documentation](https://golang.org/doc/)
- [Protocol Buffers Guide](https://developers.google.com/protocol-buffers)
- [gRPC Go Tutorial](https://grpc.io/docs/languages/go/)
- [Kubernetes Development](https://kubernetes.io/docs/contribute/generate-ref-docs/contribute-upstream/)
- [Effective Go](https://golang.org/doc/effective_go)

---

Happy coding! 🚀 If you have questions or need help, don't hesitate to reach out to the maintainers.