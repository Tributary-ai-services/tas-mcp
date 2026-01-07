# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**TAS Model Context Protocol (MCP) Server** is a high-performance, cloud-native event gateway and MCP federation platform that implements the Model Context Protocol to support RAG pipelines, event-driven architectures, and workflow orchestration across distributed AI systems. It provides seamless integration with 4+ federated MCP servers including search, web scraping, database access, and development tools.

## Data Models & Schema Reference

### Service-Specific Data Models
This service's data models are comprehensively documented in the centralized data models repository:

**Location**: `../aether-shared/data-models/tas-mcp/`

#### Key Protocol Models:
- **Protocol Buffers** (`protocol-buffers.md`) - Complete .proto definitions for gRPC communication, message types, and service contracts
- **Event Structure** (`event-structure.md`) - Event payload formats, metadata, routing rules, and condition evaluation
- **Server Registry** (`server-registry.md`) - MCP server catalog with 1,535+ servers, federation management, and discovery

#### Cross-Service Integration:
- **Platform ERD** (`../aether-shared/data-models/cross-service/diagrams/platform-erd.md`) - Complete entity relationship diagram
- **Architecture Overview** (`../aether-shared/data-models/cross-service/diagrams/architecture-overview.md`) - MCP in system architecture
- **ID Mapping Chain** (`../aether-shared/data-models/cross-service/mappings/id-mapping-chain.md`) - Cross-service identifier relationships

#### When to Reference Data Models:
1. Before modifying protocol buffer definitions or adding new message types
2. When implementing new event forwarding rules or routing logic
3. When debugging MCP server federation or event routing issues
4. When onboarding new developers to understand the protocol architecture
5. Before adding new MCP servers to the federation or registry

**Main Documentation Hub**: `../aether-shared/data-models/README.md` - Complete navigation for all 38 data model files

## Technology Stack

- **Languages**: Go 1.23+ (backend), Node.js (MCP servers)
- **Protocols**: HTTP REST API, gRPC with bidirectional streaming
- **Framework**: Gin HTTP framework
- **Message Format**: Protocol Buffers (protobuf)
- **Monitoring**: Prometheus metrics + distributed tracing
- **Deployment**: Docker, Kubernetes with full automation

## Key Features

### MCP Federation
- **Unified Access**: Single gateway to 4+ federated MCP servers
- **Search**: DuckDuckGo integration with privacy-focused search
- **Web Scraping**: Apify platform with 5,000+ scraping actors
- **Database Access**: PostgreSQL MCP with security-first design
- **Development Tools**: Git repository automation and management
- **Server Registry**: Comprehensive catalog of 1,535+ MCP servers

### Multi-Protocol Support
- **HTTP REST API**: RESTful endpoints for event submission and queries
- **Bidirectional gRPC**: Streaming communication for real-time events
- **Protocol Buffers**: Efficient binary serialization for high performance
- **WebSocket Support**: Real-time bidirectional communication

### Smart Event Forwarding
- **Rule-Based Routing**: Conditional routing based on event metadata
- **Multi-Target**: Forward events to multiple destinations simultaneously
- **Transformation**: Event payload transformation and enrichment
- **Filtering**: Condition-based event filtering and processing

### Production Ready
- **Rate Limiting**: Per-client request throttling
- **Circuit Breakers**: Automatic failover and retry logic
- **Health Checks**: Comprehensive health monitoring endpoints
- **Observability**: Built-in metrics, logging, and tracing support
- **Graceful Shutdown**: Clean shutdown with connection draining

## Common Commands

```bash
# Initialize dependencies
make init

# Build the application
make build

# Run tests
make test

# Run with coverage
make test-coverage

# Build Docker image
make docker

# Run Docker container
make docker-run

# Start with Docker Compose (includes monitoring)
make docker-compose

# Deploy to Kubernetes
make k8s-deploy

# View Kubernetes logs
make k8s-logs
```

## API Endpoints

### Event Management (HTTP REST)
- `POST /events` - Submit new event for processing and forwarding
- `GET /events` - Query event history and status
- `GET /events/{id}` - Retrieve specific event details
- `GET /health` - Health check endpoint
- `GET /metrics` - Prometheus metrics endpoint

### Event Streaming (gRPC)
- `StreamEvents` - Bidirectional event streaming
- `SubmitEvent` - Single event submission
- `QueryEvents` - Event query with filtering

### MCP Server Management
- `GET /mcp/servers` - List available MCP servers in federation
- `GET /mcp/registry` - Browse MCP server registry (1,535+ servers)
- `POST /mcp/servers/{server}/invoke` - Invoke MCP server capability

## Federated MCP Servers

The TAS MCP Server federates access to these integrated servers:

1. **Search MCP Server** (`@modelcontextprotocol/server-brave-search`)
   - Privacy-focused web search with DuckDuckGo
   - No tracking or user profiling

2. **Web Scraping MCP Server** (Custom Apify integration)
   - Access to 5,000+ Apify scraping actors
   - Dynamic content extraction and crawling

3. **Database MCP Server** (`@modelcontextprotocol/server-postgres`)
   - Secure PostgreSQL access
   - Query execution with safety controls

4. **Git MCP Server** (`@modelcontextprotocol/server-git`)
   - Repository automation and management
   - Commit, branch, and PR operations

## Integration Points

- **Aether Backend**: Event-driven notifications and workflow triggers
- **TAS Agent Builder**: MCP capabilities for agent tools and actions
- **TAS Workflow Builder**: Workflow step execution via MCP servers
- **Argo Events**: Native Argo Events integration for Kubernetes workflows
- **Kafka**: Event streaming and message queue integration
- **Webhooks**: HTTP webhook forwarding for external systems

## Configuration

Configuration is managed via YAML files and environment variables:

```yaml
server:
  http_port: 8082
  grpc_port: 50052
  metrics_port: 8083

mcp:
  federation:
    enabled: true
    servers:
      - name: search
        type: brave-search
        enabled: true
      - name: scraper
        type: apify
        enabled: true
      - name: database
        type: postgres
        enabled: true
      - name: git
        type: git
        enabled: true

forwarding:
  rules:
    - name: argo-events
      target: http://argo-events-webhook:8080
      conditions:
        - field: event_type
          operator: equals
          value: workflow
```

## Important Notes

- MCP federation provides unified access to distributed capabilities
- Event forwarding supports multiple simultaneous targets with transformation
- Protocol Buffers ensure efficient binary serialization for high throughput
- Rate limiting and circuit breakers prevent cascading failures
- Server registry includes 1,535+ community MCP servers for extensibility
- Integration with shared TAS infrastructure via `tas-shared-network` Docker network
- Supports both synchronous (request/response) and asynchronous (event streaming) patterns
