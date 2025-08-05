package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	"go.uber.org/zap"
)

// Example: Integration with PostgreSQL MCP Server
func main() {
	// Initialize logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create federation manager (you would normally get this from your app)
	manager := federation.NewManagerWithDefaults(logger)

	// Configure PostgreSQL MCP Server
	postgresServer := &federation.MCPServer{
		ID:          "postgres-mcp-server-v1.0.0",
		Name:        "PostgreSQL MCP Server v1.0.0",
		Description: "Advanced PostgreSQL database integration server with read-only access, schema inspection, and query analysis",
		Version:     "1.0.0",
		Category:    "database",
		Endpoint:    "http://localhost:3401", // Local PostgreSQL MCP server health endpoint
		Protocol:    federation.ProtocolHTTP,
		Auth: federation.AuthConfig{
			Type:   federation.AuthNone,
			Config: map[string]string{},
		},
		Capabilities: []string{
			"query",
			"describe_table",
			"list_tables",
			"analyze_query",
			"schema_inspection",
			"connection_health",
		},
		Tags: []string{"nodejs", "postgresql", "database", "sql", "read-only", "federation"},
		HealthCheck: federation.HealthCheckConfig{
			Enabled:            true,
			Interval:           30 * time.Second,
			Timeout:            10 * time.Second,
			HealthyThreshold:   3,
			UnhealthyThreshold: 2,
			Path:               "/health",
		},
		Status:    federation.StatusUnknown,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Start the federation manager
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		log.Fatalf("Failed to start federation manager: %v", err)
	}

	// Register the PostgreSQL MCP server
	if err := manager.RegisterServer(postgresServer); err != nil {
		log.Fatalf("Failed to register PostgreSQL MCP server: %v", err)
	}

	fmt.Printf("✅ Registered PostgreSQL MCP server: %s\n", postgresServer.ID)

	// Test health check
	if err := manager.CheckHealth(ctx, postgresServer.ID); err != nil {
		fmt.Printf("⚠️ Health check failed: %v\n", err)
	} else {
		fmt.Printf("✅ PostgreSQL MCP server is healthy\n")
	}

	// Example: List all tables in the database
	listTablesRequest := &federation.MCPRequest{
		ID:     "list-tables-request",
		Method: "list_tables",
		Params: map[string]interface{}{
			"schema_name": "public",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err := manager.InvokeServer(ctx, postgresServer.ID, listTablesRequest)
	if err != nil {
		fmt.Printf("❌ Failed to list tables: %v\n", err)
	} else {
		fmt.Printf("✅ Tables list response: %+v\n", response.Result)
	}

	// Example: Describe a specific table
	describeTableRequest := &federation.MCPRequest{
		ID:     "describe-table-request",
		Method: "describe_table",
		Params: map[string]interface{}{
			"table_name":  "users",
			"schema_name": "public",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, postgresServer.ID, describeTableRequest)
	if err != nil {
		fmt.Printf("❌ Failed to describe table: %v\n", err)
	} else {
		fmt.Printf("✅ Table description response: %+v\n", response.Result)
	}

	// Example: Execute a read-only query
	queryRequest := &federation.MCPRequest{
		ID:     "query-request",
		Method: "query",
		Params: map[string]interface{}{
			"sql":    "SELECT id, username, email, created_at FROM users WHERE is_active = $1 ORDER BY created_at DESC LIMIT 5",
			"params": []interface{}{true},
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, postgresServer.ID, queryRequest)
	if err != nil {
		fmt.Printf("❌ Failed to execute query: %v\n", err)
	} else {
		fmt.Printf("✅ Query response: %+v\n", response.Result)
	}

	// Example: Analyze a complex query
	analyzeQueryRequest := &federation.MCPRequest{
		ID:     "analyze-query-request",
		Method: "analyze_query",
		Params: map[string]interface{}{
			"sql": `
				SELECT u.username, u.email, COUNT(o.id) as order_count, SUM(o.total_amount) as total_spent
				FROM users u
				LEFT JOIN orders o ON u.id = o.user_id AND o.status != 'cancelled'
				WHERE u.is_active = true
				GROUP BY u.id, u.username, u.email
				HAVING COUNT(o.id) > 0
				ORDER BY total_spent DESC
				LIMIT 10
			`,
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, postgresServer.ID, analyzeQueryRequest)
	if err != nil {
		fmt.Printf("❌ Failed to analyze query: %v\n", err)
	} else {
		fmt.Printf("✅ Query analysis response: %+v\n", response.Result)
	}

	// Example: Execute aggregate queries
	aggregateQueries := []struct {
		name        string
		description string
		sql         string
	}{
		{
			name:        "user-stats",
			description: "Get user statistics",
			sql:         "SELECT COUNT(*) as total_users, COUNT(*) FILTER (WHERE is_active = true) as active_users FROM users",
		},
		{
			name:        "product-stats",
			description: "Get product statistics",
			sql:         "SELECT COUNT(*) as total_products, SUM(stock_quantity) as total_stock, AVG(price) as avg_price FROM products WHERE is_active = true",
		},
		{
			name:        "order-stats",
			description: "Get order statistics",
			sql:         "SELECT COUNT(*) as total_orders, SUM(total_amount) as total_revenue, AVG(total_amount) as avg_order_value FROM orders WHERE status != 'cancelled'",
		},
		{
			name:        "top-products",
			description: "Get top selling products",
			sql: `
				SELECT p.name, p.sku, SUM(oi.quantity) as total_sold, SUM(oi.total_price) as revenue
				FROM products p
				JOIN order_items oi ON p.id = oi.product_id
				JOIN orders o ON oi.order_id = o.id
				WHERE o.status != 'cancelled'
				GROUP BY p.id, p.name, p.sku
				ORDER BY total_sold DESC
				LIMIT 5
			`,
		},
	}

	fmt.Printf("\n📊 Running aggregate queries...\n")
	for _, query := range aggregateQueries {
		queryReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("%s-request", query.name),
			Method: "query",
			Params: map[string]interface{}{
				"sql": query.sql,
			},
			Metadata: map[string]string{
				"user":        "tas-mcp-user",
				"description": query.description,
			},
		}

		response, err := manager.InvokeServer(ctx, postgresServer.ID, queryReq)
		if err != nil {
			fmt.Printf("❌ Failed to execute %s: %v\n", query.name, err)
		} else {
			fmt.Printf("✅ %s result: %+v\n", query.description, response.Result)
		}
	}

	// Example: Test database views
	viewQueries := []struct {
		name string
		view string
		sql  string
	}{
		{
			name: "product-summary",
			view: "product_summary",
			sql:  "SELECT name, category_name, price, avg_rating, review_count FROM product_summary WHERE avg_rating > 4 ORDER BY avg_rating DESC LIMIT 5",
		},
		{
			name: "order-summary",
			view: "order_summary",
			sql:  "SELECT order_number, status, total_amount, username, item_count FROM order_summary ORDER BY created_at DESC LIMIT 5",
		},
		{
			name: "user-activity",
			view: "user_activity",
			sql:  "SELECT username, total_orders, total_spent, total_reviews FROM user_activity WHERE total_orders > 0 ORDER BY total_spent DESC LIMIT 5",
		},
	}

	fmt.Printf("\n📋 Testing database views...\n")
	for _, viewQuery := range viewQueries {
		queryReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("%s-view-request", viewQuery.name),
			Method: "query",
			Params: map[string]interface{}{
				"sql": viewQuery.sql,
			},
			Metadata: map[string]string{
				"user": "tas-mcp-user",
				"view": viewQuery.view,
			},
		}

		response, err := manager.InvokeServer(ctx, postgresServer.ID, queryReq)
		if err != nil {
			fmt.Printf("❌ Failed to query %s view: %v\n", viewQuery.view, err)
		} else {
			fmt.Printf("✅ %s view result: %+v\n", viewQuery.view, response.Result)
		}
	}

	// Example: Test stored functions
	fmt.Printf("\n⚙️ Testing stored functions...\n")
	functionRequest := &federation.MCPRequest{
		ID:     "function-request",
		Method: "query",
		Params: map[string]interface{}{
			"sql": "SELECT * FROM get_quick_stats()",
		},
		Metadata: map[string]string{
			"user":        "tas-mcp-user",
			"description": "Get quick statistics using stored function",
		},
	}

	response, err = manager.InvokeServer(ctx, postgresServer.ID, functionRequest)
	if err != nil {
		fmt.Printf("❌ Failed to execute stored function: %v\n", err)
	} else {
		fmt.Printf("✅ Quick stats function result: %+v\n", response.Result)
	}

	// List all registered servers
	servers, err := manager.ListServers()
	if err != nil {
		fmt.Printf("❌ Failed to list servers: %v\n", err)
	} else {
		fmt.Printf("📋 Registered servers (%d):\n", len(servers))
		for _, server := range servers {
			fmt.Printf("  - %s (%s) - %s\n", server.Name, server.ID, server.Status)
		}
	}

	// Get health status of all servers
	healthStatus, err := manager.GetHealthStatus()
	if err != nil {
		fmt.Printf("❌ Failed to get health status: %v\n", err)
	} else {
		fmt.Printf("🏥 Health Status:\n")
		for serverID, status := range healthStatus {
			fmt.Printf("  - %s: %s\n", serverID, status)
		}
	}

	// Gracefully stop the manager
	if err := manager.Stop(ctx); err != nil {
		fmt.Printf("⚠️ Error stopping federation manager: %v\n", err)
	}

	fmt.Println("🎯 PostgreSQL MCP integration example completed!")
	fmt.Println("")
	fmt.Println("📖 This example demonstrated:")
	fmt.Println("  ✅ PostgreSQL MCP server registration")
	fmt.Println("  ✅ Health check validation")
	fmt.Println("  ✅ Table listing and schema inspection")
	fmt.Println("  ✅ Read-only query execution")
	fmt.Println("  ✅ Query execution plan analysis")
	fmt.Println("  ✅ Aggregate query processing")
	fmt.Println("  ✅ Database view querying")
	fmt.Println("  ✅ Stored function execution")
	fmt.Println("  ✅ Federation management")
}
