# 📐 TAS MCP Server – Design Document

## 🧭 Overview

The TAS MCP Server is a Go-based gateway and event processor designed to implement the [Model Context Protocol (MCP)](https://github.com/Tributary-ai-services/tas-mcp). It enables intelligent routing, ingestion, and distribution of event-driven context updates within distributed machine learning and data processing pipelines.

It is designed to run standalone or as part of a mesh of other MCP-compatible services, enabling **multi-tenant RAG**, **workflow orchestration**, and **agent coordination**.

---

## 🎯 Goals

- Accept and validate `MCPEvent` messages over HTTP and gRPC
- Forward messages to other MCP servers for propagation
- Provide an event stream compatible with Argo Events and K8s native systems
- Be lightweight, secure, and pluggable

---

## 🧱 Architecture

```text
    ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
    │   HTTP Client   │    │   gRPC Client   │    │  Agent/Service  │
    │                 │    │                 │    │                 │
    └─────────┬───────┘    └─────────┬───────┘    └─────────┬───────┘
              │                      │                      │
              │ POST /mcp            │ EventStream()        │ HTTP/gRPC
              │                      │ (bidirectional)      │
              ▼                      ▼                      ▼
    ┌─────────────────────────────────────────────────────────────────┐
    │                    TAS MCP Server                               │
    │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
    │  │ HTTP Server │  │ gRPC Server │  │Health Check │            │
    │  │   :8080     │  │   :50051    │  │   :8082     │            │
    │  └─────┬───────┘  └─────┬───────┘  └─────────────┘            │
    │        │                │                                      │
    │        └────────┬───────┘                                      │
    │                 ▼                                               │
    │       ┌─────────────────┐     ┌─────────────────┐              │
    │       │ Event Validator │     │   Event Logger  │              │
    │       └─────────┬───────┘     └─────────────────┘              │
    │                 ▼                                               │
    │       ┌─────────────────┐                                      │
    │       │ Internal Channel│ (buffered)                           │
    │       │   (go channel)  │                                      │
    │       └─────────┬───────┘                                      │
    │                 │                                               │
    │    ┌────────────┼────────────┐                                 │
    │    ▼            ▼            ▼                                  │
    │ ┌────────┐ ┌─────────┐ ┌──────────┐                           │
    │ │gRPC    │ │Event    │ │ Metrics  │                           │
    │ │Streams │ │Forward  │ │Collector │                           │
    │ │        │ │        │ │          │                           │
    │ └────────┘ └─────┬───┘ └──────────┘                           │
    └─────────────────────┼─────────────────────────────────────────┘
                          │
                          ▼
    ┌─────────────────────────────────────────────────────────────────┐
    │                    External Systems                             │
    │                                                                 │
    │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
    │  │Downstream   │  │Argo Events  │  │Prometheus   │            │
    │  │MCP Servers  │  │K8s Events   │  │Metrics      │            │
    │  │             │  │             │  │             │            │
    │  └─────────────┘  └─────────────┘  └─────────────┘            │
    └─────────────────────────────────────────────────────────────────┘

Flow Direction:
→ HTTP Request/Response    ↔ Bidirectional gRPC Stream    ⤋ Event Distribution
```
## 🧩 Components

### 1. `cmd/`
- Entrypoint to the application
- Starts HTTP and gRPC servers
- Handles graceful shutdown and signals

### 2. `internal/http/`
- Defines `/mcp` HTTP endpoint
- Accepts POST requests with event payloads
- Deserializes and publishes to the internal event channel

### 3. `internal/grpc/`
- Implements the MCP gRPC service
- Receives events from external clients
- Sends events via gRPC stream for integration with tools like Argo Events

### 4. `internal/forwarder/`
- Forwards incoming events to peer MCP servers
- Supports fan-out and retry behavior

### 5. `internal/events/` (WIP)
- Event filtering, routing, and enrichments
- Will allow scoping and condition-based dispatch

### 6. `internal/config/`
- Loads configuration from environment variables

### 7. `internal/logger/`
- Provides structured logging via Zap

## 🧪 Protocol

### `proto/mcp.proto`
Defines the MCPEvent and gRPC interface for the MCP service.

```protobuf
message MCPEvent {
  string id = 1;
  string data = 2;
}

service Eventing {
  // Bi-directional stream for event forwarding
  rpc EventStream(stream MCPEvent) returns (stream MCPEvent);
}
```
## ⚙️ Configuration

Environment-driven config:

| Variable | Default | Description |
|----------|---------|-------------|
| `HTTP_PORT` | 8080 | HTTP server port |
| `GRPC_PORT` | 50051 | gRPC server port |
| `LOG_LEVEL` | info | Log level (debug, info, warn, error) |
| `FORWARD_TO` | (none) | Comma-separated MCP target URLs |
| `FORWARD_TIMEOUT` | 30s | Timeout for forwarding requests |
| `MAX_EVENT_SIZE` | 1MB | Maximum allowed event payload size |
| `BUFFER_SIZE` | 1000 | Internal event channel buffer size |
| `MAX_CONNECTIONS` | 100 | Maximum concurrent gRPC connections |
| `HEALTH_CHECK_PORT` | 8082 | Health check endpoint port |

## 📡 API Specification

### HTTP Endpoints

#### `POST /mcp`
Accepts MCPEvent messages via HTTP.

**Request:**
```json
{
  "id": "event-123",
  "data": "{\"type\": \"context_update\", \"payload\": {...}}"
}
```

**Response:**
- `200 OK`: Event accepted
- `400 Bad Request`: Invalid payload
- `413 Payload Too Large`: Event exceeds MAX_EVENT_SIZE
- `429 Too Many Requests`: Rate limit exceeded

**Example:**
```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"id":"ctx-001","data":"{\"agent\":\"assistant\",\"context\":\"user_query\"}"}'  
```

#### `GET /health`
Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z",
  "version": "1.0.0"
}
```

#### `GET /metrics`
Prometheus-compatible metrics endpoint.

### gRPC Service

#### `EventStream`
Bidirectional streaming for real-time event exchange.

**Client → Server:** Send events to be processed and forwarded
**Server → Client:** Receive events from other sources in real-time

## 🧵 Event Flow

### HTTP Flow:
1. Client sends MCPEvent POST request to `/mcp`
2. Server validates payload (size, format, required fields)
3. Event is published to internal buffered channel
4. All active listeners receive the event:
   - Active gRPC streams
   - Configured forwarders
5. Event processing is logged with metrics

### gRPC Bidirectional Stream Flow:
1. Client establishes bidirectional stream via `EventStream` RPC
2. Server adds client to active stream registry
3. **Inbound:** Client sends events → Server processes and forwards
4. **Outbound:** Server pushes events from other sources → Client
5. Connection cleanup on stream termination

### Error Handling Flow:
1. **Validation Errors:** Rejected immediately with error response
2. **Forwarding Failures:** Retried with exponential backoff
3. **Stream Errors:** Client reconnection with backoff
4. **Dead Letter Queue:** Failed events after max retries

## 🔐 Security

### Authentication (Planned)
- **JWT Bearer Tokens:** Validate tokens on all endpoints
- **API Keys:** Alternative auth method for service-to-service
- **mTLS:** Certificate-based authentication for gRPC

### Transport Security
- **HTTPS:** TLS termination at ingress/proxy level
- **gRPC TLS:** Native TLS support for gRPC connections

### Event Security (Planned)
- **Event Signatures:** HMAC verification for federated events
- **Content Validation:** Schema validation and sanitization
- **Rate Limiting:** Per-client request throttling

### Network Security
- **Network Policies:** Kubernetes-native network isolation
- **Service Mesh:** Istio/Linkerd integration for zero-trust

## 📊 Performance & Scalability

### Expected Performance
- **HTTP Throughput:** 10,000+ requests/second per instance
- **gRPC Connections:** 100+ concurrent bidirectional streams
- **Event Latency:** <10ms for local forwarding, <100ms for remote
- **Memory Usage:** ~50MB base + ~1KB per active connection

### Scaling Strategy
- **Horizontal:** Stateless design enables load balancer distribution
- **Load Balancing:** Round-robin for HTTP, consistent hashing for gRPC streams
- **Resource Limits:** Configurable connection and buffer limits
- **Backpressure:** Flow control to prevent memory exhaustion

## 🏥 Operational Considerations

### Health Checks
- **Startup Probe:** `/health` - server initialization complete
- **Liveness Probe:** `/health` - server responsive
- **Readiness Probe:** `/ready` - ready to accept traffic

### Monitoring & Metrics
```
# Key Metrics
tas_mcp_events_total{method="http|grpc", status="success|error"}
tas_mcp_active_connections{type="grpc_stream"}
tas_mcp_forward_duration_seconds{target="hostname"}
tas_mcp_event_size_bytes{percentile="50|95|99"}
tas_mcp_buffer_utilization_ratio
```

### Resource Requirements
```yaml
# Minimum
requests:
  cpu: 100m
  memory: 128Mi

# Recommended Production
requests:
  cpu: 500m
  memory: 512Mi
limits:
  cpu: 1000m
  memory: 1Gi
```

## 🧪 Event Schema

### MCPEvent Structure
```protobuf
message MCPEvent {
  string id = 1;        // Unique identifier (UUID recommended)
  string data = 2;      // JSON-encoded payload
  // Future fields:
  // int64 timestamp = 3;
  // string source = 4;
  // map<string, string> metadata = 5;
}
```

### Data Field Format
The `data` field contains JSON-encoded payloads:

```json
{
  "type": "context_update|agent_response|workflow_event",
  "source": "agent-id-or-service-name", 
  "timestamp": "2024-01-01T12:00:00Z",
  "payload": {
    // Event-specific data
  }
}
```

### Size Limits
- **Maximum Event Size:** 1MB (configurable)
- **ID Field:** Max 128 characters
- **Data Field:** Max 1MB - overhead

## 🚀 Deployment Examples

### Docker Compose
```yaml
version: '3.8'
services:
  tas-mcp:
    image: tas-mcp:latest
    ports:
      - "8080:8080"   # HTTP
      - "50051:50051" # gRPC
      - "8082:8082"   # Health
    environment:
      - LOG_LEVEL=info
      - FORWARD_TO=http://peer1:8080,http://peer2:8080
      - MAX_CONNECTIONS=200
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8082/health"]
      interval: 30s
      timeout: 10s
      retries: 3
```

### Kubernetes Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tas-mcp
spec:
  replicas: 3
  selector:
    matchLabels:
      app: tas-mcp
  template:
    metadata:
      labels:
        app: tas-mcp
    spec:
      containers:
      - name: tas-mcp
        image: tas-mcp:latest
        ports:
        - containerPort: 8080
        - containerPort: 50051
        - containerPort: 8082
        env:
        - name: LOG_LEVEL
          value: "info"
        - name: FORWARD_TO
          value: "http://tas-mcp-peer:8080"
        resources:
          requests:
            cpu: 500m
            memory: 512Mi
          limits:
            cpu: 1000m
            memory: 1Gi
        livenessProbe:
          httpGet:
            path: /health
            port: 8082
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8082
          initialDelaySeconds: 5
          periodSeconds: 5
```

## 🧪 Testing Strategy

### Unit Tests
- **Component Isolation:** Mock external dependencies
- **Event Processing:** Validate parsing, routing, forwarding
- **Error Handling:** Test failure scenarios and recovery
- **Configuration:** Verify env var parsing and defaults

### Integration Tests
- **End-to-End Flow:** HTTP → Internal → gRPC → Forward
- **Multi-Instance:** Test forwarding between multiple servers
- **Network Failures:** Resilience testing with network partitions
- **Load Testing:** Concurrent connections and high throughput

### Performance Tests
```bash
# HTTP Load Test
wrk -t12 -c400 -d30s --script=post-event.lua http://localhost:8080/mcp

# gRPC Streaming Test
ghz --insecure --proto proto/mcp.proto --call mcp.Eventing.EventStream \
  --data '{"id":"test","data":"{}"}' localhost:50051
```

## 🔄 Future Enhancements

### Phase 1 (Next Release)
- 💬 **WebSocket Support:** Real-time web client integration
- 📈 **OpenTelemetry:** Distributed tracing and metrics
- 🔐 **Authentication:** JWT and API key validation

### Phase 2 (Medium Term)
- 🧠 **Event Classification:** LLM-powered event categorization
- 🪪 **Identity-based Routing:** Route events based on sender identity
- 📊 **Event Analytics:** Built-in event pattern analysis

### Phase 3 (Long Term)
- 🌍 **Federation Visualization:** Real-time network topology
- 🔄 **Event Replay:** Historical event reconstruction
- 🤖 **Auto-scaling:** Dynamic replica adjustment based on load

## 📚 References

### Project Resources
- [TAS MCP Repository](https://github.com/Tributary-ai-services/tas-mcp)
- [Protocol Buffer Documentation](https://protobuf.dev/)
- [gRPC Go Documentation](https://grpc.io/docs/languages/go/)

### Integration Targets
- [Argo Events](https://argoproj.github.io/argo-events/) - Kubernetes event processing
- [Prometheus](https://prometheus.io/) - Metrics collection
- [OpenTelemetry](https://opentelemetry.io/) - Observability framework

### Development Tools
- [Zap Logger](https://github.com/uber-go/zap) - Structured logging
- [Cobra CLI](https://github.com/spf13/cobra) - Command-line interface
- [gRPC Gateway](https://github.com/grpc-ecosystem/grpc-gateway) - REST to gRPC proxy