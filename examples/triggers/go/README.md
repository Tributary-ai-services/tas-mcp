# Go Trigger Handler Example

This example demonstrates how to implement trigger handling in Go using the Argo Events paradigm for the TAS MCP system.

## Features

- **High-performance HTTP server** using Gorilla Mux
- **gRPC integration** with TAS MCP service
- **Structured logging** with Zap
- **Event condition evaluation** with multiple operators
- **Action execution** with retries and timeouts
- **Multiple action types**: HTTP, gRPC, Kafka
- **Template processing** for dynamic payloads

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Argo Events   │───▶│  Go Handler      │───▶│   TAS MCP       │
│   (Sensors)     │    │  (HTTP Server)   │    │   (gRPC)        │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ▼
                       ┌──────────────────┐
                       │   External APIs  │
                       │  (Slack, Email)  │
                       └──────────────────┘
```

## Event Types Handled

### User Events
- `user.created` - Triggers welcome email and setup
- `user.updated` - Handles profile changes
- `user.deleted` - Cleanup and notifications

### Deployment Events  
- `deployment.succeeded` - Success notifications
- `deployment.failed` - Alert escalation
- `deployment.started` - Status updates

### Alert Events
- `alert.critical` - PagerDuty integration
- `alert.warning` - Slack notifications
- `alert.resolved` - Status updates

## Setup Instructions

### 1. Build the Application

```bash
# From the TAS MCP root directory
cd examples/triggers/go
go mod init tas-mcp-go-triggers
go mod tidy
go build -o trigger-handler main.go
```

### 2. Run Locally

```bash
# Set environment variables
export LOG_LEVEL=info
export MCP_SERVICE_ENDPOINT=localhost:50051

# Run the handler
./trigger-handler
```

### 3. Deploy to Kubernetes

```bash
# Apply the sensor configuration
kubectl apply -f sensor.yaml

# Verify deployment
kubectl get pods -n argo-events -l app=tas-mcp-go-triggers
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Logging level (debug, info, warn, error) | info |
| `MCP_SERVICE_ENDPOINT` | gRPC endpoint for TAS MCP service | tas-mcp-service:50051 |
| `HTTP_PORT` | HTTP server port | 8080 |

### Trigger Configuration

Triggers are defined in code but can be extended to load from configuration:

```go
type TriggerConfig struct {
    Name        string            `json:"name"`
    Conditions  []Condition       `json:"conditions"`
    Actions     []Action          `json:"actions"`
    Enabled     bool              `json:"enabled"`
    Metadata    map[string]string `json:"metadata"`
}
```

## Testing

### Test Webhook Endpoints

```bash
# Test GitHub webhook
curl -X POST http://localhost:8080/webhook/github \
  -H "Content-Type: application/json" \
  -d '{
    "eventId": "test-123",
    "eventType": "user.created",
    "source": "github",
    "data": {
      "userId": "user-456",
      "email": "test@example.com",
      "name": "Test User"
    }
  }'

# Test generic webhook
curl -X POST http://localhost:8080/webhook/generic \
  -H "Content-Type: application/json" \
  -d '{
    "eventId": "deploy-789",
    "eventType": "deployment.succeeded",
    "source": "ci-cd",
    "data": {
      "service": "api-gateway",
      "version": "v1.2.3",
      "environment": "production"
    }
  }'
```

### Health Check

```bash
curl http://localhost:8080/health
```

## Extending the Handler

### Adding New Event Types

1. Add a new handler function:
```go
func (h *TriggerHandler) handleCustomEventTrigger(payload EventPayload) {
    trigger := TriggerConfig{
        Name: "custom-event-handler",
        Conditions: []Condition{
            {Field: "eventType", Operator: "eq", Value: "custom.event"},
        },
        Actions: []Action{
            {
                Type:    "http",
                Target:  "https://api.example.com/webhook",
                Payload: map[string]interface{}{
                    "message": "Custom event received",
                },
            },
        },
    }
    h.executeTrigger(trigger, payload)
}
```

2. Register in the main handler:
```go
switch payload.EventType {
case "custom.event":
    h.handleCustomEventTrigger(payload)
}
```

### Adding New Action Types

1. Extend the `ActionType` constants
2. Implement the action in `executeAction()`
3. Add specific execution function

```go
func (h *TriggerHandler) executeCustomAction(ctx context.Context, action Action, payload EventPayload) {
    // Custom action implementation
}
```

## Monitoring

The handler provides built-in monitoring through:

- **Structured logging** with Zap
- **HTTP endpoints** for health checks
- **Metrics** (can be extended with Prometheus)
- **Error tracking** with context

## Best Practices

1. **Error Handling**: Always handle errors gracefully with proper logging
2. **Timeouts**: Set appropriate timeouts for external calls
3. **Retries**: Implement exponential backoff for failed actions
4. **Monitoring**: Add comprehensive logging and metrics
5. **Security**: Validate all incoming payloads
6. **Performance**: Use goroutines for concurrent action execution

## Integration with Argo Events

The Go handler integrates with Argo Events through:

1. **Event Sources** - Defined in `k8s/event-sources.yaml`
2. **Sensors** - Configured in `sensor.yaml`
3. **HTTP Endpoints** - Receive events from sensors
4. **gRPC Client** - Forward events to TAS MCP service

## Troubleshooting

### Common Issues

1. **Connection Refused**
   - Check if the service is running
   - Verify network connectivity
   - Check firewall settings

2. **gRPC Errors**
   - Ensure MCP service is available
   - Check gRPC endpoint configuration
   - Verify proto definitions match

3. **Action Failures**
   - Check external service availability
   - Verify authentication credentials
   - Review payload format

### Debug Commands

```bash
# Check pod logs
kubectl logs -n argo-events deployment/tas-mcp-go-triggers

# Check service endpoints
kubectl get endpoints -n argo-events tas-mcp-go-triggers

# Test connectivity
kubectl exec -n argo-events deployment/tas-mcp-go-triggers -- nc -zv tas-mcp-service 50051
```