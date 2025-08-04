# 📡 TAS Model Context Protocol (MCP) Server

[![Build](https://github.com/tributary-ai-services/tas-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/tributary-ai-services/tas-mcp/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/tributary-ai-services/tas-mcp)](https://goreportcard.com/report/github.com/tributary-ai-services/tas-mcp)
[![Test Coverage](https://img.shields.io/badge/coverage-64.6%25-green?style=flat&logo=go)](https://github.com/tributary-ai-services/tas-mcp#-testing--quality-metrics)
[![Go Reference](https://pkg.go.dev/badge/github.com/tributary-ai-services/tas-mcp.svg)](https://pkg.go.dev/github.com/tributary-ai-services/tas-mcp)
[![License](https://img.shields.io/github/license/tributary-ai-services/tas-mcp.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/tributary-ai-services/tas-mcp.svg)](https://github.com/tributary-ai-services/tas-mcp/releases)
[![Join Slack](https://img.shields.io/badge/chat-on%20Slack-blue?logo=slack)](https://join.slack.com/t/tributaryaiservcies/shared_invite/zt-3alfz1o77-kwk3mL~1oBdMAEEY9VEVSw)

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io)
[![MCP Federation](https://img.shields.io/badge/MCP-Federation%20Ready-FF6B6B?style=flat&logo=network-wired)](https://github.com/tributary-ai-services/tas-mcp)

The **TAS MCP Server** is a high-performance, cloud-native event gateway and ingestion service that implements the [Model Context Protocol](https://github.com/anthropics/model-context-protocol) to support RAG pipelines, event-driven architectures, and workflow orchestration across distributed AI systems.

## 🌟 Key Features

- **🚀 Multi-Protocol Support**: HTTP REST API and bidirectional gRPC streaming
- **🔄 Smart Event Forwarding**: Rule-based routing with condition evaluation
- **🎯 Event Transformation**: Template-based and programmatic event transformation
- **🔌 Integration Ready**: Native support for Argo Events, Kafka, webhooks, and more
- **📚 MCP Server Registry**: Comprehensive catalog of MCP servers and capabilities
- **📊 Observability**: Built-in metrics, health checks, and distributed tracing support
- **🔒 Production Ready**: Rate limiting, circuit breakers, and retry logic
- **☁️ Cloud Native**: Kubernetes-native with Helm charts and operators
- **🎨 Extensible**: Plugin architecture for custom forwarders and processors

## 🚀 Quick Start

### Using Docker

```bash
# Run with Docker
docker run -p 8080:8080 -p 50051:50051 ghcr.io/tributary-ai-services/tas-mcp:latest

# Or build locally
make docker
make docker-run
```

### Using Docker Compose

```bash
# Start all services
make docker-compose

# View logs
docker-compose logs -f tas-mcp-server
```

### Local Development

```bash
# Install dependencies
make init

# Run locally
make run

# Run with hot reload
make dev
```

## 📡 API Usage

### HTTP API

```bash
# Ingest a single event
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_id": "evt-123",
    "event_type": "user.created",
    "source": "auth-service",
    "data": "{\"user_id\": \"usr-456\", \"email\": \"user@example.com\"}"
  }'

# Batch event ingestion
curl -X POST http://localhost:8080/api/v1/events/batch \
  -H "Content-Type: application/json" \
  -d '[
    {"event_id": "evt-1", "event_type": "order.created", "source": "order-service", "data": "{}"},
    {"event_id": "evt-2", "event_type": "payment.processed", "source": "payment-service", "data": "{}"}
  ]'

# Health check
curl http://localhost:8082/health
```

### gRPC API

```go
// Example Go client
import (
    mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
    "google.golang.org/grpc"
)

conn, _ := grpc.Dial("localhost:50051", grpc.WithInsecure())
client := mcpv1.NewMCPServiceClient(conn)

// Ingest event
resp, _ := client.IngestEvent(ctx, &mcpv1.IngestEventRequest{
    EventId:   "evt-123",
    EventType: "user.action",
    Source:    "webapp",
    Data:      `{"action": "login"}`,
})

// Stream events
stream, _ := client.EventStream(ctx)
```

## 🔧 Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_PORT` | `8080` | HTTP API server port |
| `GRPC_PORT` | `50051` | gRPC server port |
| `HEALTH_CHECK_PORT` | `8082` | Health check endpoint port |
| `LOG_LEVEL` | `info` | Logging level (debug, info, warn, error) |
| `FORWARDING_ENABLED` | `false` | Enable event forwarding |
| `FORWARDING_WORKERS` | `5` | Number of forwarding workers |
| `MAX_EVENT_SIZE` | `1048576` | Maximum event size in bytes (1MB) |

### Configuration File

```json
{
  "HTTPPort": 8080,
  "GRPCPort": 50051,
  "LogLevel": "info",
  "forwarding": {
    "enabled": true,
    "targets": [
      {
        "id": "argo-events",
        "type": "webhook",
        "endpoint": "http://argo-events-webhook:12000",
        "rules": [
          {
            "conditions": [
              {"field": "event_type", "operator": "contains", "value": "critical"}
            ]
          }
        ]
      }
    ]
  }
}
```

## 🏗️ Architecture

The TAS MCP Server follows a modular, event-driven architecture:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   HTTP API  │     │  gRPC API   │     │   Webhook   │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                    │
       └───────────────────┴────────────────────┘
                           │
                    ┌──────▼──────┐
                    │   Ingestion │
                    │    Layer    │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │    Rules    │
                    │   Engine    │
                    └──────┬──────┘
                           │
       ┌───────────────────┴────────────────────┐
       │                                        │
┌──────▼──────┐  ┌──────────────┐  ┌──────────▼────┐
│  Forwarder  │  │  Transform   │  │   Metrics     │
│   (gRPC)    │  │   Engine     │  │  Collector    │
└─────────────┘  └──────────────┘  └───────────────┘
```

## 🚢 Deployment

### Kubernetes

```bash
# Deploy to development
kubectl apply -k k8s/overlays/dev

# Deploy to production with auto-scaling
kubectl apply -k k8s/overlays/prod

# Check deployment status
kubectl get pods,svc,ingress -n tas-mcp-prod
```

### Helm Chart (Coming Soon)

```bash
helm repo add tas-mcp https://tributary-ai-services.github.io/helm-charts
helm install tas-mcp tas-mcp/tas-mcp-server
```

## 📚 MCP Server Registry

The TAS MCP project includes a comprehensive **MCP Server Registry** - a curated catalog of Model Context Protocol servers across different categories and use cases.

### 🗂️ Registry Categories

- **🤖 AI Models** - LLM integrations and model serving
- **📡 Event Streaming** - Real-time event processing and forwarding  
- **🔄 Workflow Orchestration** - Complex workflow and agent coordination
- **💾 Knowledge Bases** - Vector stores and search capabilities
- **🔧 Data Processing** - ETL and data transformation services
- **📊 Monitoring** - Observability and metrics collection
- **💬 Communication** - Chat bots and messaging integrations

### 🚀 Using the Registry

```bash
# Browse the registry
cat registry/mcp-servers.json | jq '.servers[] | select(.category == "ai-models")'

# Find servers by capability
cat registry/mcp-servers.json | jq '.servers[] | select(.capabilities[] | contains("event-streaming"))'

# Get deployment information
cat registry/mcp-servers.json | jq '.servers[] | select(.name == "tas-mcp-server") | .deployment'
```

### 📋 Registry Features

- **JSON Schema Validation** - Ensures data consistency and structure
- **Deployment Ready** - Docker and Kubernetes deployment configurations
- **Access Models** - Clear documentation of API access patterns
- **Capability Mapping** - Searchable capability tags
- **Cost Information** - Pricing and resource requirements

See [registry/README.md](registry/README.md) for complete registry documentation and [registry/ENDPOINT_INTEGRATION.md](registry/ENDPOINT_INTEGRATION.md) for integration guides.

## 🔌 Integrations

### Argo Events

See [examples/triggers/](examples/triggers/) for complete integration examples in Go, Python, and Node.js.

### Kafka

```json
{
  "type": "kafka",
  "endpoint": "kafka-broker:9092",
  "config": {
    "topic": "mcp-events",
    "batch_size": 100
  }
}
```

### Prometheus Metrics

The server exposes Prometheus metrics at `/api/v1/metrics`:

- `mcp_events_total` - Total events processed
- `mcp_events_forwarded_total` - Events forwarded by target
- `mcp_forwarding_errors_total` - Forwarding errors by target
- `mcp_event_processing_duration_seconds` - Event processing latency

## 🧪 Testing

The project includes comprehensive test coverage across all packages:

```bash
# Run all unit tests
make test-unit

# Run integration tests
make test-integration

# Run benchmark tests
make test-benchmark

# Generate coverage report
make test-coverage

# Run all tests (unit + integration + benchmarks)
make test

# Lint code
make lint

# Format code
make fmt
```

### Test Coverage
- **Config Package**: 77.6% statement coverage
- **Forwarding Package**: 60.1% statement coverage
- **Integration Tests**: End-to-end event forwarding scenarios
- **Benchmark Tests**: Performance testing for critical paths

### Test Features
- Table-driven tests for comprehensive scenario coverage
- Mock HTTP servers for integration testing
- Event matching and validation utilities
- Concurrent testing patterns
- Test utilities package for reusable helpers

## 🗺️ Roadmap

See our comprehensive [ROADMAP.md](ROADMAP.md) for detailed development priorities, including:
- **1,535+ MCP server federation** across 12 categories from [mcpservers.org](https://mcpservers.org)
- **Universal MCP Orchestrator** vision and implementation plan
- **Quarterly release schedule** with progressive federation milestones
- **Community involvement** opportunities and feedback channels
- **Technical implementation** plans for massive-scale federation

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Development Setup

See [DEVELOPER.md](DEVELOPER.md) for detailed development instructions.

```bash
# Setup development environment
make init

# Run tests and linting
make test lint

# Submit changes
make fmt
git add .
git commit -m "feat: add new feature"
```

## 📚 Documentation

- [Developer Guide](DEVELOPER.md) - Development setup and guidelines
- [API Reference](docs/API.md) - Complete API documentation
- [Architecture](docs/DESIGN.md) - System design and architecture
- [Docker Guide](docs/DOCKER.md) - Container deployment guide
- [Examples](examples/) - Integration examples and tutorials

## 🔐 Security

- Non-root container execution
- TLS support for all protocols
- Authentication via API keys or OAuth2
- Rate limiting and DDoS protection
- Regular security scanning with Trivy

Report security vulnerabilities to: security@tributary-ai-services.com

## 📊 Performance

- Handles 10,000+ events/second per instance
- Sub-millisecond forwarding latency
- Horizontal scaling with Kubernetes HPA
- Efficient memory usage with bounded buffers
- Connection pooling for downstream services

## 🗺️ Roadmap

- [ ] Helm chart for easy deployment
- [ ] WebSocket support for real-time streaming
- [ ] Event replay and time-travel debugging
- [ ] GraphQL API for flexible queries
- [ ] Built-in event store with retention policies
- [ ] SDK libraries for popular languages
- [ ] Terraform modules for cloud deployment

## 📄 License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [Model Context Protocol](https://github.com/anthropics/model-context-protocol) by Anthropic
- [Argo Events](https://argoproj.github.io/argo-events/) for event-driven workflows
- The Go community for excellent libraries and tools

---

<p align="center">
  Built with ❤️ by <a href="https://tributary-ai-services.com">Tributary AI Services</a>
</p>