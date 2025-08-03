# Python Trigger Handler Example

This example demonstrates how to implement trigger handling in Python using the Argo Events paradigm for the TAS MCP system with FastAPI and asyncio.

## Features

- **High-performance async API** using FastAPI
- **Event-driven architecture** with asyncio
- **Multiple integrations**: Redis, Kafka, HTTP, gRPC
- **Advanced condition evaluation** with comprehensive operators
- **Rate limiting and cooldown** mechanisms
- **Template processing** with variable substitution
- **Comprehensive error handling** and retries
- **Real-time statistics** and monitoring

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Argo Events   │───▶│ Python Handler   │───▶│   TAS MCP       │
│   (Sensors)     │    │  (FastAPI)       │    │   (gRPC)        │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ├─────────────────┐
                                ▼                 ▼
                       ┌──────────────────┐ ┌─────────────────┐
                       │     Redis        │ │     Kafka       │
                       │  (Pub/Sub)       │ │  (Streaming)    │
                       └──────────────────┘ └─────────────────┘
```

## Default Triggers

### User Welcome Trigger
```python
{
    "name": "user-welcome",
    "conditions": [
        {"field": "event_type", "operator": "eq", "value": "user.created"},
        {"field": "data.email", "operator": "contains", "value": "@"}
    ],
    "actions": [
        {
            "type": "http",
            "target": "https://api.example.com/welcome",
            "payload": {"template": "welcome"}
        }
    ]
}
```

### Deployment Notification
```python
{
    "name": "deployment-notify",
    "conditions": [
        {"field": "event_type", "operator": "eq", "value": "deployment.completed"},
        {"field": "data.environment", "operator": "in", "value": ["staging", "production"]}
    ],
    "actions": [
        {
            "type": "http", 
            "target": "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK",
            "payload": {"text": "🚀 Deployment completed!"}
        }
    ]
}
```

### Critical Alert Escalation
```python
{
    "name": "critical-alert",
    "conditions": [
        {"field": "event_type", "operator": "eq", "value": "alert.critical"},
        {"field": "data.severity", "operator": "gte", "value": 8}
    ],
    "actions": [
        {
            "type": "http",
            "target": "https://api.pagerduty.com/incidents",
            "payload": {"urgency": "high"}
        }
    ],
    "rate_limit": 10,
    "cooldown": 300.0
}
```

## Setup Instructions

### 1. Install Dependencies

```bash
cd examples/triggers/python

# Create virtual environment
python -m venv venv
source venv/bin/activate  # On Windows: venv\Scripts\activate

# Install dependencies
pip install -r requirements.txt
```

### 2. Requirements File (`requirements.txt`)

```txt
fastapi==0.104.1
uvicorn[standard]==0.24.0
aiohttp==3.9.0
redis[hiredis]==5.0.1
kafka-python==2.0.2
grpcio==1.59.0
grpcio-tools==1.59.0
pydantic==2.5.0
python-multipart==0.0.6
```

### 3. Run Locally

```bash
# Set environment variables
export LOG_LEVEL=INFO
export REDIS_URL=redis://localhost:6379
export KAFKA_BROKERS=localhost:9092
export MCP_GRPC_ENDPOINT=localhost:50051

# Run with uvicorn
python trigger_handler.py

# Or use uvicorn directly
uvicorn trigger_handler:app --host 0.0.0.0 --port 8080 --reload
```

### 4. Deploy to Kubernetes

```bash
# Build Docker image
docker build -t tas-mcp/python-triggers:latest .

# Apply Kubernetes manifests
kubectl apply -f sensor.yaml

# Check deployment
kubectl get pods -n argo-events -l app=tas-mcp-python-triggers
```

## API Endpoints

### Core Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/webhook/github` | GitHub webhook events |
| `POST` | `/webhook/generic` | Generic webhook events |
| `POST` | `/webhook/kafka` | Kafka-sourced events |
| `GET` | `/health` | Health check |
| `GET` | `/triggers` | List all triggers |
| `POST` | `/triggers/{name}` | Add/update trigger |
| `DELETE` | `/triggers/{name}` | Remove trigger |
| `GET` | `/stats` | Get statistics |

### Example API Calls

```bash
# Health check
curl http://localhost:8080/health

# List triggers
curl http://localhost:8080/triggers

# Add new trigger
curl -X POST http://localhost:8080/triggers/my-trigger \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-trigger",
    "conditions": [
      {"field": "event_type", "operator": "eq", "value": "test.event"}
    ],
    "actions": [
      {
        "type": "http",
        "target": "https://httpbin.org/post",
        "payload": {"message": "Test trigger fired"}
      }
    ]
  }'

# Send test event
curl -X POST http://localhost:8080/webhook/generic \
  -H "Content-Type: application/json" \
  -d '{
    "event_id": "test-123",
    "event_type": "test.event",
    "source": "manual",
    "data": {"key": "value"}
  }'
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Logging level | INFO |
| `REDIS_URL` | Redis connection URL | redis://redis-service.redis:6379 |
| `KAFKA_BROKERS` | Kafka broker addresses | kafka-broker.kafka:9092 |
| `MCP_GRPC_ENDPOINT` | TAS MCP gRPC endpoint | tas-mcp-service:50051 |

### Condition Operators

- `eq` - Equal to
- `ne` - Not equal to  
- `gt` - Greater than
- `lt` - Less than
- `gte` - Greater than or equal
- `lte` - Less than or equal
- `contains` - String contains
- `in` - Value in array
- `not_in` - Value not in array
- `regex` - Regular expression match

### Action Types

- `http` - HTTP POST request
- `kafka` - Kafka message
- `redis` - Redis pub/sub
- `grpc` - gRPC call
- `email` - Email notification (planned)

## Advanced Features

### Rate Limiting

```python
{
    "name": "rate-limited-trigger",
    "rate_limit": 10,  # Max 10 executions per minute
    "cooldown": 60.0   # 60 second cooldown between executions
}
```

### Template Processing

Actions support template variables:

```python
{
    "type": "http",
    "target": "https://api.example.com/notify",
    "payload": {
        "user_id": "{{data.user_id}}",
        "message": "Welcome {{data.name}}!",
        "timestamp": "{{timestamp}}"
    }
}
```

### Conditional Actions

Complex conditions with multiple operators:

```python
{
    "conditions": [
        {"field": "event_type", "operator": "eq", "value": "order.created"},
        {"field": "data.amount", "operator": "gt", "value": 1000},
        {"field": "data.customer_tier", "operator": "in", "value": ["gold", "platinum"]},
        {"field": "data.country", "operator": "not_in", "value": ["restricted_country"]}
    ]
}
```

## Monitoring and Debugging

### Statistics Endpoint

```bash
curl http://localhost:8080/stats
```

Returns:
```json
{
    "total_triggers": 5,
    "active_triggers": 4,
    "trigger_stats": {
        "user-welcome": {
            "executions": 150,
            "successes": 148,
            "failures": 2
        }
    },
    "uptime": 3600,
    "memory_usage": {...}
}
```

### Logging

Structured JSON logging with correlation IDs:

```json
{
    "timestamp": "2024-01-15T10:30:00Z",
    "level": "INFO",
    "message": "Trigger executed successfully",
    "trigger_name": "user-welcome",
    "event_id": "evt_123",
    "duration_ms": 245
}
```

## Testing

### Unit Tests

```python
import pytest
from trigger_handler import TriggerHandler, EventPayload

@pytest.mark.asyncio
async def test_user_welcome_trigger():
    handler = TriggerHandler()
    await handler.initialize()
    
    payload = EventPayload(
        event_id="test-123",
        event_type="user.created",
        source="test",
        data={"email": "test@example.com", "name": "Test User"}
    )
    
    # Test condition evaluation
    trigger = handler.triggers["user-welcome"]
    assert handler._evaluate_conditions(trigger.conditions, payload)
```

### Integration Tests

```bash
# Start test dependencies
docker-compose -f docker-compose.test.yml up -d

# Run tests
pytest tests/ -v

# Clean up
docker-compose -f docker-compose.test.yml down
```

## Docker Configuration

### Dockerfile

```dockerfile
FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY trigger_handler.py .

EXPOSE 8080

CMD ["python", "trigger_handler.py"]
```

### Docker Compose for Development

```yaml
version: '3.8'
services:
  python-triggers:
    build: .
    ports:
      - "8080:8080"
    environment:
      - REDIS_URL=redis://redis:6379
      - KAFKA_BROKERS=kafka:9092
    depends_on:
      - redis
      - kafka
  
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
  
  kafka:
    image: confluentinc/cp-kafka:latest
    environment:
      KAFKA_ZOOKEEPER_CONNECT: zookeeper:2181
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
    depends_on:
      - zookeeper
```

## Performance Optimization

### Async Best Practices

1. **Use connection pooling** for HTTP clients
2. **Batch operations** where possible
3. **Implement circuit breakers** for external services
4. **Use background tasks** for heavy processing
5. **Monitor memory usage** and implement cleanup

### Scaling Considerations

- **Horizontal scaling**: Multiple replicas with load balancing
- **Vertical scaling**: Increase CPU/memory for compute-heavy triggers
- **Database sharding**: For high-volume statistics storage
- **Caching**: Redis for frequently accessed trigger configurations

## Troubleshooting

### Common Issues

1. **Redis Connection Errors**
   ```bash
   # Check Redis connectivity
   redis-cli -h redis-service.redis -p 6379 ping
   ```

2. **Kafka Consumer Lag**
   ```bash
   # Check consumer group status
   kafka-consumer-groups.sh --bootstrap-server kafka:9092 --group tas-mcp-triggers --describe
   ```

3. **High Memory Usage**
   ```bash
   # Monitor with kubectl
   kubectl top pods -n argo-events -l app=tas-mcp-python-triggers
   ```

### Debug Mode

Enable debug logging:
```bash
export LOG_LEVEL=DEBUG
python trigger_handler.py
```

This provides detailed logs for:
- Event processing flow
- Condition evaluation steps  
- Action execution details
- Error stack traces