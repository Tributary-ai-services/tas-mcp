# TAS MCP Trigger Examples

This directory contains examples of how to add new triggers to the TAS MCP system using the Argo Events paradigm. The examples demonstrate event source definitions, sensor configurations, and trigger implementations in multiple languages.

## Argo Events Paradigm Overview

The TAS MCP system follows the Argo Events architecture:

1. **Event Sources** - Define how events are received (HTTP webhooks, message queues, etc.)
2. **Sensors** - Define trigger conditions and actions based on events
3. **Triggers** - Execute specific actions when conditions are met

## Example Structure

```
examples/triggers/
├── go/                 # Go language examples
├── python/             # Python language examples  
├── node/               # Node.js language examples
├── k8s/                # Kubernetes manifests
└── README.md           # This file
```

## Event Flow

```
Event Source → Gateway → Sensor → Trigger → Action
```

## Languages Covered

- **Go**: Native Argo Events integration with gRPC
- **Python**: REST API integration with asyncio
- **Node.js**: Event-driven architecture with Express

## Getting Started

1. Choose your preferred language directory
2. Review the event source definitions in `k8s/`
3. Follow the language-specific setup instructions
4. Deploy and test the examples

Each language directory contains:
- Event source configuration
- Sensor definitions
- Trigger implementations
- Setup and testing instructions