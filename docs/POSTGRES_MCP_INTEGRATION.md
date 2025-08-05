# 🐘 PostgreSQL MCP Server Integration Guide

Complete guide for integrating PostgreSQL databases with the TAS MCP Federation using the PostgreSQL MCP Server.

## 📋 Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [MCP Tools & Capabilities](#mcp-tools--capabilities)
- [Security & Best Practices](#security--best-practices)
- [Deployment Options](#deployment-options)
- [Monitoring & Observability](#monitoring--observability)
- [Troubleshooting](#troubleshooting)
- [Advanced Usage](#advanced-usage)

## 🎯 Overview

The PostgreSQL MCP Server provides a secure, read-only interface for AI agents and applications to interact with PostgreSQL databases through the Model Context Protocol (MCP). It enables powerful database operations while maintaining strict security boundaries.

### Key Features

- **🔒 Security-First Design**: Read-only transactions with comprehensive SQL injection protection
- **📊 Advanced Schema Inspection**: Complete database metadata and structure analysis
- **⚡ Query Performance Analysis**: Execution plan analysis without running queries
- **🏊‍♂️ Connection Management**: Efficient connection pooling and resource management
- **🔍 Resource Discovery**: Automatic enumeration of tables, views, and schemas
- **🏥 Health Monitoring**: Built-in health checks and operational metrics
- **🐳 Cloud-Ready**: Docker and Kubernetes deployment support

## 🏗️ Architecture

```mermaid
graph TB
    subgraph "TAS MCP Federation"
        FM[Federation Manager]
        API[HTTP/gRPC API]
    end
    
    subgraph "PostgreSQL MCP Server"
        MCP[MCP Server]
        Pool[Connection Pool]
        Auth[Auth Manager]
        Health[Health Monitor]
    end
    
    subgraph "PostgreSQL Database"
        DB[(PostgreSQL)]
        Schema[Schema Objects]
        Data[Application Data]
    end
    
    Client --> API
    API --> FM
    FM --> MCP
    MCP --> Pool
    Pool --> DB
    MCP --> Auth
    MCP --> Health
    Health --> DB
    
    DB --> Schema
    DB --> Data
```

### Component Overview

| Component | Purpose | Technology |
|-----------|---------|------------|
| **MCP Server** | Protocol implementation and request handling | Node.js + MCP SDK |
| **Connection Pool** | Database connection management | PostgreSQL pg pool |
| **Auth Manager** | Authentication and authorization | Connection string auth |
| **Health Monitor** | Service health and database connectivity | HTTP health endpoint |
| **Query Validator** | SQL security and read-only enforcement | AST parsing |

## 🚀 Quick Start

### 1. Docker Compose Deployment (Recommended)

```bash
# Clone and navigate to PostgreSQL MCP directory
cd deployments/docker-compose/postgres-mcp

# Configure environment
cp .env.example .env
vim .env  # Customize settings

# Start PostgreSQL database and MCP server
docker-compose up -d

# Verify services are running
docker-compose ps
curl http://localhost:3401/health
```

### 2. Register with TAS MCP Federation

```bash
# Register PostgreSQL MCP server
curl -X POST http://localhost:8080/api/v1/federation/servers \
  -H "Content-Type: application/json" \
  -d '{
    "id": "postgres-mcp-server-v1.0.0",
    "name": "PostgreSQL MCP Server",
    "description": "Advanced PostgreSQL database integration with read-only access",
    "version": "1.0.0",
    "category": "database",
    "endpoint": "http://postgres-mcp-server:3401",
    "protocol": "http",
    "capabilities": ["query", "describe_table", "list_tables", "analyze_query"]
  }'
```

### 3. Test Integration

```bash
# List all tables
curl -X POST http://localhost:8080/api/v1/federation/servers/postgres-mcp-server-v1.0.0/invoke \
  -H "Content-Type: application/json" \
  -d '{"method": "list_tables", "params": {}}'

# Query sample data
curl -X POST http://localhost:8080/api/v1/federation/servers/postgres-mcp-server-v1.0.0/invoke \
  -H "Content-Type: application/json" \
  -d '{"method": "query", "params": {"sql": "SELECT COUNT(*) FROM users"}}'
```

## ⚙️ Configuration

### Environment Variables

#### Core Configuration
```bash
# Database connection
DATABASE_URL=postgresql://user:password@host:5432/database

# Security settings
READ_ONLY=true                    # Enforce read-only mode
MAX_CONNECTIONS=10               # Connection pool size
QUERY_TIMEOUT=30000             # Query timeout (ms)

# Service configuration
HEALTH_CHECK_ENABLED=true       # Enable health endpoint
HEALTH_PORT=3401                # Health check port
LOG_LEVEL=info                  # Logging level
```

#### Advanced Configuration
```bash
# Connection pool tuning
POOL_IDLE_TIMEOUT=30000         # Idle connection timeout
POOL_CONNECTION_TIMEOUT=2000    # Connection establishment timeout
POOL_STATEMENT_TIMEOUT=30000    # Statement execution timeout

# Security enhancements
ENABLE_SSL=true                 # Enable SSL connections
SSL_MODE=require               # SSL mode (disable, prefer, require, verify-ca, verify-full)
SSL_CERT_PATH=/certs/client.crt # SSL certificate path
SSL_KEY_PATH=/certs/client.key  # SSL private key path
SSL_CA_PATH=/certs/ca.crt      # SSL CA certificate path

# Query restrictions
MAX_QUERY_LENGTH=10000         # Maximum SQL query length
ALLOWED_SCHEMAS=public,app     # Comma-separated list of allowed schemas
BLOCKED_TABLES=secrets,tokens  # Comma-separated list of blocked tables
```

### Connection String Examples

```bash
# Basic connection
DATABASE_URL="postgresql://postgres:password@localhost:5432/mydb"

# With SSL
DATABASE_URL="postgresql://user:pass@host:5432/db?sslmode=require"

# With connection pool settings
DATABASE_URL="postgresql://user:pass@host:5432/db?pool_max_conns=20&pool_timeout=10s"

# Read-only user (recommended)
DATABASE_URL="postgresql://readonly_user:secure_pass@host:5432/db?sslmode=require"

# Cloud database examples
DATABASE_URL="postgresql://user:pass@aws-rds-endpoint:5432/prod?sslmode=require"
DATABASE_URL="postgresql://user:pass@azure-postgres.postgres.database.azure.com:5432/db?sslmode=require"
DATABASE_URL="postgresql://user:pass@gcp-sql-instance:5432/db?sslmode=require"
```

## 🛠️ MCP Tools & Capabilities

### Available Tools

#### 1. Query Tool
Execute read-only SQL queries with parameter binding.

```json
{
  "name": "query",
  "arguments": {
    "sql": "SELECT id, name, email FROM users WHERE created_at > $1 LIMIT $2",
    "params": ["2025-01-01", 10]
  }
}
```

**Features:**
- ✅ Parameterized queries (SQL injection protection)
- ✅ Read-only transaction enforcement
- ✅ Query timeout protection
- ✅ Result set formatting
- ✅ Error handling and reporting

#### 2. Describe Table Tool
Get comprehensive table schema information.

```json
{
  "name": "describe_table",
  "arguments": {
    "table_name": "users",
    "schema_name": "public"
  }
}
```

**Returns:**
- Column definitions (name, type, nullable, default)
- Primary keys and unique constraints
- Foreign key relationships
- Indexes and their properties
- Table statistics and comments

#### 3. List Tables Tool
Enumerate all accessible tables and views.

```json
{
  "name": "list_tables",
  "arguments": {
    "schema_name": "public"
  }
}
```

**Features:**
- Schema filtering
- Table metadata (owner, size, row count estimates)
- Access permissions validation
- View identification

#### 4. Analyze Query Tool
Analyze query execution plans without executing queries.

```json
{
  "name": "analyze_query",
  "arguments": {
    "sql": "SELECT u.*, COUNT(o.id) FROM users u LEFT JOIN orders o ON u.id = o.user_id GROUP BY u.id"
  }
}
```

**Provides:**
- Execution plan analysis
- Cost estimates
- Index usage recommendations
- Join strategy insights
- Performance optimization suggestions

### Resource Discovery

The PostgreSQL MCP Server automatically discovers and exposes database resources:

#### Table Resources
```
postgres://table/public/users
postgres://table/public/orders
postgres://table/public/products
```

#### Schema Resources
```
postgres://schema/public
postgres://schema/app_data
postgres://schema/analytics
```

#### Function Resources
```
postgres://function/public/get_user_stats
postgres://function/public/calculate_totals
```

## 🔒 Security & Best Practices

### Security Features

#### Read-Only Mode
- **Transaction Isolation**: All queries execute in `READ ONLY` transactions
- **SQL Validation**: AST parsing blocks non-SELECT statements
- **Connection Security**: Dedicated read-only database users
- **Parameter Binding**: Prevents SQL injection attacks

#### Access Control
```sql
-- Create dedicated read-only user
CREATE USER mcp_readonly WITH PASSWORD 'secure_password';

-- Grant minimal required permissions
GRANT CONNECT ON DATABASE myapp TO mcp_readonly;
GRANT USAGE ON SCHEMA public TO mcp_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO mcp_readonly;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO mcp_readonly;

-- Grant access to views and functions
GRANT SELECT ON ALL TABLES IN SCHEMA public TO mcp_readonly;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO mcp_readonly;

-- Revoke dangerous permissions
REVOKE CREATE ON SCHEMA public FROM mcp_readonly;
REVOKE ALL ON DATABASE myapp FROM mcp_readonly;
```

#### Network Security
```yaml
# Docker network isolation
networks:
  mcp-network:
    driver: bridge
    internal: true  # No external access

# Kubernetes network policies
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: postgres-mcp-netpol
spec:
  podSelector:
    matchLabels:
      app: postgres-mcp-server
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: tas-mcp-federation
    ports:
    - protocol: TCP
      port: 3401
```

### Best Practices

#### Database Configuration
1. **Use dedicated read-only users**
2. **Enable SSL/TLS connections**
3. **Configure connection limits**
4. **Monitor query performance**
5. **Regular security audits**

#### Application Configuration
1. **Set appropriate query timeouts**
2. **Limit connection pool size**
3. **Enable comprehensive logging**
4. **Monitor resource usage**
5. **Implement health checks**

#### Production Deployment
1. **Use secrets management**
2. **Enable monitoring and alerting**
3. **Configure backup strategies**
4. **Implement disaster recovery**
5. **Regular security updates**

## 🚀 Deployment Options

### Docker Compose

#### Basic Deployment
```yaml
version: '3.8'
services:
  postgres-mcp:
    image: tas-mcp/postgres-mcp-server:1.0.0
    environment:
      DATABASE_URL: postgresql://user:pass@db:5432/myapp
      READ_ONLY: true
      MAX_CONNECTIONS: 10
    ports:
      - "3401:3401"
    depends_on:
      - database
```

#### Production Deployment
```yaml
version: '3.8'
services:
  postgres-mcp:
    image: tas-mcp/postgres-mcp-server:1.0.0
    environment:
      DATABASE_URL_FILE: /run/secrets/database_url
      READ_ONLY: true
      MAX_CONNECTIONS: 5
      HEALTH_CHECK_ENABLED: true
      LOG_LEVEL: warn
    secrets:
      - database_url
    deploy:
      replicas: 3
      restart_policy:
        condition: on-failure
        max_attempts: 3
      resources:
        limits:
          memory: 512M
          cpus: '0.5'
        reservations:
          memory: 256M
          cpus: '0.25'
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3401/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s

secrets:
  database_url:
    external: true
```

### Kubernetes

#### Basic Deployment
```bash
# Deploy PostgreSQL MCP server
kubectl apply -f deployments/k8s/postgres-mcp-secret.yaml
kubectl apply -f deployments/k8s/postgres-mcp-configmap.yaml
kubectl apply -f deployments/k8s/postgres-database-deployment.yaml
kubectl apply -f deployments/k8s/postgres-mcp-deployment.yaml

# Verify deployment
kubectl get pods -n tas-mcp -l app=postgres-mcp-server
kubectl get svc -n tas-mcp -l app=postgres-mcp-server
```

#### Production Deployment
```bash
# Create namespace and RBAC
kubectl create namespace tas-mcp-prod
kubectl apply -f k8s/rbac.yaml

# Deploy with monitoring
kubectl apply -f k8s/postgres-mcp-monitoring.yaml
kubectl apply -f k8s/postgres-mcp-deployment.yaml

# Configure ingress and load balancing
kubectl apply -f k8s/postgres-mcp-ingress.yaml

# Set up backup and disaster recovery
kubectl apply -f k8s/postgres-backup-cronjob.yaml
```

### Cloud Deployments

#### AWS ECS/Fargate
```json
{
  "family": "postgres-mcp-server",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "512",
  "memory": "1024",
  "containerDefinitions": [
    {
      "name": "postgres-mcp",
      "image": "tas-mcp/postgres-mcp-server:1.0.0",
      "portMappings": [
        {
          "containerPort": 3401,
          "protocol": "tcp"
        }
      ],
      "environment": [
        {
          "name": "READ_ONLY",
          "value": "true"
        }
      ],
      "secrets": [
        {
          "name": "DATABASE_URL",
          "valueFrom": "arn:aws:secretsmanager:region:account:secret:postgres-mcp-db-url"
        }
      ],
      "healthCheck": {
        "command": ["CMD-SHELL", "curl -f http://localhost:3401/health || exit 1"],
        "interval": 30,
        "timeout": 5,
        "retries": 3,
        "startPeriod": 60
      },
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/postgres-mcp-server",
          "awslogs-region": "us-west-2",
          "awslogs-stream-prefix": "ecs"
        }
      }
    }
  ]
}
```

#### Google Cloud Run
```yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: postgres-mcp-server
  annotations:
    run.googleapis.com/ingress: internal
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/maxScale: "10"
        run.googleapis.com/execution-environment: gen2
        run.googleapis.com/cpu-throttling: "false"
    spec:
      containerConcurrency: 100
      containers:
      - image: gcr.io/project/postgres-mcp-server:1.0.0
        ports:
        - containerPort: 3401
        env:
        - name: READ_ONLY
          value: "true"
        - name: MAX_CONNECTIONS
          value: "5"
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: postgres-credentials
              key: database-url
        resources:
          limits:
            cpu: 1000m
            memory: 512Mi
          requests:
            cpu: 200m
            memory: 256Mi
        livenessProbe:
          httpGet:
            path: /health
            port: 3401
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 3401
          initialDelaySeconds: 5
          periodSeconds: 5
```

## 📊 Monitoring & Observability

### Health Endpoints

#### Health Check
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
  "database": "connected",
  "connection_pool": {
    "total": 10,
    "active": 2,
    "idle": 8
  },
  "uptime": 3600,
  "memory_usage": {
    "rss": "45.2MB",
    "heap_used": "32.1MB",
    "heap_total": "41.8MB"
  }
}
```

#### Metrics Endpoint
```bash
curl http://localhost:3401/metrics
```

**Prometheus Metrics:**
```
# HELP postgres_mcp_queries_total Total number of queries executed
# TYPE postgres_mcp_queries_total counter
postgres_mcp_queries_total{status="success"} 1234
postgres_mcp_queries_total{status="error"} 56

# HELP postgres_mcp_query_duration_seconds Query execution duration
# TYPE postgres_mcp_query_duration_seconds histogram
postgres_mcp_query_duration_seconds_bucket{le="0.1"} 892
postgres_mcp_query_duration_seconds_bucket{le="0.5"} 1124
postgres_mcp_query_duration_seconds_bucket{le="1.0"} 1267
postgres_mcp_query_duration_seconds_bucket{le="+Inf"} 1290

# HELP postgres_mcp_connections_active Active database connections
# TYPE postgres_mcp_connections_active gauge
postgres_mcp_connections_active 3

# HELP postgres_mcp_memory_usage_bytes Memory usage in bytes
# TYPE postgres_mcp_memory_usage_bytes gauge
postgres_mcp_memory_usage_bytes{type="rss"} 47472640
postgres_mcp_memory_usage_bytes{type="heap_used"} 33685504
```

### Grafana Dashboard

The integration includes a pre-built Grafana dashboard with:

- **Service Health**: Uptime, response time, error rates
- **Database Metrics**: Connection count, query performance, cache hit ratio
- **Resource Usage**: CPU, memory, disk I/O
- **Query Analytics**: Slow queries, query patterns, execution plans
- **Alert Status**: Active alerts and notifications

### Alerting Rules

#### Critical Alerts
```yaml
- alert: PostgresMCPServerDown
  expr: up{job="postgres-mcp-server"} == 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "PostgreSQL MCP Server is down"
    description: "PostgreSQL MCP Server has been down for more than 1 minute"

- alert: PostgresDatabaseUnreachable
  expr: postgres_mcp_database_connected == 0
  for: 30s
  labels:
    severity: critical
  annotations:
    summary: "PostgreSQL database is unreachable"
    description: "PostgreSQL MCP server cannot connect to the database"
```

#### Warning Alerts
```yaml
- alert: PostgresMCPHighMemoryUsage
  expr: (postgres_mcp_memory_usage_bytes{type="rss"} / postgres_mcp_memory_limit_bytes) > 0.85
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "PostgreSQL MCP Server high memory usage"
    description: "Memory usage is above 85% for more than 5 minutes"

- alert: PostgresMCPSlowQueries
  expr: histogram_quantile(0.95, postgres_mcp_query_duration_seconds) > 5
  for: 3m
  labels:
    severity: warning
  annotations:
    summary: "PostgreSQL MCP Server slow queries detected"
    description: "95th percentile query duration is above 5 seconds"
```

## 🚨 Troubleshooting

### Common Issues

#### Connection Problems
```bash
# Test database connectivity
docker exec postgres-mcp-server node -e "
const { Pool } = require('pg');
const pool = new Pool({connectionString: process.env.DATABASE_URL});
pool.connect()
  .then(client => {
    console.log('✅ Database connected successfully');
    client.release();
  })
  .catch(err => console.error('❌ Connection failed:', err.message));
"

# Check PostgreSQL server status
docker exec postgres-database pg_isready -U postgres -d sampledb

# Verify network connectivity
docker exec postgres-mcp-server nc -zv postgres-database 5432
```

#### Permission Issues
```bash
# Check database user permissions
docker exec postgres-database psql -U postgres -c "
SELECT 
  schemaname,
  tablename,
  tableowner,
  hasselect,
  hasinsert,
  hasupdate,
  hasdelete
FROM pg_tables 
WHERE schemaname = 'public';"

# Test query permissions
docker exec postgres-database psql -U readonly_user -d sampledb -c "SELECT 1;"
```

#### Performance Issues
```bash
# Monitor active connections
docker exec postgres-database psql -U postgres -c "
SELECT 
  datname,
  usename,
  state,
  query_start,
  state_change,
  backend_start
FROM pg_stat_activity
WHERE state != 'idle'
ORDER BY query_start;"

# Check slow queries
docker exec postgres-database psql -U postgres -c "
SELECT 
  query,
  calls,
  total_time,
  mean_time,
  min_time,
  max_time
FROM pg_stat_statements
ORDER BY total_time DESC
LIMIT 10;"
```

#### Resource Issues
```bash
# Check container resource usage
docker stats postgres-mcp-server postgres-database

# Monitor disk usage
docker exec postgres-database df -h /var/lib/postgresql/data

# Check PostgreSQL configuration
docker exec postgres-database psql -U postgres -c "SHOW ALL;"
```

### Debug Commands

#### Container Inspection
```bash
# View container logs
docker logs postgres-mcp-server -f --tail 100
docker logs postgres-database -f --tail 100

# Execute interactive shell
docker exec -it postgres-mcp-server sh
docker exec -it postgres-database psql -U postgres -d sampledb

# Inspect container configuration
docker inspect postgres-mcp-server
docker inspect postgres-database
```

#### Service Debugging
```bash
# Test MCP server health
curl -v http://localhost:3401/health

# Check service ports
docker port postgres-mcp-server
docker port postgres-database

# Network troubleshooting
docker network ls
docker network inspect mcp-network
```

#### Database Debugging
```sql
-- Check database statistics
SELECT * FROM pg_stat_database WHERE datname = 'sampledb';

-- Monitor table activity
SELECT * FROM pg_stat_user_tables ORDER BY seq_scan + idx_scan DESC;

-- Check index usage
SELECT 
  schemaname,
  tablename,
  indexname,
  idx_scan,
  idx_tup_read,
  idx_tup_fetch
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;

-- View active queries
SELECT 
  pid,
  usename,
  datname,
  state,
  query_start,
  query
FROM pg_stat_activity
WHERE state != 'idle'
ORDER BY query_start;
```

## 🚀 Advanced Usage

### Custom Database Schemas

#### Extending the Sample Schema
```sql
-- Add custom tables
CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    company_name VARCHAR(255) NOT NULL,
    contact_person VARCHAR(100),
    email VARCHAR(100),
    phone VARCHAR(20),
    address JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create custom views
CREATE VIEW customer_summary AS
SELECT 
    c.id,
    c.company_name,
    c.contact_person,
    COUNT(o.id) as total_orders,
    SUM(o.total_amount) as total_spent,
    MAX(o.created_at) as last_order_date
FROM customers c
LEFT JOIN orders o ON c.id = o.customer_id
GROUP BY c.id, c.company_name, c.contact_person;

-- Add custom functions
CREATE OR REPLACE FUNCTION get_customer_analytics(customer_id_param INTEGER)
RETURNS TABLE (
    customer_name VARCHAR,
    total_orders INTEGER,
    total_spent DECIMAL,
    avg_order_value DECIMAL,
    last_order_date TIMESTAMP
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        c.company_name,
        COUNT(o.id)::INTEGER,
        COALESCE(SUM(o.total_amount), 0),
        COALESCE(AVG(o.total_amount), 0),
        MAX(o.created_at)
    FROM customers c
    LEFT JOIN orders o ON c.id = o.customer_id AND o.status != 'cancelled'
    WHERE c.id = customer_id_param
    GROUP BY c.id, c.company_name;
END;
$$ LANGUAGE plpgsql;
```

### Custom MCP Tools

#### Extending the MCP Server
```javascript
// Add custom tool for customer analytics
server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;
  
  switch (name) {
    // ... existing tools ...
    
    case 'customer_analytics':
      return this.handleCustomerAnalytics(args);
    
    case 'revenue_report':
      return this.handleRevenueReport(args);
    
    default:
      throw new Error(`Unknown tool: ${name}`);
  }
});

// Implementation
async handleCustomerAnalytics(args) {
  const { customer_id, date_range } = args;
  
  const client = await this.pool.connect();
  try {
    const result = await client.query(`
      SELECT * FROM get_customer_analytics($1)
    `, [customer_id]);
    
    return {
      content: [{
        type: 'text',
        text: JSON.stringify({
          customer_id,
          analytics: result.rows[0],
          generated_at: new Date().toISOString()
        }, null, 2)
      }]
    };
  } finally {
    client.release();
  }
}
```

### Integration Patterns

#### Real-time Data Processing
```javascript
// Stream processing integration
const streamProcessor = new EventEmitter();

streamProcessor.on('user_registration', async (userData) => {
  const response = await manager.InvokeServer(ctx, 'postgres-mcp-server-v1.0.0', {
    method: 'query',
    params: {
      sql: 'INSERT INTO user_events (user_id, event_type, event_data) VALUES ($1, $2, $3)',
      params: [userData.id, 'registration', JSON.stringify(userData)]
    }
  });
});
```

#### Batch Analytics
```javascript
// Scheduled analytics processing
const analyticsScheduler = new CronJob('0 0 * * *', async () => {
  const queries = [
    'SELECT * FROM daily_sales_summary()',
    'SELECT * FROM user_engagement_metrics()',
    'SELECT * FROM product_performance_report()'
  ];
  
  for (const sql of queries) {
    const response = await manager.InvokeServer(ctx, 'postgres-mcp-server-v1.0.0', {
      method: 'query',
      params: { sql }
    });
    
    // Process and store results
    await processAnalyticsResults(response.result);
  }
});
```

#### Multi-Database Federation
```javascript
// Configure multiple PostgreSQL instances
const databases = [
  {
    id: 'postgres-main-v1.0.0',
    name: 'Main Application Database',
    endpoint: 'http://postgres-main:3401'
  },
  {
    id: 'postgres-analytics-v1.0.0', 
    name: 'Analytics Database',
    endpoint: 'http://postgres-analytics:3401'
  },
  {
    id: 'postgres-audit-v1.0.0',
    name: 'Audit Database',
    endpoint: 'http://postgres-audit:3401'
  }
];

// Register all databases
for (const db of databases) {
  await manager.RegisterServer({
    ...db,
    protocol: federation.ProtocolHTTP,
    capabilities: ['query', 'describe_table', 'list_tables', 'analyze_query']
  });
}
```

---

**Next Steps:**
- Explore [Git MCP Integration](GIT_MCP_INTEGRATION.md)
- Review [TAS MCP Federation Architecture](ARCHITECTURE.md)  
- Check [API Documentation](API.md)
- See [Deployment Guide](DEPLOYMENT.md)