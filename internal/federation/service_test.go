package federation

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestGenericService(t *testing.T) {
	logger := zap.NewNop()
	bridge := &MockProtocolBridge{}
	server := createTestServer()

	service, err := NewGenericService(server, logger, bridge)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Test interface methods
	if service.ID() != server.ID {
		t.Errorf("Expected ID %s, got %s", server.ID, service.ID())
	}

	if service.Name() != server.Name {
		t.Errorf("Expected name %s, got %s", server.Name, service.Name())
	}

	if service.Category() != server.Category {
		t.Errorf("Expected category %s, got %s", server.Category, service.Category())
	}

	if service.Status() != server.Status {
		t.Errorf("Expected status %s, got %s", server.Status, service.Status())
	}

	capabilities := service.Capabilities()
	if len(capabilities) != len(server.Capabilities) {
		t.Errorf("Expected %d capabilities, got %d", len(server.Capabilities), len(capabilities))
	}
}

func TestGenericServiceInvoke(t *testing.T) {
	logger := zap.NewNop()
	bridge := &MockProtocolBridge{}
	server := createTestServer()

	service, err := NewGenericService(server, logger, bridge)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Test successful invocation
	request := &MCPRequest{
		ID:     "test-request",
		Method: "test_method",
		Params: map[string]interface{}{
			"param1": "value1",
		},
	}

	response, err := service.Invoke(context.Background(), request)
	if err != nil {
		t.Fatalf("Expected successful invocation, got error: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	if response.ID != request.ID {
		t.Errorf("Expected response ID %s, got %s", request.ID, response.ID)
	}

	// Test nil request
	_, err = service.Invoke(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for nil request")
	}
}

func TestGenericServiceLifecycle(t *testing.T) {
	logger := zap.NewNop()
	bridge := &MockProtocolBridge{}
	server := createTestServer()

	service, err := NewGenericService(server, logger, bridge)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// Test start
	err = service.Start(ctx)
	if err != nil {
		t.Fatalf("Expected successful start, got error: %v", err)
	}

	// Test health check
	err = service.Health(ctx)
	if err != nil {
		// Health check might fail for mock, that's okay
		t.Logf("Health check failed (expected for mock): %v", err)
	}

	// Test stop
	err = service.Stop(ctx)
	if err != nil {
		t.Fatalf("Expected successful stop, got error: %v", err)
	}
}

func TestHTTPClient(t *testing.T) {
	logger := zap.NewNop()
	server := createTestServer()

	client, err := NewHTTPClient(server, logger)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client, got nil")
	}

	if client.server != server {
		t.Error("Expected server to be set")
	}

	if client.httpClient == nil {
		t.Error("Expected HTTP client to be initialized")
	}
}

func TestHTTPClientCall(t *testing.T) {
	logger := zap.NewNop()
	server := createTestServer()

	client, err := NewHTTPClient(server, logger)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Test call (mock implementation)
	result, err := client.Call(context.Background(), "test_method", map[string]interface{}{
		"param1": "value1",
	})

	if err != nil {
		t.Fatalf("Expected successful call, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Check result structure (from mock implementation)
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be map")
	}

	if resultMap["status"] != testStatusSuccess {
		t.Errorf("Expected status 'success', got %v", resultMap["status"])
	}

	if resultMap["method"] != "test_method" {
		t.Errorf("Expected method 'test_method', got %v", resultMap["method"])
	}
}

func TestHTTPClientLifecycle(t *testing.T) {
	logger := zap.NewNop()
	server := createTestServer()

	client, err := NewHTTPClient(server, logger)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	ctx := context.Background()

	// Test start
	err = client.Start(ctx)
	if err != nil {
		t.Fatalf("Expected successful start, got error: %v", err)
	}

	// Test stop
	err = client.Stop(ctx)
	if err != nil {
		t.Fatalf("Expected successful stop, got error: %v", err)
	}
}

func TestGRPCClient(t *testing.T) {
	logger := zap.NewNop()
	server := createTestServer()
	server.Protocol = ProtocolGRPC
	server.Endpoint = testGRPCEndpoint

	client, err := NewGRPCClient(server, logger)
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client, got nil")
	}

	if client.server != server {
		t.Error("Expected server to be set")
	}
}

func TestGRPCClientCall(t *testing.T) {
	logger := zap.NewNop()
	server := createTestServer()
	server.Protocol = ProtocolGRPC
	server.Endpoint = testGRPCEndpoint

	client, err := NewGRPCClient(server, logger)
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}

	// Test call (mock implementation)
	result, err := client.Call(context.Background(), "test_method", map[string]interface{}{
		"param1": "value1",
	})

	if err != nil {
		t.Fatalf("Expected successful call, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Check result structure (from mock implementation)
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be map")
	}

	if resultMap["status"] != testStatusSuccess {
		t.Errorf("Expected status 'success', got %v", resultMap["status"])
	}
}

func TestUnsupportedProtocol(t *testing.T) {
	logger := zap.NewNop()
	bridge := &MockProtocolBridge{}
	server := createTestServer()
	server.Protocol = ProtocolSSE // Unsupported protocol

	_, err := NewGenericService(server, logger, bridge)
	if err == nil {
		t.Error("Expected error for unsupported protocol")
	}
}
