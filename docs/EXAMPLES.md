# 💡 TAS MCP Server Examples

This document provides practical examples of using the TAS MCP Server in various scenarios and integration patterns.

## Table of Contents

- [Basic Usage](#basic-usage)
- [Integration Examples](#integration-examples)
- [Advanced Configurations](#advanced-configurations)
- [Client Examples](#client-examples)
- [Use Case Scenarios](#use-case-scenarios)

## Basic Usage

### Simple Event Ingestion

#### HTTP API

```bash
# Single event
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_id": "user-123-login",
    "event_type": "user.login",
    "source": "auth-service",
    "data": "{\"user_id\": \"123\", \"ip\": \"192.168.1.1\", \"timestamp\": \"2024-01-15T10:30:00Z\"}"
  }'

# Batch events
curl -X POST http://localhost:8080/api/v1/events/batch \
  -H "Content-Type: application/json" \
  -d '[
    {
      "event_id": "order-456",
      "event_type": "order.created",
      "source": "e-commerce",
      "data": "{\"order_id\": \"456\", \"customer_id\": \"789\", \"amount\": 99.99}"
    },
    {
      "event_id": "payment-789",
      "event_type": "payment.processed",
      "source": "payment-gateway",
      "data": "{\"payment_id\": \"789\", \"order_id\": \"456\", \"status\": \"completed\"}"
    }
  ]'
```

#### gRPC Client (Go)

```go
package main

import (
    "context"
    "log"
    "time"

    "google.golang.org/grpc"
    mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
)

func main() {
    // Connect to server
    conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer conn.Close()

    client := mcpv1.NewMCPServiceClient(conn)

    // Single event ingestion
    resp, err := client.IngestEvent(context.Background(), &mcpv1.IngestEventRequest{
        EventId:   "golang-client-test",
        EventType: "test.event",
        Source:    "golang-client",
        Timestamp: time.Now().Unix(),
        Data:      `{"message": "Hello from Go client"}`,
        Metadata: map[string]string{
            "client_version": "1.0.0",
            "environment":    "development",
        },
    })
    if err != nil {
        log.Fatalf("Failed to ingest event: %v", err)
    }

    log.Printf("Event ingested: %s, status: %s", resp.EventId, resp.Status)
}
```

## Integration Examples

### Argo Events Integration

#### Event Source Configuration

```yaml
# webhook-eventsource.yaml
apiVersion: argoproj.io/v1alpha1
kind: EventSource
metadata:
  name: tas-mcp-webhook
  namespace: argo-events
spec:
  webhook:
    mcp-events:
      port: "12000"
      endpoint: /webhook
      method: POST
```

#### Sensor Configuration

```yaml
# mcp-sensor.yaml
apiVersion: argoproj.io/v1alpha1
kind: Sensor
metadata:
  name: mcp-event-processor
  namespace: argo-events
spec:
  template:
    serviceAccountName: operate-workflow-sa
  dependencies:
  - name: mcp-events
    eventSourceName: tas-mcp-webhook
    eventName: mcp-events
  triggers:
  - template:
      name: process-critical-event
      conditions: "data.event_type contains 'critical'"
      argoWorkflow:
        group: argoproj.io
        version: v1alpha1
        resource: workflows
        operation: create
        source:
          resource:
            apiVersion: argoproj.io/v1alpha1
            kind: Workflow
            metadata:
              generateName: critical-event-handler-
            spec:
              entrypoint: handle-event
              templates:
              - name: handle-event
                container:
                  image: alpine:latest
                  command: [sh, -c]
                  args: ["echo 'Processing critical event: {{workflow.parameters.event_id}}'"]
```

### Kafka Integration

#### Producer Configuration

```json
{
  "forwarding": {
    "enabled": true,
    "targets": [
      {
        "id": "kafka-events",
        "name": "Kafka Event Stream",
        "type": "kafka",
        "endpoint": "kafka-broker.kafka:9092",
        "config": {
          "topic": "mcp-events",
          "batch_size": 100,
          "batch_timeout": "5s",
          "compression": "gzip"
        },
        "rules": [
          {
            "id": "all-events",
            "conditions": [
              {
                "field": "event_type",
                "operator": "ne",
                "value": ""
              }
            ]
          }
        ]
      }
    ]
  }
}
```

#### Consumer Example (Go)

```go
package main

import (
    "context"
    "encoding/json"
    "log"

    "github.com/segmentio/kafka-go"
)

type MCPEvent struct {
    EventID   string            `json:"event_id"`
    EventType string            `json:"event_type"`
    Source    string            `json:"source"`
    Data      string            `json:"data"`
    Metadata  map[string]string `json:"metadata"`
}

func main() {
    reader := kafka.NewReader(kafka.ReaderConfig{
        Brokers:   []string{"localhost:9092"},
        Topic:     "mcp-events",
        GroupID:   "mcp-consumer-group",
        Partition: 0,
        MinBytes:  10e3, // 10KB
        MaxBytes:  10e6, // 10MB
    })
    defer reader.Close()

    for {
        message, err := reader.ReadMessage(context.Background())
        if err != nil {
            log.Printf("Error reading message: %v", err)
            continue
        }

        var event MCPEvent
        if err := json.Unmarshal(message.Value, &event); err != nil {
            log.Printf("Error unmarshaling event: %v", err)
            continue
        }

        log.Printf("Received event: %s, type: %s", event.EventID, event.EventType)
        
        // Process event
        processEvent(event)
    }
}

func processEvent(event MCPEvent) {
    // Implement your event processing logic here
    switch event.EventType {
    case "user.created":
        handleUserCreated(event)
    case "order.completed":
        handleOrderCompleted(event)
    default:
        log.Printf("Unknown event type: %s", event.EventType)
    }
}
```

### Slack Integration

```json
{
  "forwarding": {
    "targets": [
      {
        "id": "slack-alerts",
        "name": "Slack Alert Channel",
        "type": "http",
        "endpoint": "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK",
        "config": {
          "timeout": "10s",
          "retry_attempts": 3,
          "headers": {
            "Content-Type": "application/json"
          }
        },
        "rules": [
          {
            "id": "critical-alerts",
            "conditions": [
              {
                "field": "event_type",
                "operator": "contains",
                "value": "alert.critical"
              }
            ],
            "transform": {
              "template": "{\"text\": \"🚨 Critical Alert: {{data.title}}\", \"attachments\": [{\"color\": \"danger\", \"fields\": [{\"title\": \"Service\", \"value\": \"{{data.service}}\", \"short\": true}, {\"title\": \"Severity\", \"value\": \"{{data.severity}}\", \"short\": true}]}]}"
            }
          }
        ]
      }
    ]
  }
}
```

### PagerDuty Integration

```json
{
  "forwarding": {
    "targets": [
      {
        "id": "pagerduty-incidents",
        "name": "PagerDuty Integration",
        "type": "http",
        "endpoint": "https://api.pagerduty.com/incidents",
        "config": {
          "headers": {
            "Authorization": "Token token=YOUR_PAGERDUTY_TOKEN",
            "Content-Type": "application/json",
            "Accept": "application/vnd.pagerduty+json;version=2"
          }
        },
        "rules": [
          {
            "id": "create-incident",
            "conditions": [
              {
                "field": "event_type",
                "operator": "eq",
                "value": "system.outage"
              }
            ],
            "transform": {
              "template": "{\"incident\": {\"type\": \"incident\", \"title\": \"{{data.title}}\", \"service\": {\"id\": \"{{data.service_id}}\", \"type\": \"service_reference\"}, \"urgency\": \"high\"}}"
            }
          }
        ]
      }
    ]
  }
}
```

## Advanced Configurations

### Multi-Environment Setup

```json
{
  "forwarding": {
    "enabled": true,
    "targets": [
      {
        "id": "dev-kafka",
        "type": "kafka",
        "endpoint": "kafka-dev.internal:9092",
        "rules": [
          {
            "conditions": [
              {"field": "metadata.environment", "operator": "eq", "value": "development"}
            ]
          }
        ]
      },
      {
        "id": "prod-kafka",
        "type": "kafka", 
        "endpoint": "kafka-prod.internal:9092",
        "rules": [
          {
            "conditions": [
              {"field": "metadata.environment", "operator": "eq", "value": "production"}
            ]
          }
        ]
      }
    ]
  }
}
```

### Event Filtering and Transformation

```json
{
  "forwarding": {
    "rules": [
      {
        "id": "filter-sensitive-data",
        "priority": 100,
        "conditions": [
          {"field": "data.sensitive", "operator": "eq", "value": true}
        ],
        "actions": [
          {"type": "drop", "config": {"reason": "sensitive_data"}}
        ]
      },
      {
        "id": "enrich-events",  
        "priority": 50,
        "conditions": [
          {"field": "event_type", "operator": "ne", "value": ""}
        ],
        "actions": [
          {
            "type": "transform",
            "config": {
              "add_fields": {
                "processed_by": "tas-mcp-server",
                "processed_at": "{{current_timestamp}}",
                "version": "1.0"
              },
              "remove_fields": ["metadata.internal_id"]
            }
          }
        ]
      }
    ]
  }
}
```

### Rate Limiting Configuration

```json
{
  "forwarding": {
    "targets": [
      {
        "id": "external-api",
        "type": "http",
        "endpoint": "https://external-service.com/webhook",
        "rules": [
          {
            "rate_limit": {
              "requests_per_second": 10,
              "burst_size": 20,
              "window": "60s"
            }
          }
        ]
      }
    ]
  }
}
```

## Client Examples

### Python Client

```python
import requests
import json
import time
import uuid

class MCPClient:
    def __init__(self, base_url, api_key=None):
        self.base_url = base_url.rstrip('/')
        self.session = requests.Session()
        if api_key:
            self.session.headers.update({'Authorization': f'Bearer {api_key}'})
    
    def ingest_event(self, event_type, source, data, event_id=None, metadata=None):
        event = {
            'event_id': event_id or str(uuid.uuid4()),
            'event_type': event_type,
            'source': source,
            'timestamp': int(time.time()),
            'data': json.dumps(data) if isinstance(data, dict) else data,
            'metadata': metadata or {}
        }
        
        response = self.session.post(
            f'{self.base_url}/api/v1/events',
            json=event
        )
        response.raise_for_status()
        return response.json()
    
    def batch_ingest(self, events):
        formatted_events = []
        for event in events:
            formatted_events.append({
                'event_id': event.get('event_id', str(uuid.uuid4())),
                'event_type': event['event_type'],
                'source': event['source'],
                'timestamp': event.get('timestamp', int(time.time())),
                'data': json.dumps(event['data']) if isinstance(event['data'], dict) else event['data'],
                'metadata': event.get('metadata', {})
            })
        
        response = self.session.post(
            f'{self.base_url}/api/v1/events/batch',
            json=formatted_events
        )
        response.raise_for_status()
        return response.json()

# Usage example
if __name__ == '__main__':
    client = MCPClient('http://localhost:8080')
    
    # Single event
    result = client.ingest_event(
        event_type='user.signup',
        source='web-app',
        data={'user_id': '12345', 'email': 'user@example.com'},
        metadata={'source_ip': '192.168.1.1'}
    )
    print(f"Event ingested: {result}")
    
    # Batch events
    events = [
        {
            'event_type': 'order.created',
            'source': 'e-commerce',
            'data': {'order_id': 'ord-001', 'amount': 99.99}
        },
        {
            'event_type': 'inventory.updated',
            'source': 'inventory-service',
            'data': {'product_id': 'prod-123', 'quantity': 50}
        }
    ]
    
    batch_result = client.batch_ingest(events)
    print(f"Batch processed: {batch_result['processed']} events")
```

### Node.js Client

```javascript
const axios = require('axios');
const { v4: uuidv4 } = require('uuid');

class MCPClient {
    constructor(baseUrl, apiKey = null) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.client = axios.create({
            baseURL: this.baseUrl,
            headers: apiKey ? { 'Authorization': `Bearer ${apiKey}` } : {}
        });
    }

    async ingestEvent(eventType, source, data, options = {}) {
        const event = {
            event_id: options.eventId || uuidv4(),
            event_type: eventType,
            source,
            timestamp: options.timestamp || Math.floor(Date.now() / 1000),
            data: typeof data === 'string' ? data : JSON.stringify(data),
            metadata: options.metadata || {}
        };

        try {
            const response = await this.client.post('/api/v1/events', event);
            return response.data;
        } catch (error) {
            throw new Error(`Failed to ingest event: ${error.response?.data?.error || error.message}`);
        }
    }

    async batchIngest(events) {
        const formattedEvents = events.map(event => ({
            event_id: event.eventId || uuidv4(),
            event_type: event.eventType,
            source: event.source,
            timestamp: event.timestamp || Math.floor(Date.now() / 1000),
            data: typeof event.data === 'string' ? event.data : JSON.stringify(event.data),
            metadata: event.metadata || {}
        }));

        try {
            const response = await this.client.post('/api/v1/events/batch', formattedEvents);
            return response.data;
        } catch (error) {
            throw new Error(`Failed to batch ingest: ${error.response?.data?.error || error.message}`);
        }
    }

    async getHealth() {
        try {
            const response = await this.client.get('/health');
            return response.data;
        } catch (error) {
            throw new Error(`Health check failed: ${error.message}`);
        }
    }
}

// Usage example
async function main() {
    const client = new MCPClient('http://localhost:8080');

    try {
        // Single event
        const result = await client.ingestEvent(
            'user.login',
            'auth-service',
            { userId: '12345', sessionId: 'sess-abc123' },
            { metadata: { userAgent: 'Mozilla/5.0...' } }
        );
        console.log('Event ingested:', result);

        // Batch events
        const events = [
            {
                eventType: 'page.view',
                source: 'web-analytics',
                data: { page: '/home', userId: '12345' }
            },
            {
                eventType: 'button.click',
                source: 'web-analytics', 
                data: { button: 'signup', page: '/home' }
            }
        ];

        const batchResult = await client.batchIngest(events);
        console.log(`Batch processed: ${batchResult.processed} events`);

    } catch (error) {
        console.error('Error:', error.message);
    }
}

main();
```

## Use Case Scenarios

### E-commerce Event Tracking

```bash
# User registration
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_id": "reg-' $(date +%s) '",
    "event_type": "user.registered",
    "source": "registration-service", 
    "data": "{\"user_id\": \"usr-123\", \"email\": \"john@example.com\", \"plan\": \"premium\"}"
  }'

# Product view
curl -X POST http://localhost:8080/api/v1/events \
  -d '{
    "event_type": "product.viewed",
    "source": "web-frontend",
    "data": "{\"product_id\": \"prod-456\", \"user_id\": \"usr-123\", \"category\": \"electronics\"}"
  }'

# Purchase completed
curl -X POST http://localhost:8080/api/v1/events \
  -d '{
    "event_type": "order.completed",
    "source": "checkout-service",
    "data": "{\"order_id\": \"ord-789\", \"user_id\": \"usr-123\", \"amount\": 299.99, \"items\": [{\"product_id\": \"prod-456\", \"quantity\": 1}]}"
  }'
```

### IoT Device Monitoring

```python
import time
import random
import json
from mcp_client import MCPClient

client = MCPClient('http://localhost:8080')

def simulate_sensor_data():
    devices = ['sensor-001', 'sensor-002', 'sensor-003']
    
    while True:
        for device_id in devices:
            # Temperature reading
            temp_data = {
                'device_id': device_id,
                'temperature': round(random.uniform(18.0, 25.0), 2),
                'humidity': round(random.uniform(40.0, 60.0), 2),
                'battery_level': random.randint(60, 100)
            }
            
            client.ingest_event(
                event_type='sensor.reading',
                source=f'iot-device-{device_id}',
                data=temp_data,
                metadata={'location': 'office-floor-2'}
            )
            
            # Simulate alert condition
            if temp_data['temperature'] > 24.0:
                client.ingest_event(
                    event_type='sensor.alert.high_temperature',
                    source=f'iot-device-{device_id}',
                    data={
                        'device_id': device_id,
                        'temperature': temp_data['temperature'],
                        'threshold': 24.0
                    }
                )
            
        time.sleep(30)  # Send data every 30 seconds

if __name__ == '__main__':
    simulate_sensor_data()
```

### CI/CD Pipeline Events

```yaml
# .github/workflows/deploy.yml
name: Deployment Pipeline
on:
  push:
    branches: [main]

jobs:
  notify-start:
    runs-on: ubuntu-latest
    steps:
    - name: Notify deployment start
      run: |
        curl -X POST ${{ secrets.MCP_SERVER_URL }}/api/v1/events \
          -H "Authorization: Bearer ${{ secrets.MCP_API_KEY }}" \
          -H "Content-Type: application/json" \
          -d '{
            "event_type": "deployment.started",
            "source": "github-actions",
            "data": "{\"repository\": \"${{ github.repository }}\", \"commit\": \"${{ github.sha }}\", \"branch\": \"${{ github.ref }}\"}"
          }'

  deploy:
    runs-on: ubuntu-latest
    needs: notify-start
    steps:
    - name: Deploy application
      run: |
        # Deployment steps here
        echo "Deploying application..."
        
    - name: Notify deployment success
      if: success()
      run: |
        curl -X POST ${{ secrets.MCP_SERVER_URL }}/api/v1/events \
          -H "Authorization: Bearer ${{ secrets.MCP_API_KEY }}" \
          -H "Content-Type: application/json" \
          -d '{
            "event_type": "deployment.succeeded",
            "source": "github-actions",
            "data": "{\"repository\": \"${{ github.repository }}\", \"commit\": \"${{ github.sha }}\", \"duration\": \"${{ job.duration }}\"}"
          }'
          
    - name: Notify deployment failure
      if: failure()
      run: |
        curl -X POST ${{ secrets.MCP_SERVER_URL }}/api/v1/events \
          -H "Authorization: Bearer ${{ secrets.MCP_API_KEY }}" \
          -H "Content-Type: application/json" \
          -d '{
            "event_type": "deployment.failed",
            "source": "github-actions",
            "data": "{\"repository\": \"${{ github.repository }}\", \"commit\": \"${{ github.sha }}\", \"error\": \"Deployment failed\"}"
          }'
```

These examples demonstrate the flexibility and power of the TAS MCP Server for various integration scenarios. Adapt them to your specific use cases and requirements.