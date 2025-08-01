# MCP Server Registry

A comprehensive catalog of Model Context Protocol (MCP) servers, their capabilities, access models, and deployment information.

## 📋 Registry Overview

This registry provides a structured catalog of MCP servers across different categories:

- **AI Models** - LLM integrations and model serving
- **Event Streaming** - Real-time event processing and forwarding  
- **Workflow Orchestration** - Complex workflow and agent coordination
- **Knowledge Bases** - Vector stores and search capabilities
- **Data Processing** - ETL and data transformation services
- **Monitoring** - Observability and metrics collection
- **Communication** - Chat bots and messaging integrations

## 🏗️ Registry Structure

### Files
- **`mcp-servers.json`** - Main registry with all server entries
- **`schema.json`** - JSON Schema for registry validation
- **`README.md`** - This documentation
- **`scripts/`** - Utility scripts for registry management

### Registry Schema

Each MCP server entry includes:

```json
{
  "id": "unique-server-id",
  "name": "Human Readable Name",
  "description": "Brief description of functionality",
  "category": "ai-model|event-streaming|workflow-orchestration|...",
  "provider": {
    "name": "Organization Name",
    "website": "https://provider.com",
    "contact": "contact@provider.com"
  },
  "access": {
    "type": "free|freemium|paid|enterprise|private",
    "authentication": ["none", "api-key", "jwt", "oauth2"],
    "registrationRequired": false
  },
  "protocols": {
    "mcp": "1.0",
    "transport": ["http", "grpc", "websocket"]
  },
  "endpoints": {
    "http": ["https://api.example.com/mcp"],
    "grpc": ["grpc.example.com:443"]
  },
  "features": ["feature1", "feature2"],
  "deployment": {
    "docker": "provider/server:tag",
    "kubernetes": true,
    "helm": "https://charts.example.com"
  }
}
```

## 📊 Registry Statistics

### By Access Type
- **Free**: 6 servers (60%)
- **Freemium**: 3 servers (30%) 
- **Paid**: 0 servers (0%)
- **Enterprise**: 0 servers (0%)
- **Private**: 1 server (10%)

### By Category
- **AI Model**: 3 servers
- **Event Streaming**: 2 servers
- **Workflow Orchestration**: 1 server
- **Knowledge Base**: 1 server
- **Database**: 1 server
- **Monitoring**: 1 server
- **Communication**: 1 server

### By Transport Protocol
- **HTTP**: 9 servers (90%)
- **gRPC**: 6 servers (60%)
- **WebSocket**: 3 servers (30%)
- **TCP**: 2 servers (20%)

## 🚀 Quick Start

### Using the Registry

```bash
# Download the registry
curl -O https://raw.githubusercontent.com/tributary-ai-services/tas-mcp/main/registry/mcp-servers.json

# Find free AI model servers
jq '.servers[] | select(.category == "ai-model" and .access.type == "free")' mcp-servers.json

# List all HTTP endpoints
jq -r '.servers[].endpoints.http[]?' mcp-servers.json | grep -v null

# Find servers with Kubernetes support
jq '.servers[] | select(.deployment.kubernetes == true) | .name' mcp-servers.json
```

### Server Categories

#### 🤖 AI Model Servers
- **Anthropic MCP Server** - Reference Claude integration
- **OpenAI MCP Bridge** - GPT model access via MCP
- **Hugging Face MCP Hub** - 200,000+ models via API

#### 📡 Event Streaming
- **TAS MCP Server** - Event ingestion and forwarding
- **Kafka MCP Bridge** - Apache Kafka integration

#### ⚙️ Workflow Orchestration  
- **LangChain MCP Server** - Agent and chain orchestration

#### 🔍 Knowledge & Data
- **Elasticsearch MCP Connector** - Vector search capabilities
- **Redis MCP Cache** - High-performance caching layer

#### 📊 Monitoring & Communication
- **Prometheus MCP Metrics** - Metrics collection
- **Discord MCP Bot** - Discord bot integration

## 🔧 Contributing

### Adding a New Server

1. Fork this repository
2. Add your server entry to `mcp-servers.json`
3. Validate against the schema:
   ```bash
   npm install -g ajv-cli
   ajv validate -s schema.json -d mcp-servers.json
   ```
4. Submit a pull request

### Server Entry Requirements

**Required Fields:**
- `id` - Unique kebab-case identifier
- `name` - Human-readable name
- `description` - Brief functionality description
- `category` - Primary server category
- `provider` - Organization/maintainer info
- `access` - Access model and authentication
- `protocols` - MCP version and transports

**Recommended Fields:**
- `endpoints` - Live endpoint URLs
- `features` - Key capabilities list
- `deployment` - Container/cloud deployment info
- `documentation` - Links to docs and examples
- `repository` - Source code location

### Quality Standards

- **Working Endpoints**: All listed endpoints should be functional
- **Accurate Information**: Version numbers, features, and access info must be current
- **Complete Documentation**: README and API docs should be accessible
- **Stable Hosting**: Servers should have reasonable uptime expectations

## 🔍 Server Discovery

### By Access Model

**Free & Open Access:**
```bash
jq '.servers[] | select(.access.type == "free") | {name, endpoints}' mcp-servers.json
```

**Freemium Services:**
```bash
jq '.servers[] | select(.access.type == "freemium") | {name, access}' mcp-servers.json
```

### By Protocol Support

**gRPC Servers:**
```bash
jq '.servers[] | select(.protocols.transport[] == "grpc") | .name' mcp-servers.json
```

**WebSocket Support:**
```bash
jq '.servers[] | select(.protocols.transport[] == "websocket") | .name' mcp-servers.json
```

### By Deployment Model

**Kubernetes-Ready:**
```bash
jq '.servers[] | select(.deployment.kubernetes == true) | {name, deployment.helm}' mcp-servers.json
```

**Docker Images:**
```bash
jq -r '.servers[] | select(.deployment.docker) | "\(.name): \(.deployment.docker)"' mcp-servers.json
```

## 📈 Health Monitoring

The registry includes health status for each server when available:

```bash
# Check server health status
jq '.servers[] | {name, status: .status.health}' mcp-servers.json

# Find offline servers
jq '.servers[] | select(.status.health == "offline") | .name' mcp-servers.json
```

## 🛡️ Security Considerations

When connecting to MCP servers:

1. **Authentication** - Use proper API keys and tokens
2. **Rate Limits** - Respect server rate limiting policies  
3. **Data Privacy** - Review data handling policies
4. **Network Security** - Prefer HTTPS/TLS connections
5. **Access Control** - Implement proper authorization

## 📚 Additional Resources

- [Model Context Protocol Specification](https://github.com/anthropics/mcp)
- [TAS MCP Server Documentation](../docs/DESIGN.md)
- [MCP Community Discord](https://discord.gg/mcp-community)
- [Registry Schema Definition](./schema.json)

## 📄 License

This registry is provided under the Apache 2.0 License. Server entries link to third-party services with their own terms of service and licenses.