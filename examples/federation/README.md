# Federation Integration Examples

This directory contains comprehensive integration examples for all TAS MCP Federation servers. Each example demonstrates how to use the federation API to interact with different MCP servers.

## Available Integration Examples

### 🔍 [DuckDuckGo MCP](duckduckgo-mcp/)
Privacy-focused web search integration with:
- Web search with region and safety controls
- News search with time filtering
- Image search with advanced filters
- Webpage content extraction
- Privacy-focused search patterns

### 🕷️ [Apify MCP](apify-mcp/)
Comprehensive web scraping platform integration with:
- Access to 5,000+ web scraping actors
- Actor discovery and search by category
- Custom scraping configurations
- Dataset management and export
- Multi-category scraping workflows

### 🗃️ [PostgreSQL MCP](postgres-mcp/)
Secure database integration with:
- Read-only SQL query execution
- Database schema inspection
- Table metadata analysis
- Query performance analysis
- Connection health monitoring

### 🛠️ [Git MCP](git-mcp/)
Repository automation and management with:
- Repository status and diff operations
- Branch creation and management
- Commit operations and staging
- Repository history access
- Working tree management

## Running Examples

Each example is a standalone Go module. To run any example:

```bash
# Navigate to the specific example
cd examples/federation/[example-name]

# Install dependencies
go mod tidy

# Run the example
go run main.go
```

## Prerequisites

### TAS MCP Federation Server
All examples require the TAS MCP Federation server running:
```bash
# Start the federation server
docker-compose -f deployments/docker-compose/full-stack.yml up -d

# Or run locally
go run cmd/server/main.go
```

### Individual MCP Servers
Each example requires its corresponding MCP server:

- **DuckDuckGo MCP**: `http://localhost:3402`
- **Apify MCP**: `http://localhost:3403`
- **PostgreSQL MCP**: `http://localhost:3401`
- **Git MCP**: `http://localhost:3000`

## Federation API Usage

All examples demonstrate:
- Federation server registration
- Health check validation
- Service capability invocation
- Error handling and retry logic
- Real-world usage scenarios

## Development

Each example includes:
- Complete Go module with dependencies
- Comprehensive README with setup instructions
- Real-world usage scenarios
- Error handling examples
- Best practices demonstration

## Integration Patterns

These examples show common patterns for:
- **Service Discovery**: Finding and registering MCP servers
- **Health Monitoring**: Checking server availability
- **Request/Response**: Making API calls through federation
- **Error Handling**: Graceful failure management
- **Authentication**: Managing API keys and tokens
- **Configuration**: Setting up server connections