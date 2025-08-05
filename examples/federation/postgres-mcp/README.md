# PostgreSQL MCP Integration Example

This example demonstrates how to integrate with the PostgreSQL MCP Server for secure database operations.

## Features Demonstrated

- Read-only SQL query execution
- Database schema inspection
- Table metadata analysis
- Query performance analysis
- Connection health monitoring

## Running the Example

```bash
cd examples/federation/postgres-mcp
go mod tidy
go run main.go
```

## Prerequisites

- PostgreSQL MCP Server running on localhost:3401
- TAS MCP Federation server running on localhost:8080
- PostgreSQL database with sample data

## Database Capabilities

- **query** - Execute SELECT queries with performance analysis
- **describe_table** - Comprehensive table metadata and schema inspection
- **list_tables** - Schema exploration with filtering options
- **analyze_query** - Query execution plan analysis
- **schema_inspection** - Database structure analysis
- **connection_health** - Connection pooling and health monitoring

## Security Features

- Read-only access enforced
- SQL injection protection
- Connection pooling
- Query validation
- Performance monitoring