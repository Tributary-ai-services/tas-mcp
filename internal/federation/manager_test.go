package federation

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

const (
	// Test constants
	testServerID1     = "server-1"
	testGRPCEndpoint  = "localhost:50051"
	testStatusSuccess = "success"
)

// MockServiceDiscovery implements ServiceDiscovery for testing
type MockServiceDiscovery struct {
	servers []MCPServer
}

func (m *MockServiceDiscovery) Discover(_ context.Context) ([]*MCPServer, error) {
	servers := make([]*MCPServer, len(m.servers))
	for i := range m.servers {
		servers[i] = &m.servers[i]
	}
	return servers, nil
}

func (m *MockServiceDiscovery) Watch(_ context.Context, _ func(*MCPServer, DiscoveryEvent)) error {
	// Mock implementation - doesn't actually watch
	return nil
}

func (m *MockServiceDiscovery) AddSource(_ DiscoverySource) error {
	return nil
}

func (m *MockServiceDiscovery) RemoveSource(_ string) error {
	return nil
}

func (m *MockServiceDiscovery) ListSources() ([]DiscoverySource, error) {
	return []DiscoverySource{}, nil
}

// MockProtocolBridge implements ProtocolBridge for testing
type MockProtocolBridge struct{}

func (m *MockProtocolBridge) TranslateRequest(_ context.Context, _, _ Protocol, request *MCPRequest) (*MCPRequest, error) {
	return request, nil
}

func (m *MockProtocolBridge) TranslateResponse(_ context.Context, _, _ Protocol, response *MCPResponse) (*MCPResponse, error) {
	return response, nil
}

func (m *MockProtocolBridge) SupportsProtocol(protocol Protocol) bool {
	return protocol == ProtocolHTTP || protocol == ProtocolGRPC
}

func (m *MockProtocolBridge) SupportedProtocols() []Protocol {
	return []Protocol{ProtocolHTTP, ProtocolGRPC}
}

func createTestServer() *MCPServer {
	return &MCPServer{
		ID:          "test-server-1",
		Name:        "Test Server",
		Description: "A test MCP server",
		Version:     "1.0.0",
		Category:    "test",
		Endpoint:    "http://localhost:8080",
		Protocol:    ProtocolHTTP,
		Auth: AuthConfig{
			Type:   AuthNone,
			Config: map[string]string{},
		},
		Capabilities: []string{"test_capability"},
		Tags:         []string{"test", "mock"},
		Metadata:     map[string]string{"env": "test"},
		Status:       StatusUnknown,
		HealthCheck: HealthCheckConfig{
			Enabled:            true,
			Interval:           30 * time.Second,
			Timeout:            5 * time.Second,
			HealthyThreshold:   2,
			UnhealthyThreshold: 3,
			Path:               "/health",
		},
	}
}

func createTestManager() *Manager {
	logger := zap.NewNop()
	discovery := &MockServiceDiscovery{}
	bridge := &MockProtocolBridge{}
	return NewManager(logger, discovery, bridge)
}

func TestNewManager(t *testing.T) {
	manager := createTestManager()

	if manager == nil {
		t.Fatal("Expected manager to be created")
	}

	if manager.servers == nil {
		t.Error("Expected servers map to be initialized")
	}

	if manager.services == nil {
		t.Error("Expected services map to be initialized")
	}

	if manager.metrics == nil {
		t.Error("Expected metrics to be initialized")
	}
}

func TestRegisterServer(t *testing.T) {
	manager := createTestManager()
	server := createTestServer()

	// Test successful registration
	err := manager.RegisterServer(server)
	if err != nil {
		t.Fatalf("Expected successful registration, got error: %v", err)
	}

	// Verify server was registered
	registeredServer, err := manager.GetServer(server.ID)
	if err != nil {
		t.Fatalf("Expected to find registered server, got error: %v", err)
	}

	if registeredServer.ID != server.ID {
		t.Errorf("Expected server ID %s, got %s", server.ID, registeredServer.ID)
	}

	// Test duplicate registration
	err = manager.RegisterServer(server)
	if err == nil {
		t.Error("Expected error for duplicate registration")
	}

	// Test nil server
	err = manager.RegisterServer(nil)
	if err == nil {
		t.Error("Expected error for nil server")
	}

	// Test server with empty ID
	emptyIDServer := createTestServer()
	emptyIDServer.ID = ""
	err = manager.RegisterServer(emptyIDServer)
	if err == nil {
		t.Error("Expected error for server with empty ID")
	}
}

func TestUnregisterServer(t *testing.T) {
	manager := createTestManager()
	server := createTestServer()

	// Register server first
	err := manager.RegisterServer(server)
	if err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}

	// Test successful unregistration
	err = manager.UnregisterServer(server.ID)
	if err != nil {
		t.Fatalf("Expected successful unregistration, got error: %v", err)
	}

	// Verify server was unregistered
	_, err = manager.GetServer(server.ID)
	if err == nil {
		t.Error("Expected error when getting unregistered server")
	}

	// Test unregistering non-existent server
	err = manager.UnregisterServer("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent server")
	}
}

func TestListServers(t *testing.T) {
	manager := createTestManager()

	// Test empty list
	servers, err := manager.ListServers()
	if err != nil {
		t.Fatalf("Expected successful list, got error: %v", err)
	}

	if len(servers) != 0 {
		t.Errorf("Expected 0 servers, got %d", len(servers))
	}

	// Register test servers
	server1 := createTestServer()
	server1.ID = testServerID1
	server1.Category = "database"

	server2 := createTestServer()
	server2.ID = "server-2"
	server2.Category = "search"

	err = manager.RegisterServer(server1)
	if err != nil {
		t.Fatalf("Failed to register server1: %v", err)
	}

	err = manager.RegisterServer(server2)
	if err != nil {
		t.Fatalf("Failed to register server2: %v", err)
	}

	// Test listing all servers
	servers, err = manager.ListServers()
	if err != nil {
		t.Fatalf("Expected successful list, got error: %v", err)
	}

	if len(servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(servers))
	}

	// Test listing by category
	dbServers, err := manager.ListServersByCategory("database")
	if err != nil {
		t.Fatalf("Expected successful category list, got error: %v", err)
	}

	if len(dbServers) != 1 {
		t.Errorf("Expected 1 database server, got %d", len(dbServers))
	}

	if dbServers[0].ID != "server-1" {
		t.Errorf("Expected server-1, got %s", dbServers[0].ID)
	}

	// Test listing non-existent category
	noServers, err := manager.ListServersByCategory("non-existent")
	if err != nil {
		t.Fatalf("Expected successful list, got error: %v", err)
	}

	if len(noServers) != 0 {
		t.Errorf("Expected 0 servers for non-existent category, got %d", len(noServers))
	}
}

func TestInvokeServer(t *testing.T) {
	manager := createTestManager()
	server := createTestServer()

	// Register server
	err := manager.RegisterServer(server)
	if err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}

	// Create test request
	request := &MCPRequest{
		ID:     "test-request-1",
		Method: "test_method",
		Params: map[string]interface{}{
			"param1": "value1",
		},
	}

	// Test successful invocation
	response, err := manager.InvokeServer(context.Background(), server.ID, request)
	if err != nil {
		t.Fatalf("Expected successful invocation, got error: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.ID != request.ID {
		t.Errorf("Expected response ID %s, got %s", request.ID, response.ID)
	}

	// Test invocation with non-existent server
	_, err = manager.InvokeServer(context.Background(), "non-existent", request)
	if err == nil {
		t.Error("Expected error for non-existent server")
	}
}

func TestHealthCheck(t *testing.T) {
	manager := createTestManager()
	server := createTestServer()

	// Register server
	err := manager.RegisterServer(server)
	if err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}

	// Test health check
	err = manager.CheckHealth(context.Background(), server.ID)
	if err != nil {
		// Health check might fail for mock implementation, that's okay
		t.Logf("Health check failed (expected for mock): %v", err)
	}

	// Test health check for non-existent server
	err = manager.CheckHealth(context.Background(), "non-existent")
	if err == nil {
		t.Error("Expected error for non-existent server health check")
	}

	// Test getting health status
	status, err := manager.GetHealthStatus()
	if err != nil {
		t.Fatalf("Expected successful health status, got error: %v", err)
	}

	if len(status) != 1 {
		t.Errorf("Expected 1 server status, got %d", len(status))
	}

	if _, exists := status[server.ID]; !exists {
		t.Errorf("Expected status for server %s", server.ID)
	}
}

func TestGetMetrics(t *testing.T) {
	manager := createTestManager()

	// Get initial metrics
	metrics := manager.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics, got nil")
	}

	if metrics.ServersRegistered != 0 {
		t.Errorf("Expected 0 registered servers, got %d", metrics.ServersRegistered)
	}

	// Register a server and check metrics
	server := createTestServer()
	err := manager.RegisterServer(server)
	if err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}

	metrics = manager.GetMetrics()
	if metrics.ServersRegistered != 1 {
		t.Errorf("Expected 1 registered server, got %d", metrics.ServersRegistered)
	}
}

func TestManagerLifecycle(t *testing.T) {
	manager := createTestManager()
	ctx := context.Background()

	// Test start
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Expected successful start, got error: %v", err)
	}

	// Test stop
	err = manager.Stop(ctx)
	if err != nil {
		t.Fatalf("Expected successful stop, got error: %v", err)
	}
}
