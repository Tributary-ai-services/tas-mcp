package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	"go.uber.org/zap"
)

// Example: Integration with Git MCP Server
func main() {
	// Initialize logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create federation manager (you would normally get this from your app)
	manager := federation.NewManagerWithDefaults(logger)

	// Configure Git MCP Server
	gitServer := &federation.MCPServer{
		ID:          "git-mcp-server",
		Name:        "Git MCP Server",
		Description: "Official Git repository interaction and automation server from Model Context Protocol",
		Version:     "1.0.0",
		Category:    "development-tools",
		Endpoint:    "http://localhost:3000", // Local Git MCP server endpoint
		Protocol:    federation.ProtocolHTTP,
		Auth: federation.AuthConfig{
			Type:   federation.AuthNone,
			Config: map[string]string{},
		},
		Capabilities: []string{
			"git_status",
			"git_diff_unstaged",
			"git_diff_staged",
			"git_commit",
			"git_add",
			"git_reset",
			"git_log",
			"git_create_branch",
			"git_checkout",
		},
		Tags: []string{"python", "git", "repository", "development", "official"},
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

	// Register the Git MCP server
	if err := manager.RegisterServer(gitServer); err != nil {
		log.Fatalf("Failed to register Git MCP server: %v", err)
	}

	fmt.Printf("✅ Registered Git MCP server: %s\n", gitServer.ID)

	// Test health check
	if err := manager.CheckHealth(ctx, gitServer.ID); err != nil {
		fmt.Printf("⚠️ Health check failed: %v\n", err)
	} else {
		fmt.Printf("✅ Git MCP server is healthy\n")
	}

	// Example: Get repository status
	statusRequest := &federation.MCPRequest{
		ID:     "git-status-request",
		Method: "git_status",
		Params: map[string]interface{}{
			"repository": "/path/to/your/repo",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err := manager.InvokeServer(ctx, gitServer.ID, statusRequest)
	if err != nil {
		fmt.Printf("❌ Failed to get git status: %v\n", err)
	} else {
		fmt.Printf("✅ Git status response: %+v\n", response.Result)
	}

	// Example: Create a new branch
	branchRequest := &federation.MCPRequest{
		ID:     "git-branch-request",
		Method: "git_create_branch",
		Params: map[string]interface{}{
			"repository":  "/path/to/your/repo",
			"branch_name": "feature/git-mcp-integration",
		},
		Metadata: map[string]string{
			"user": "tas-mcp-user",
		},
	}

	response, err = manager.InvokeServer(ctx, gitServer.ID, branchRequest)
	if err != nil {
		fmt.Printf("❌ Failed to create branch: %v\n", err)
	} else {
		fmt.Printf("✅ Branch creation response: %+v\n", response.Result)
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

	fmt.Println("🎯 Git MCP integration example completed!")
}
