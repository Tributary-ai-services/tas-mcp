# 🐘 PostgreSQL MCP Server

Advanced PostgreSQL database integration server with read-only access, schema inspection, and query analysis for the TAS MCP Federation.

## 🎯 Overview

The PostgreSQL MCP Server provides secure, read-only access to PostgreSQL databases through the Model Context Protocol (MCP). It enables AI agents and applications to safely query databases, inspect schemas, and analyze query execution plans without risk of data modification.

## ✨ Key Features

- **🔒 Security-First**: Read-only mode with transaction-level isolation
- **🔍 Schema Inspection**: Comprehensive database metadata and structure analysis  
- **📊 Query Analysis**: Execution plan analysis without running queries
- **🏊 Connection Pooling**: Efficient database connection management
- **🏥 Health Monitoring**: Built-in health checks and metrics
- **📋 Resource Discovery**: Automatic table and schema enumeration
- **🐳 Docker Ready**: Containerized deployment with sample data

## 🚀 Quick Start

### Using Docker Compose (Recommended)

```bash
# 1. Navigate to postgres-mcp directory
cd deployments/docker-compose/postgres-mcp

# 2. Copy environment template
cp .env.example .env

# 3. Customize your settings (optional)
vim .env

# 4. Start PostgreSQL database and MCP server
docker-compose up -d

# 5. Check service health
curl http://localhost:3401/health
```

### Standalone Docker

```bash
# Build the PostgreSQL MCP server
docker build -t tas-mcp/postgres-mcp-server:1.0.0 -f deployments/postgres-mcp/Dockerfile .

# Run with your database
docker run -d \
  --name postgres-mcp-server \
  -e DATABASE_URL="postgresql://user:pass@host:5432/dbname" \
  -e READ_ONLY=true \
  -p 3401:3401 \
  tas-mcp/postgres-mcp-server:1.0.0
```

## 🔧 Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DATABASE_URL` | PostgreSQL connection string | `postgresql://postgres:password@postgres-db:5432/postgres` |
| `READ_ONLY` | Enable read-only mode | `true` |
| `MAX_CONNECTIONS` | Connection pool size | `10` |
| `QUERY_TIMEOUT` | Query timeout (ms) | `30000` |
| `HEALTH_CHECK_ENABLED` | Enable health endpoint | `true` |
| `HEALTH_PORT` | Health check port | `3401` |
| `LOG_LEVEL` | Logging level | `info` |

### Connection String Format

```
postgresql://[user[:password]@][host][:port][/dbname][?param1=value1&...]
```

**Examples:**
```bash
# Basic connection
DATABASE_URL="postgresql://postgres:password@localhost:5432/mydb"

# With SSL
DATABASE_URL="postgresql://user:pass@host:5432/db?sslmode=require"

# Connection pool settings
DATABASE_URL="postgresql://user:pass@host:5432/db?pool_max_conns=20&pool_timeout=10s"
```

## 🛠️ MCP Tools & Capabilities

### Available Tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `query` | Execute read-only SQL queries | `sql`, `params` (optional) |
| `describe_table` | Get detailed table schema | `table_name`, `schema_name` |
| `list_tables` | List all accessible tables | `schema_name` (optional) |
| `analyze_query` | Analyze query execution plan | `sql` |

### Resources

- **Table Resources**: `postgres://table/{schema}/{table}`
- **Auto-discovery**: Automatically enumerates all accessible tables
- **Metadata**: Includes column types, constraints, indexes

### Example Usage

#### Query Execution
```json
{
  "name": "query",
  "arguments": {
    "sql": "SELECT id, name, email FROM users WHERE created_at > $1 LIMIT 10",
    "params": ["2025-01-01"]
  }
}
```

#### Schema Inspection
```json
{
  "name": "describe_table",
  "arguments": {
    "table_name": "users",
    "schema_name": "public"
  }
}
```

#### Query Analysis
```json
{
  "name": "analyze_query", 
  "arguments": {
    "sql": "SELECT u.*, COUNT(o.id) FROM users u LEFT JOIN orders o ON u.id = o.user_id GROUP BY u.id"
  }
}
```

## 📊 Sample Database

The included sample database contains a complete e-commerce schema:

### Tables
- **Users & Addresses**: Customer management
- **Products & Categories**: Product catalog
- **Orders & Order Items**: Transaction records
- **Reviews**: Customer feedback
- **Inventory**: Stock tracking

### Views
- `product_summary`: Products with ratings and categories
- `order_summary`: Orders with customer info
- `user_activity`: Customer analytics

### Functions
- `get_product_analytics(product_id)`: Product performance metrics

## 🏥 Health & Monitoring

### Health Check Endpoint

```bash
curl http://localhost:3401/health
```

**Response:**
```json
{
  "status": "healthy",
  "service": "postgres-mcp-server", 
  "version": "1.0.0",
  "timestamp": "2025-08-04T12:00:00Z",
  "database": "connected"
}
```

### Docker Health Check

The container includes built-in health checks:
- **Interval**: 30 seconds
- **Timeout**: 10 seconds
- **Retries**: 3
- **Start Period**: 15 seconds

## 🔒 Security Features

### Read-Only Mode
- ✅ **Transaction Isolation**: All queries run in `READ ONLY` transactions
- ✅ **Query Validation**: Blocks non-SELECT statements
- ✅ **SQL Injection Protection**: Parameterized queries
- ✅ **Connection Security**: Secure connection pooling

### Access Control
- 🔐 Database-level authentication
- 🔐 Connection string security
- 🔐 Network isolation via Docker networks
- 🔐 Non-root container execution

## 🐳 Docker Configuration

### Image Details
- **Base**: Node.js 20 Alpine
- **User**: Non-root `postgres-mcp` user
- **Ports**: 3401 (health check)
- **Volumes**: `/app/logs` for logging

### Docker Compose Services
- **postgres-db**: PostgreSQL 16 database with sample data
- **postgres-mcp**: MCP server connecting to database

## 🔗 Integration Examples

### With TAS MCP Federation

```bash
# Register with federation
curl -X POST http://localhost:8080/api/v1/federation/servers \
  -H "Content-Type: application/json" \
  -d '{
    "id": "postgres-mcp-server-v1.0.0",
    "name": "PostgreSQL MCP Server", 
    "endpoint": "http://postgres-mcp-server:3400",
    "protocol": "http",
    "capabilities": ["query", "describe_table", "list_tables", "analyze_query"]
  }'
```

### With Claude Desktop

```json
{
  "mcpServers": {
    "postgres": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "--network", "mcp-network",
        "-e", "DATABASE_URL=postgresql://postgres:password@postgres-db:5432/sampledb",
        "tas-mcp/postgres-mcp-server:1.0.0"
      ]
    }
  }
}
```

## 🧪 Testing

### Basic Connection Test
```bash
# Test database connectivity
docker exec postgres-mcp-server node -e "
const { Pool } = require('pg');
const pool = new Pool({ connectionString: process.env.DATABASE_URL });
pool.connect().then(() => console.log('✅ Connected')).catch(console.error);
"
```

### Query Testing
```bash
# Test sample queries
curl -X POST http://localhost:8080/api/v1/federation/servers/postgres-mcp-server-v1.0.0/invoke \
  -H "Content-Type: application/json" \
  -d '{
    "method": "query",
    "params": {
      "sql": "SELECT COUNT(*) as user_count FROM users"
    }
  }'
```

## 🚨 Troubleshooting

### Common Issues

#### Connection Issues
```bash
# Check database accessibility
docker exec postgres-db pg_isready -U postgres

# Verify network connectivity  
docker exec postgres-mcp-server nc -zv postgres-database 5432
```

#### Permission Issues
```bash
# Check database user permissions
docker exec postgres-db psql -U postgres -c "\\du"

# Test read permissions
docker exec postgres-db psql -U postgres -c "SELECT 1 FROM users LIMIT 1;"
```

#### Resource Issues
```bash
# Check available connections
docker exec postgres-db psql -U postgres -c "SELECT * FROM pg_stat_activity;"

# Monitor connection pool
curl http://localhost:3401/health | jq '.connection_pool'
```

### Debug Commands

```bash
# View server logs
docker logs postgres-mcp-server -f

# Check database logs
docker logs postgres-db -f

# Inspect container
docker exec -it postgres-mcp-server sh
```

## 📚 Advanced Usage

### Custom Database Schema

Replace `init-db.sql` with your schema:
```bash
# Mount custom initialization
docker run -v ./my-schema.sql:/docker-entrypoint-initdb.d/init.sql postgres:16-alpine
```

### Production Deployment

```bash
# Use production settings
docker run -d \
  --name postgres-mcp-prod \
  -e DATABASE_URL="postgresql://readonly_user:secure_pass@prod-db:5432/proddb?sslmode=require" \
  -e READ_ONLY=true \
  -e MAX_CONNECTIONS=5 \
  -e QUERY_TIMEOUT=15000 \
  --restart unless-stopped \
  tas-mcp/postgres-mcp-server:1.0.0
```

### Custom Queries

Create application-specific query tools by extending the server implementation in the Dockerfile.

---

**Next Steps**: 
- Explore [Git MCP Integration](../git-mcp/README.md)
- Check [Full Stack Deployment](../README.md)
- Review [TAS MCP Federation](../../README.md)