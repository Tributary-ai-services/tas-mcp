package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	"go.uber.org/zap"
)

// Example: Integration with Apify MCP Server
func main() {
	// Initialize logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create federation manager (you would normally get this from your app)
	manager := federation.NewManagerWithDefaults(logger)

	// Configure Apify MCP Server
	apifyServer := &federation.MCPServer{
		ID:          "apify-mcp-server-v1.0.0",
		Name:        "Apify MCP Server v1.0.0",
		Description: "Comprehensive web scraping and automation server using Apify platform with access to 5,000+ actors",
		Version:     "1.0.0",
		Category:    "web-scraping",
		Endpoint:    "http://localhost:3403", // Local Apify MCP server health endpoint
		Protocol:    federation.ProtocolHTTP,
		Auth: federation.AuthConfig{
			Type:   federation.AuthNone,
			Config: map[string]string{},
		},
		Capabilities: []string{
			"run_actor",
			"get_actor_info",
			"search_actors",
			"get_run_status",
			"get_dataset_items",
			"scrape_url",
		},
		Tags: []string{"nodejs", "apify", "web-scraping", "automation", "data-extraction", "federation"},
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

	// Register the Apify MCP server
	if err := manager.RegisterServer(apifyServer); err != nil {
		log.Fatalf("Failed to register Apify MCP server: %v", err)
	}

	fmt.Printf("✅ Registered Apify MCP server: %s\n", apifyServer.ID)

	// Test health check
	if err := manager.CheckHealth(ctx, apifyServer.ID); err != nil {
		fmt.Printf("⚠️ Health check failed: %v\n", err)
	} else {
		fmt.Printf("✅ Apify MCP server is healthy\n")
	}

	// Example: Search for actors
	searchActorsRequest := &federation.MCPRequest{
		ID:     "search-actors-request",
		Method: "search_actors",
		Params: map[string]interface{}{
			"query":    "instagram scraper",
			"category": "SOCIAL_MEDIA",
			"limit":    5,
			"sort_by":  "popularity",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err := manager.InvokeServer(ctx, apifyServer.ID, searchActorsRequest)
	if err != nil {
		fmt.Printf("❌ Failed to search actors: %v\n", err)
	} else {
		fmt.Printf("✅ Actor search response: %+v\n", response.Result)
	}

	// Example: Get actor information
	getActorInfoRequest := &federation.MCPRequest{
		ID:     "get-actor-info-request",
		Method: "get_actor_info",
		Params: map[string]interface{}{
			"actor_id": "apify/web-scraper",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, apifyServer.ID, getActorInfoRequest)
	if err != nil {
		fmt.Printf("❌ Failed to get actor info: %v\n", err)
	} else {
		fmt.Printf("✅ Actor info response: %+v\n", response.Result)
	}

	// Example: Quick URL scraping
	scrapeUrlRequest := &federation.MCPRequest{
		ID:     "scrape-url-request",
		Method: "scrape_url",
		Params: map[string]interface{}{
			"urls": []string{"https://example.com"},
			"extract_data": map[string]interface{}{
				"title":   "h1",
				"content": ".main-content",
				"links":   "a[href]",
			},
			"javascript_enabled": true,
			"max_pages":          1,
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, apifyServer.ID, scrapeUrlRequest)
	if err != nil {
		fmt.Printf("❌ Failed to scrape URL: %v\n", err)
	} else {
		fmt.Printf("✅ URL scraping response: %+v\n", response.Result)
	}

	// Example: Run a specific actor (requires API token)
	runActorRequest := &federation.MCPRequest{
		ID:     "run-actor-request",
		Method: "run_actor",
		Params: map[string]interface{}{
			"actor_id": "apify/google-search-results-scraper",
			"input": map[string]interface{}{
				"queries":          []string{"web scraping tools", "data extraction"},
				"maxPagesPerQuery": 1,
				"resultsPerPage":   5,
				"countryCode":      "US",
				"languageCode":     "en",
			},
			"memory_mbytes":   512,
			"timeout_secs":    300,
			"wait_for_finish": true,
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, apifyServer.ID, runActorRequest)
	if err != nil {
		fmt.Printf("❌ Failed to run actor (this may require API token): %v\n", err)
	} else {
		fmt.Printf("✅ Actor run response: %+v\n", response.Result)
	}

	// Example: Scraping scenarios for different use cases
	scrapingScenarios := []struct {
		name        string
		description string
		actorQuery  string
		category    string
	}{
		{
			name:        "ecommerce-scraping",
			description: "Find e-commerce scraping actors",
			actorQuery:  "amazon product scraper",
			category:    "E_COMMERCE",
		},
		{
			name:        "social-media-scraping",
			description: "Find social media scraping actors",
			actorQuery:  "linkedin profile scraper",
			category:    "SOCIAL_MEDIA",
		},
		{
			name:        "news-scraping",
			description: "Find news scraping actors",
			actorQuery:  "news article scraper",
			category:    "NEWS",
		},
		{
			name:        "seo-tools",
			description: "Find SEO and search-related actors",
			actorQuery:  "google search results",
			category:    "SEO",
		},
	}

	fmt.Printf("\n🕷️ Running scraping scenario discovery...\n")
	for _, scenario := range scrapingScenarios {
		scenarioReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("%s-search", scenario.name),
			Method: "search_actors",
			Params: map[string]interface{}{
				"query":    scenario.actorQuery,
				"category": scenario.category,
				"limit":    3,
				"sort_by":  "popularity",
			},
			Metadata: map[string]string{
				"user":        "tas-mcp-user",
				"description": scenario.description,
				"scenario":    scenario.name,
			},
		}

		response, err := manager.InvokeServer(ctx, apifyServer.ID, scenarioReq)
		if err != nil {
			fmt.Printf("❌ Failed to search %s actors: %v\n", scenario.name, err)
		} else {
			fmt.Printf("✅ %s actors found: %+v\n", scenario.description, response.Result)
		}
	}

	// Example: Popular actors information gathering
	popularActors := []string{
		"apify/web-scraper",
		"apify/instagram-scraper",
		"apify/amazon-product-scraper",
		"apify/linkedin-company-scraper",
		"apify/google-search-results-scraper",
	}

	fmt.Printf("\n📊 Getting information for popular actors...\n")
	for _, actorId := range popularActors {
		actorInfoReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("info-%s", actorId),
			Method: "get_actor_info",
			Params: map[string]interface{}{
				"actor_id": actorId,
			},
			Metadata: map[string]string{
				"user": "tas-mcp-user",
			},
		}

		response, err := manager.InvokeServer(ctx, apifyServer.ID, actorInfoReq)
		if err != nil {
			fmt.Printf("❌ Failed to get info for %s: %v\n", actorId, err)
		} else {
			fmt.Printf("✅ Info for %s: %+v\n", actorId, response.Result)
		}
	}

	// Example: Multiple URL scraping scenarios
	scrapingUrls := []struct {
		name        string
		description string
		urls        []string
		extractors  map[string]interface{}
	}{
		{
			name:        "news-sites",
			description: "Scrape news article headlines",
			urls:        []string{"https://example-news.com"},
			extractors: map[string]interface{}{
				"headline": "h1, .headline",
				"summary":  ".summary, .lead",
				"author":   ".author, .byline",
			},
		},
		{
			name:        "product-pages",
			description: "Scrape product information",
			urls:        []string{"https://example-shop.com/product"},
			extractors: map[string]interface{}{
				"title":       "h1, .product-title",
				"price":       ".price, .cost",
				"description": ".description",
				"reviews":     ".review-count",
			},
		},
		{
			name:        "blog-posts",
			description: "Scrape blog content",
			urls:        []string{"https://example-blog.com/post"},
			extractors: map[string]interface{}{
				"title":   "h1, .post-title",
				"content": ".post-content, .article-body",
				"tags":    ".tags a, .categories a",
			},
		},
	}

	fmt.Printf("\n📄 Testing multiple URL scraping scenarios...\n")
	for _, scenario := range scrapingUrls {
		urlScrapeReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("scrape-%s", scenario.name),
			Method: "scrape_url",
			Params: map[string]interface{}{
				"urls":               scenario.urls,
				"extract_data":       scenario.extractors,
				"javascript_enabled": true,
				"max_pages":          1,
			},
			Metadata: map[string]string{
				"user":        "tas-mcp-user",
				"description": scenario.description,
				"scenario":    scenario.name,
			},
		}

		response, err := manager.InvokeServer(ctx, apifyServer.ID, urlScrapeReq)
		if err != nil {
			fmt.Printf("❌ Failed to scrape %s: %v\n", scenario.name, err)
		} else {
			fmt.Printf("✅ %s scraping result: %+v\n", scenario.description, response.Result)
		}
	}

	// Example: Actor categories exploration
	categories := []string{
		"E_COMMERCE",
		"SOCIAL_MEDIA",
		"TRAVEL",
		"NEWS",
		"SEO",
		"DEVELOPER_TOOLS",
		"ENTERTAINMENT",
	}

	fmt.Printf("\n📚 Exploring actors by category...\n")
	for _, category := range categories {
		categorySearchReq := &federation.MCPRequest{
			ID:     fmt.Sprintf("category-%s", category),
			Method: "search_actors",
			Params: map[string]interface{}{
				"query":    "",
				"category": category,
				"limit":    3,
				"sort_by":  "popularity",
			},
			Metadata: map[string]string{
				"user":     "tas-mcp-user",
				"category": category,
			},
		}

		response, err := manager.InvokeServer(ctx, apifyServer.ID, categorySearchReq)
		if err != nil {
			fmt.Printf("❌ Failed to search %s category: %v\n", category, err)
		} else {
			fmt.Printf("✅ %s category actors: %+v\n", category, response.Result)
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

	fmt.Println("🎯 Apify MCP integration example completed!")
	fmt.Println("")
	fmt.Println("📖 This example demonstrated:")
	fmt.Println("  ✅ Apify MCP server registration")
	fmt.Println("  ✅ Health check validation")
	fmt.Println("  ✅ Actor discovery and search")
	fmt.Println("  ✅ Actor information retrieval")
	fmt.Println("  ✅ Quick URL scraping")
	fmt.Println("  ✅ Actor execution (with API token)")
	fmt.Println("  ✅ Scraping scenario discovery")
	fmt.Println("  ✅ Popular actors exploration")
	fmt.Println("  ✅ Multiple URL scraping scenarios")
	fmt.Println("  ✅ Category-based actor exploration")
	fmt.Println("  ✅ Federation management")
	fmt.Printf("  🕷️ Access to 5,000+ web scraping and automation actors\n")
}
