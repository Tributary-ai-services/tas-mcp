package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	"go.uber.org/zap"
)

// Example: Integration with DuckDuckGo MCP Server
func main() {
	// Initialize logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create federation manager (you would normally get this from your app)
	manager := federation.NewManagerWithDefaults(logger)

	// Configure DuckDuckGo MCP Server
	duckduckgoServer := &federation.MCPServer{
		ID:          "duckduckgo-mcp-server-v1.0.0",
		Name:        "DuckDuckGo MCP Server v1.0.0",
		Description: "Privacy-focused web search server using DuckDuckGo with content extraction and image search capabilities",
		Version:     "1.0.0",
		Category:    "search",
		Endpoint:    "http://localhost:3402", // Local DuckDuckGo MCP server health endpoint
		Protocol:    federation.ProtocolHTTP,
		Auth: federation.AuthConfig{
			Type:   federation.AuthNone,
			Config: map[string]string{},
		},
		Capabilities: []string{
			"search",
			"search_news",
			"search_images",
			"fetch_content",
		},
		Tags: []string{"nodejs", "duckduckgo", "search", "privacy", "web-scraping", "federation"},
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

	// Register the DuckDuckGo MCP server
	if err := manager.RegisterServer(duckduckgoServer); err != nil {
		log.Fatalf("Failed to register DuckDuckGo MCP server: %v", err)
	}

	fmt.Printf("✅ Registered DuckDuckGo MCP server: %s\n", duckduckgoServer.ID)

	// Test health check
	if err := manager.CheckHealth(ctx, duckduckgoServer.ID); err != nil {
		fmt.Printf("⚠️ Health check failed: %v\n", err)
	} else {
		fmt.Printf("✅ DuckDuckGo MCP server is healthy\n")
	}

	// Example: Perform a web search
	searchRequest := &federation.MCPRequest{
		ID:     "web-search-request",
		Method: "search",
		Params: map[string]interface{}{
			"query":       "artificial intelligence trends 2025",
			"max_results": 5,
			"safe_search": "moderate",
			"region":      "us-en",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err := manager.InvokeServer(ctx, duckduckgoServer.ID, searchRequest)
	if err != nil {
		fmt.Printf("❌ Failed to perform web search: %v\n", err)
	} else {
		fmt.Printf("✅ Web search response: %+v\n", response.Result)
	}

	// Example: Search for news
	newsSearchRequest := &federation.MCPRequest{
		ID:     "news-search-request",
		Method: "search_news",
		Params: map[string]interface{}{
			"query":       "climate change technology",
			"max_results": 3,
			"region":      "us-en",
			"time_filter": "week",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, duckduckgoServer.ID, newsSearchRequest)
	if err != nil {
		fmt.Printf("❌ Failed to search news: %v\n", err)
	} else {
		fmt.Printf("✅ News search response: %+v\n", response.Result)
	}

	// Example: Search for images
	imageSearchRequest := &federation.MCPRequest{
		ID:     "image-search-request",
		Method: "search_images",
		Params: map[string]interface{}{
			"query":       "renewable energy technology",
			"max_results": 5,
			"safe_search": "moderate",
			"size":        "medium",
			"color":       "color",
			"type":        "photo",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, duckduckgoServer.ID, imageSearchRequest)
	if err != nil {
		fmt.Printf("❌ Failed to search images: %v\n", err)
	} else {
		fmt.Printf("✅ Image search response: %+v\n", response.Result)
	}

	// Example: Fetch content from a webpage
	contentFetchRequest := &federation.MCPRequest{
		ID:     "content-fetch-request",
		Method: "fetch_content",
		Params: map[string]interface{}{
			"url":                "https://example.com/article",
			"extract_text":       true,
			"max_content_length": 2000,
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, duckduckgoServer.ID, contentFetchRequest)
	if err != nil {
		fmt.Printf("❌ Failed to fetch content: %v\n", err)
	} else {
		fmt.Printf("✅ Content fetch response: %+v\n", response.Result)
	}

	// Example: Advanced search scenarios
	searchScenarios := []struct {
		name        string
		description string
		method      string
		params      map[string]interface{}
	}{
		{
			name:        "tech-trends",
			description: "Search for technology trends",
			method:      "search",
			params: map[string]interface{}{
				"query":       "machine learning breakthrough 2025",
				"max_results": 3,
				"safe_search": "moderate",
			},
		},
		{
			name:        "recent-news",
			description: "Get recent tech news",
			method:      "search_news",
			params: map[string]interface{}{
				"query":       "artificial intelligence",
				"max_results": 5,
				"time_filter": "day",
			},
		},
		{
			name:        "research-images",
			description: "Find research-related images",
			method:      "search_images",
			params: map[string]interface{}{
				"query":       "data visualization graphs",
				"max_results": 8,
				"size":        "large",
				"type":        "photo",
			},
		},
	}

	fmt.Printf("\n🔍 Running advanced search scenarios...\n")
	for _, scenario := range searchScenarios {
		scenarioReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("%s-request", scenario.name),
			Method: scenario.method,
			Params: scenario.params,
			Metadata: map[string]string{
				"user":        "tas-mcp-user",
				"description": scenario.description,
			},
		}

		response, err := manager.InvokeServer(ctx, duckduckgoServer.ID, scenarioReq)
		if err != nil {
			fmt.Printf("❌ Failed to execute %s: %v\n", scenario.name, err)
		} else {
			fmt.Printf("✅ %s result: %+v\n", scenario.description, response.Result)
		}
	}

	// Example: Privacy-focused search patterns
	privacySearches := []struct {
		query    string
		category string
	}{
		{"privacy tools software", "Privacy Software"},
		{"encrypted messaging apps", "Security Tools"},
		{"anonymous browsing techniques", "Privacy Techniques"},
		{"data protection regulations", "Privacy Law"},
	}

	fmt.Printf("\n🔒 Testing privacy-focused searches...\n")
	for _, privacySearch := range privacySearches {
		privacyReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("privacy-search-%s", privacySearch.category),
			Method: "search",
			Params: map[string]interface{}{
				"query":       privacySearch.query,
				"max_results": 3,
				"safe_search": "strict",
				"region":      "us-en",
			},
			Metadata: map[string]string{
				"user":     "tas-mcp-user",
				"category": privacySearch.category,
			},
		}

		response, err := manager.InvokeServer(ctx, duckduckgoServer.ID, privacyReq)
		if err != nil {
			fmt.Printf("❌ Failed to search for %s: %v\n", privacySearch.category, err)
		} else {
			fmt.Printf("✅ %s search result: %+v\n", privacySearch.category, response.Result)
		}
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

	fmt.Println("🎯 DuckDuckGo MCP integration example completed!")
	fmt.Println("")
	fmt.Println("📖 This example demonstrated:")
	fmt.Println("  ✅ DuckDuckGo MCP server registration")
	fmt.Println("  ✅ Health check validation")
	fmt.Println("  ✅ Privacy-focused web search")
	fmt.Println("  ✅ News search with time filtering")
	fmt.Println("  ✅ Image search with advanced filters")
	fmt.Println("  ✅ Webpage content extraction")
	fmt.Println("  ✅ Advanced search scenarios")
	fmt.Println("  ✅ Privacy-focused search patterns")
	fmt.Println("  ✅ Federation management")
	fmt.Printf("  🔒 Privacy benefits: No tracking, no stored history, anonymous search\n")
}
