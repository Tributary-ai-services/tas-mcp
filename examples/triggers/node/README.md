# Node.js Trigger Handler Example

This example demonstrates how to implement trigger handling in Node.js using the Argo Events paradigm for the TAS MCP system with Express and event-driven architecture.

## Features

- **Express.js HTTP server** with comprehensive middleware
- **Event-driven architecture** using EventEmitter
- **Multiple integrations**: Redis, Kafka, HTTP, gRPC
- **Advanced template processing** with variable substitution
- **Built-in rate limiting** and security middleware
- **Real-time statistics** and monitoring
- **Extensible function system** for custom actions
- **Graceful shutdown** handling

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Argo Events   │───▶│ Node.js Handler  │───▶│   TAS MCP       │
│   (Sensors)     │    │  (Express)       │    │   (gRPC)        │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ├─────────────────┐
                                ▼                 ▼
                       ┌──────────────────┐ ┌─────────────────┐
                       │     Redis        │ │     Kafka       │
                       │  (IORedis)       │ │  (KafkaJS)      │
                       └──────────────────┘ └─────────────────┘
```

## Default Triggers

### User Registration
Triggers welcome email and Kafka notification when users register:

```javascript
{
  name: 'user-registration',
  conditions: [
    { field: 'event_type', operator: 'eq', value: 'user.registered' },
    { field: 'data.email', operator: 'contains', value: '@' }
  ],
  actions: [
    {
      type: 'http',
      target: 'https://api.sendgrid.com/v3/mail/send',
      payload: {
        personalizations: [{ to: [{ email: '{{data.email}}' }] }],
        from: { email: 'noreply@tas-mcp.com' },
        content: [{ type: 'text/html', value: '<h1>Welcome!</h1>' }]
      }
    }
  ]
}
```

### CI/CD Pipeline
Triggers GitHub Actions workflows for code deployments:

```javascript
{
  name: 'cicd-pipeline',
  conditions: [
    { field: 'event_type', operator: 'eq', value: 'git.push' },
    { field: 'data.branch', operator: 'in', value: ['main', 'master', 'develop'] }
  ],
  actions: [
    {
      type: 'http',
      target: 'https://api.github.com/repos/{{data.repository}}/actions/workflows/deploy.yml/dispatches',
      payload: {
        ref: '{{data.branch}}',
        inputs: { environment: '{{data.branch === "main" ? "production" : "staging"}}' }
      }
    }
  ]
}
```

### Monitoring Alerts
Escalates critical alerts to Slack and stores in Redis:

```javascript
{
  name: 'monitoring-alert',
  conditions: [
    { field: 'event_type', operator: 'eq', value: 'alert.triggered' },
    { field: 'data.severity', operator: 'in', value: ['critical', 'high'] }
  ],
  actions: [
    {
      type: 'http',
      target: 'https://hooks.slack.com/services/{{SLACK_WEBHOOK_PATH}}',
      payload: {
        text: '🚨 Alert: {{data.title}}',
        attachments: [{
          color: '{{data.severity === "critical" ? "danger" : "warning"}}',
          fields: [
            { title: 'Severity', value: '{{data.severity}}', short: true }
          ]
        }]
      }
    }
  ]
}
```

## Setup Instructions

### 1. Install Dependencies

```bash
cd examples/triggers/node

# Initialize if needed
npm init -y

# Install dependencies
npm install express @grpc/grpc-js @grpc/proto-loader axios ioredis kafkajs winston helmet cors express-rate-limit
```

### 2. Package.json Dependencies

```json
{
  "dependencies": {
    "express": "^4.18.2",
    "@grpc/grpc-js": "^1.9.0",
    "@grpc/proto-loader": "^0.7.0",
    "axios": "^1.6.0",
    "ioredis": "^5.3.0",
    "kafkajs": "^2.2.0",
    "winston": "^3.11.0",
    "helmet": "^7.1.0",
    "cors": "^2.8.5",
    "express-rate-limit": "^7.1.0"
  },
  "devDependencies": {
    "nodemon": "^3.0.0",
    "jest": "^29.7.0",
    "supertest": "^6.3.0"
  }
}
```

### 3. Run Locally

```bash
# Set environment variables
export NODE_ENV=development
export LOG_LEVEL=info
export REDIS_HOST=localhost
export REDIS_PORT=6379
export KAFKA_BROKERS=localhost:9092

# Run with Node.js
node trigger-handler.js

# Or use nodemon for development
npx nodemon trigger-handler.js
```

### 4. Deploy to Kubernetes

```bash
# Build Docker image
docker build -t tas-mcp/node-triggers:latest .

# Apply Kubernetes manifests
kubectl apply -f sensor.yaml

# Check deployment status
kubectl get pods -n argo-events -l app=tas-mcp-node-triggers
```

## API Endpoints

### Core Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/webhook/github` | GitHub webhook events |
| `POST` | `/webhook/generic` | Generic webhook events |
| `POST` | `/webhook/kafka` | Kafka-sourced events |
| `POST` | `/webhook/monitoring` | Monitoring alerts |
| `GET` | `/health` | Health check with uptime |
| `GET` | `/triggers` | List all triggers with stats |
| `POST` | `/triggers/:name` | Add/update trigger |
| `DELETE` | `/triggers/:name` | Remove trigger |
| `GET` | `/stats` | Detailed statistics |

### Example API Usage

```bash
# Health check
curl http://localhost:8080/health

# List triggers with statistics
curl http://localhost:8080/triggers

# Add a custom trigger
curl -X POST http://localhost:8080/triggers/custom-alert \
  -H "Content-Type: application/json" \
  -d '{
    "name": "custom-alert",
    "conditions": [
      {"field": "event_type", "operator": "eq", "value": "custom.alert"},
      {"field": "data.priority", "operator": "gte", "value": 5}
    ],
    "actions": [
      {
        "type": "http",
        "target": "https://httpbin.org/post",
        "payload": {
          "alert": "{{data.message}}",
          "priority": "{{data.priority}}"
        }
      }
    ],
    "enabled": true
  }'

# Test the trigger
curl -X POST http://localhost:8080/webhook/generic \
  -H "Content-Type: application/json" \
  -d '{
    "event_id": "test-alert-1",
    "event_type": "custom.alert",
    "source": "test-system",
    "data": {
      "message": "Test alert message",
      "priority": 7,
      "system": "monitoring"
    }
  }'
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NODE_ENV` | Environment (development/production) | development |
| `LOG_LEVEL` | Logging level | info |
| `REDIS_HOST` | Redis hostname | redis-service.redis |
| `REDIS_PORT` | Redis port | 6379 |
| `KAFKA_BROKERS` | Kafka broker list | kafka-broker.kafka:9092 |
| `MCP_GRPC_ENDPOINT` | TAS MCP gRPC endpoint | tas-mcp-service:50051 |

### Trigger Configuration Structure

```javascript
{
  name: 'trigger-name',
  conditions: [
    {
      field: 'event_type',        // Field path (supports dot notation)
      operator: 'eq',             // Condition operator
      value: 'expected.value'     // Expected value
    }
  ],
  actions: [
    {
      type: 'http',               // Action type
      target: 'https://api.com',  // Target endpoint/topic
      payload: {...},             // Action payload (supports templates)
      headers: {...},             // HTTP headers (if applicable)
      timeout: 30000,             // Timeout in milliseconds
      retries: 3                  // Retry attempts
    }
  ],
  enabled: true,                  // Enable/disable trigger
  rateLimit: 100,                 // Max executions per minute
  cooldown: 5000                  // Cooldown between executions (ms)
}
```

## Advanced Features

### Template Processing

Supports dynamic template variables in action payloads:

```javascript
{
  type: 'http',
  target: 'https://api.slack.com/webhook',
  payload: {
    text: 'User {{data.name}} registered with email {{data.email}}',
    channel: '#{{data.department || "general"}}',
    timestamp: '{{timestamp}}',
    metadata: {
      event_id: '{{event_id}}',
      processed_at: '{{new Date().toISOString()}}'
    }
  }
}
```

### Condition Operators

- `eq` - Equal to
- `ne` - Not equal to
- `gt` - Greater than
- `lt` - Less than  
- `gte` - Greater than or equal
- `lte` - Less than or equal
- `contains` - String contains substring
- `in` - Value exists in array
- `not_in` - Value does not exist in array
- `regex` - Regular expression match

### Action Types

- `http` - HTTP POST request with payload
- `kafka` - Publish message to Kafka topic
- `redis` - Publish to Redis channel
- `grpc` - gRPC service call
- `function` - Execute built-in function
- `webhook` - Generic webhook call

### Built-in Functions

Custom function actions for common operations:

```javascript
{
  type: 'function',
  target: 'notifyTeam',
  payload: {
    message: 'Deployment completed for {{data.service}}',
    channel: '#deployments'
  }
}
```

Available functions:
- `notifyTeam` - Team notifications
- `quarantineResource` - Security quarantine
- Custom functions can be added easily

### Rate Limiting

Per-trigger rate limiting with Redis backend:

```javascript
{
  name: 'high-volume-trigger',
  rateLimit: 50,    // Max 50 executions per minute
  cooldown: 10000   // 10 second cooldown between executions
}
```

## Monitoring and Observability

### Statistics Endpoint

```bash
curl http://localhost:8080/stats
```

Response:
```json
{
  "total_triggers": 8,
  "active_triggers": 7,
  "trigger_stats": {
    "user-registration": {
      "executions": 1250,
      "successes": 1245,
      "failures": 5
    },
    "cicd-pipeline": {
      "executions": 89,
      "successes": 87,
      "failures": 2
    }
  },
  "uptime": 7200,
  "memory_usage": {
    "rss": 52428800,
    "heapTotal": 20971520,
    "heapUsed": 15728640
  }
}
```

### Structured Logging

Winston-based structured logging with correlation tracking:

```javascript
// Log example
{
  "timestamp": "2024-01-15T14:30:45.123Z",
  "level": "info",
  "message": "Trigger executed successfully",
  "trigger_name": "user-registration",
  "event_id": "evt_1705324245123_abc123",
  "duration_ms": 342,
  "actions_executed": 2,
  "actions_succeeded": 2
}
```

### Health Monitoring

Comprehensive health check with dependency status:

```javascript
{
  "status": "healthy",
  "timestamp": "2024-01-15T14:30:45.123Z",
  "triggers": 8,
  "uptime": 7200,
  "dependencies": {
    "redis": "connected",
    "kafka": "connected", 
    "grpc": "available"
  }
}
```

## Testing

### Unit Tests with Jest

```javascript
const TriggerHandler = require('./trigger-handler');
const supertest = require('supertest');

describe('TriggerHandler', () => {
  let handler;
  let request;

  beforeAll(async () => {
    handler = new TriggerHandler();
    await handler.initialize();
    request = supertest(handler.app);
  });

  test('should handle user registration event', async () => {
    const response = await request
      .post('/webhook/generic')
      .send({
        event_id: 'test-123',
        event_type: 'user.registered', 
        source: 'auth-service',
        data: {
          email: 'test@example.com',
          name: 'Test User'
        }
      })
      .expect(200);

    expect(response.body.status).toBe('accepted');
  });

  test('should evaluate conditions correctly', () => {
    const payload = {
      event_type: 'user.registered',
      data: { email: 'test@example.com' }
    };

    const conditions = [
      { field: 'event_type', operator: 'eq', value: 'user.registered' },
      { field: 'data.email', operator: 'contains', value: '@' }
    ];

    expect(handler.evaluateConditions(conditions, payload)).toBe(true);
  });
});
```

Run tests:
```bash
npm test
```

### Integration Testing

```bash
# Start test environment
docker-compose -f docker-compose.test.yml up -d

# Run integration tests
npm run test:integration

# Clean up
docker-compose -f docker-compose.test.yml down
```

## Docker Configuration

### Dockerfile

```dockerfile
FROM node:18-alpine

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci --only=production

# Copy application code
COPY trigger-handler.js ./

# Create non-root user
RUN addgroup -g 1001 -S nodejs
RUN adduser -S nodejs -u 1001
USER nodejs

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD node -e "require('http').get('http://localhost:8080/health', (res) => { process.exit(res.statusCode === 200 ? 0 : 1) })"

CMD ["node", "trigger-handler.js"]
```

### Docker Compose for Development

```yaml
version: '3.8'

services:
  node-triggers:
    build: .
    ports:
      - "8080:8080"
    environment:
      - NODE_ENV=development
      - REDIS_HOST=redis
      - KAFKA_BROKERS=kafka:9092
    depends_on:
      - redis
      - kafka
    volumes:
      - ./trigger-handler.js:/app/trigger-handler.js
      - ./package.json:/app/package.json

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  zookeeper:
    image: confluentinc/cp-zookeeper:latest
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
      ZOOKEEPER_TICK_TIME: 2000

  kafka:
    image: confluentinc/cp-kafka:latest
    ports:
      - "9092:9092"
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: zookeeper:2181
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
    depends_on:
      - zookeeper
```

## Performance Optimization

### Best Practices

1. **Use connection pooling** for HTTP clients (axios)
2. **Implement circuit breakers** for external services
3. **Use async/await** consistently for non-blocking operations
4. **Monitor memory usage** and implement cleanup for long-running processes
5. **Use clustering** for CPU-intensive workloads

### Scaling Strategies

- **Horizontal scaling**: Multiple Node.js instances behind load balancer
- **Vertical scaling**: Increase CPU/memory for high-throughput scenarios
- **Redis clustering**: For high-volume rate limiting and statistics
- **Kafka partitioning**: Distribute event processing across partitions

## Troubleshooting

### Common Issues

1. **Memory Leaks**
   ```bash
   # Monitor memory usage
   kubectl top pods -n argo-events -l app=tas-mcp-node-triggers
   
   # Enable heap profiling
   node --inspect trigger-handler.js
   ```

2. **Event Processing Delays**
   ```bash
   # Check event queue sizes
   redis-cli -h redis-service.redis info replication
   
   # Monitor Kafka consumer lag
   kubectl exec -it kafka-0 -- kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group tas-mcp-triggers
   ```

3. **Connection Issues**
   ```bash
   # Test Redis connectivity
   kubectl exec -it deployment/tas-mcp-node-triggers -- redis-cli -h redis-service.redis ping
   
   # Test Kafka connectivity
   kubectl exec -it deployment/tas-mcp-node-triggers -- nc -zv kafka-broker.kafka 9092
   ```

### Debug Mode

Enable verbose logging:
```bash
export LOG_LEVEL=debug
node trigger-handler.js
```

This provides detailed information about:
- Event processing flow
- Condition evaluation steps
- Action execution details  
- Template processing
- Error stack traces