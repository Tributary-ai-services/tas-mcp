# 📡 TAS MCP Server API Reference

This document provides a comprehensive reference for all APIs exposed by the TAS MCP Server.

## Table of Contents

- [HTTP REST API](#http-rest-api)
- [gRPC API](#grpc-api)
- [WebSocket API](#websocket-api) (Coming Soon)
- [Event Formats](#event-formats)
- [Error Handling](#error-handling)
- [Authentication](#authentication)
- [Rate Limiting](#rate-limiting)

## HTTP REST API

### Base URL

```
http://localhost:8080/api/v1
```

### Endpoints

#### Event Ingestion

##### POST `/events`

Ingest a single event into the MCP server.

**Request:**
```http
POST /api/v1/events
Content-Type: application/json

{
  "event_id": "evt-123",
  "event_type": "user.created",
  "source": "auth-service",
  "timestamp": 1703001234,
  "data": "{\"user_id\": \"usr-456\", \"email\": \"user@example.com\"}",
  "metadata": {
    "correlation_id": "req-789",
    "version": "1.0"
  }
}
```

**Response:**
```json
{
  "event_id": "evt-123",
  "status": "accepted"
}
```

**Status Codes:**
- `200 OK` - Event accepted successfully
- `400 Bad Request` - Invalid request format
- `413 Payload Too Large` - Event exceeds size limit
- `429 Too Many Requests` - Rate limit exceeded
- `500 Internal Server Error` - Server error

##### POST `/events/batch`

Ingest multiple events in a single request.

**Request:**
```http
POST /api/v1/events/batch
Content-Type: application/json

[
  {
    "event_id": "evt-1",
    "event_type": "order.created",
    "source": "order-service",
    "data": "{\"order_id\": \"ord-123\"}"
  },
  {
    "event_id": "evt-2",
    "event_type": "payment.processed",
    "source": "payment-service",
    "data": "{\"payment_id\": \"pay-456\"}"
  }
]
```

**Response:**
```json
{
  "processed": 2,
  "results": [
    {
      "event_id": "evt-1",
      "status": "accepted"
    },
    {
      "event_id": "evt-2",
      "status": "accepted"
    }
  ]
}
```

**Constraints:**
- Maximum 1000 events per batch
- Total payload size limit: 10MB

#### Forwarding Management

##### GET `/forwarding/targets`

List all configured forwarding targets.

**Request:**
```http
GET /api/v1/forwarding/targets
```

**Response:**
```json
{
  "target-1": {
    "id": "target-1",
    "name": "Argo Events Webhook",
    "type": "webhook",
    "endpoint": "http://argo-events:12000",
    "status": "active",
    "rules": [
      {
        "id": "rule-1",
        "name": "Critical Events",
        "enabled": true,
        "conditions": [
          {
            "field": "event_type",
            "operator": "contains",
            "value": "critical"
          }
        ]
      }
    ]
  }
}
```

##### POST `/forwarding/targets`

Add a new forwarding target.

**Request:**
```json
{
  "id": "kafka-prod",
  "name": "Production Kafka",
  "type": "kafka",
  "endpoint": "kafka-broker:9092",
  "config": {
    "topic": "mcp-events",
    "batch_size": 100,
    "timeout": "30s"
  },
  "rules": [
    {
      "conditions": [
        {
          "field": "source",
          "operator": "ne",
          "value": "test"
        }
      ]
    }
  ]
}
```

##### PUT `/forwarding/targets/{id}`

Update an existing forwarding target.

##### DELETE `/forwarding/targets/{id}`

Remove a forwarding target.

##### GET `/forwarding/metrics`

Get forwarding metrics.

**Response:**
```json
{
  "total_events": 10000,
  "forwarded_events": 9500,
  "failed_events": 50,
  "dropped_events": 10,
  "retry_events": 100,
  "targets": {
    "target-1": {
      "forwarded": 5000,
      "failed": 20,
      "average_latency_ms": 45
    }
  }
}
```

#### Server Management

##### GET `/metrics`

Get server metrics in Prometheus format.

```
# HELP mcp_events_total Total number of events processed
# TYPE mcp_events_total counter
mcp_events_total{source="auth-service",type="user.created"} 1234

# HELP mcp_forwarding_latency_seconds Event forwarding latency
# TYPE mcp_forwarding_latency_seconds histogram
mcp_forwarding_latency_seconds_bucket{target="kafka",le="0.005"} 100
```

##### GET `/stats`

Get server statistics.

**Response:**
```json
{
  "total_events": 15000,
  "stream_events": 5000,
  "forwarded_events": 14500,
  "error_events": 50,
  "active_streams": 3,
  "start_time": "2024-01-01T00:00:00Z"
}
```

#### Health Checks

##### GET `/health`

Detailed health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z",
  "version": "1.0.0",
  "uptime": "24h15m30s",
  "stats": {
    "total_events": 15000,
    "active_streams": 3
  },
  "forwarding": {
    "total_events": 15000,
    "forwarded_events": 14500,
    "targets": 5
  }
}
```

##### GET `/ready`

Kubernetes readiness probe endpoint.

**Response:**
```json
{
  "status": "ready"
}
```

## gRPC API

### Service Definition

```protobuf
service MCPService {
  // Ingest a single event
  rpc IngestEvent(IngestEventRequest) returns (IngestEventResponse);
  
  // Bidirectional streaming for events
  rpc EventStream(stream Event) returns (stream Event);
  
  // Health check
  rpc GetHealth(HealthCheckRequest) returns (HealthCheckResponse);
  
  // Get metrics
  rpc GetMetrics(MetricsRequest) returns (MetricsResponse);
}
```

### Connection

```go
import (
    "google.golang.org/grpc"
    mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
)

// Create connection
conn, err := grpc.Dial("localhost:50051", 
    grpc.WithInsecure(),
    grpc.WithBlock(),
)
defer conn.Close()

// Create client
client := mcpv1.NewMCPServiceClient(conn)
```

### Methods

#### IngestEvent

```go
req := &mcpv1.IngestEventRequest{
    EventId:   "evt-123",
    EventType: "user.action",
    Source:    "webapp",
    Timestamp: time.Now().Unix(),
    Data:      `{"action": "login", "user_id": "123"}`,
    Metadata: map[string]string{
        "ip": "192.168.1.1",
        "user_agent": "Mozilla/5.0",
    },
}

resp, err := client.IngestEvent(context.Background(), req)
if err != nil {
    log.Fatalf("Failed to ingest event: %v", err)
}

fmt.Printf("Event %s status: %s\n", resp.EventId, resp.Status)
```

#### EventStream

```go
// Create bidirectional stream
stream, err := client.EventStream(context.Background())
if err != nil {
    log.Fatalf("Failed to create stream: %v", err)
}

// Send events
go func() {
    for i := 0; i < 10; i++ {
        event := &mcpv1.Event{
            EventId:   fmt.Sprintf("evt-%d", i),
            EventType: "test.event",
            Source:    "test-client",
            Data:      fmt.Sprintf(`{"index": %d}`, i),
        }
        if err := stream.Send(event); err != nil {
            log.Printf("Failed to send event: %v", err)
            return
        }
    }
    stream.CloseSend()
}()

// Receive events
for {
    event, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatalf("Failed to receive event: %v", err)
    }
    
    fmt.Printf("Received event: %s\n", event.EventId)
}
```

## Event Formats

### Event Structure

```json
{
  "event_id": "string (required, unique identifier)",
  "event_type": "string (required, dot-separated type)",
  "source": "string (required, event source identifier)",
  "timestamp": "integer (optional, Unix timestamp)",
  "data": "string (required, JSON-encoded payload)",
  "metadata": {
    "key": "value (optional, string key-value pairs)"
  }
}
```

### Event Type Conventions

- Use dot notation: `resource.action`
- Examples:
  - `user.created`
  - `order.shipped`
  - `payment.failed`
  - `system.alert.critical`

### Data Payload

The `data` field must contain a valid JSON string:

```json
{
  "data": "{\"user_id\": \"123\", \"name\": \"John Doe\", \"email\": \"john@example.com\"}"
}
```

## Error Handling

### HTTP Error Responses

```json
{
  "error": {
    "code": "INVALID_EVENT",
    "message": "Event validation failed",
    "details": {
      "field": "event_type",
      "reason": "Event type cannot be empty"
    }
  },
  "request_id": "req-123"
}
```

### gRPC Error Codes

| Code | Description | HTTP Equivalent |
|------|-------------|-----------------|
| `INVALID_ARGUMENT` | Invalid request parameters | 400 |
| `NOT_FOUND` | Resource not found | 404 |
| `ALREADY_EXISTS` | Resource already exists | 409 |
| `RESOURCE_EXHAUSTED` | Rate limit exceeded | 429 |
| `INTERNAL` | Internal server error | 500 |
| `UNAVAILABLE` | Service unavailable | 503 |

### Error Handling Best Practices

1. **Always check error responses**
2. **Implement exponential backoff for retries**
3. **Log error details for debugging**
4. **Handle rate limiting gracefully**

## Authentication

### API Key Authentication

```http
GET /api/v1/events
Authorization: Bearer your-api-key-here
```

### mTLS Authentication (gRPC)

```go
// Load client certificates
cert, err := tls.LoadX509KeyPair("client.crt", "client.key")

// Create TLS config
config := &tls.Config{
    Certificates: []tls.Certificate{cert},
}

// Create connection with TLS
conn, err := grpc.Dial("localhost:50051",
    grpc.WithTransportCredentials(credentials.NewTLS(config)),
)
```

## Rate Limiting

### Default Limits

- **Per IP**: 1000 requests/minute
- **Per API Key**: 10000 requests/minute
- **Batch Operations**: 100 requests/minute

### Rate Limit Headers

```http
HTTP/1.1 200 OK
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 999
X-RateLimit-Reset: 1703001234
```

### Handling Rate Limits

```go
// Example retry logic with exponential backoff
func retryWithBackoff(fn func() error) error {
    backoff := 100 * time.Millisecond
    maxBackoff := 30 * time.Second
    
    for i := 0; i < 5; i++ {
        err := fn()
        if err == nil {
            return nil
        }
        
        // Check if rate limited
        if isRateLimited(err) {
            time.Sleep(backoff)
            backoff *= 2
            if backoff > maxBackoff {
                backoff = maxBackoff
            }
            continue
        }
        
        return err
    }
    
    return fmt.Errorf("max retries exceeded")
}
```

## Client Libraries

### Go Client

```go
import "github.com/tributary-ai-services/tas-mcp-go-client"

client := mcp.NewClient("http://localhost:8080", 
    mcp.WithAPIKey("your-key"),
    mcp.WithTimeout(30*time.Second),
)

err := client.IngestEvent(ctx, &mcp.Event{
    ID:   "evt-123",
    Type: "user.action",
    Data: map[string]interface{}{
        "action": "login",
    },
})
```

### Python Client (Coming Soon)

```python
from tas_mcp import MCPClient

client = MCPClient("http://localhost:8080", api_key="your-key")

client.ingest_event({
    "event_id": "evt-123",
    "event_type": "user.action",
    "data": {"action": "login"}
})
```

### Node.js Client (Coming Soon)

```javascript
const { MCPClient } = require('@tas/mcp-client');

const client = new MCPClient('http://localhost:8080', {
    apiKey: 'your-key'
});

await client.ingestEvent({
    eventId: 'evt-123',
    eventType: 'user.action',
    data: { action: 'login' }
});
```