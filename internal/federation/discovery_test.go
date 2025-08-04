package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewDiscovery(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	if discovery == nil {
		t.Fatal("Expected discovery instance, got nil")
	}

	if discovery.sources == nil {
		t.Error("Expected sources map to be initialized")
	}

	if discovery.servers == nil {
		t.Error("Expected servers map to be initialized")
	}

	if discovery.watchers == nil {
		t.Error("Expected watchers slice to be initialized")
	}
}

func TestAddRemoveSource(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Test adding source
	source := DiscoverySource{
		ID:       "test-source",
		Type:     SourceStatic,
		Config:   map[string]string{"servers": "[]"},
		Enabled:  true,
		Priority: 1,
	}

	err := discovery.AddSource(source)
	if err != nil {
		t.Fatalf("Expected successful add, got error: %v", err)
	}

	// Test adding source with empty ID
	emptySource := DiscoverySource{
		Type:    SourceStatic,
		Enabled: true,
	}

	err = discovery.AddSource(emptySource)
	if err == nil {
		t.Error("Expected error for source with empty ID")
	}

	// Test listing sources
	sources, err := discovery.ListSources()
	if err != nil {
		t.Fatalf("Expected successful list, got error: %v", err)
	}

	if len(sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(sources))
	}

	if sources[0].ID != "test-source" {
		t.Errorf("Expected source ID 'test-source', got %s", sources[0].ID)
	}

	// Test removing source
	err = discovery.RemoveSource("test-source")
	if err != nil {
		t.Fatalf("Expected successful remove, got error: %v", err)
	}

	// Test removing non-existent source
	err = discovery.RemoveSource("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent source")
	}

	// Verify source was removed
	sources, err = discovery.ListSources()
	if err != nil {
		t.Fatalf("Expected successful list, got error: %v", err)
	}

	if len(sources) != 0 {
		t.Errorf("Expected 0 sources, got %d", len(sources))
	}
}

func TestDiscoverStatic(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Create test servers
	servers := []*MCPServer{
		{
			ID:       "static-server-1",
			Name:     "Static Test Server 1",
			Endpoint: "http://localhost:8080",
			Protocol: ProtocolHTTP,
		},
		{
			ID:       "static-server-2",
			Name:     "Static Test Server 2",
			Endpoint: "http://localhost:8081",
			Protocol: ProtocolHTTP,
		},
	}

	serversJSON, _ := json.Marshal(servers)

	source := DiscoverySource{
		ID:      "static-test",
		Type:    SourceStatic,
		Config:  map[string]string{"servers": string(serversJSON)},
		Enabled: true,
	}

	err := discovery.AddSource(source)
	if err != nil {
		t.Fatalf("Failed to add source: %v", err)
	}

	// Test discovery
	discovered, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Expected successful discovery, got error: %v", err)
	}

	if len(discovered) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(discovered))
	}

	// Check metadata was added
	for _, server := range discovered {
		if server.Metadata["discovery_source"] != "static-test" {
			t.Errorf("Expected discovery_source metadata to be set")
		}
		if server.Metadata["discovery_type"] != string(SourceStatic) {
			t.Errorf("Expected discovery_type metadata to be set")
		}
	}
}

func TestDiscoverRegistry(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Create mock registry server
	servers := []*MCPServer{
		{
			ID:       "registry-server-1",
			Name:     "Registry Test Server",
			Endpoint: "http://example.com:8080",
			Protocol: ProtocolHTTP,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for API key
		if r.Header.Get("X-API-Key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		response := struct {
			Servers []*MCPServer `json:"servers"`
		}{
			Servers: servers,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	source := DiscoverySource{
		ID:   "registry-test",
		Type: SourceRegistry,
		Config: map[string]string{
			"url":     server.URL,
			"api_key": "test-key",
		},
		Enabled: true,
	}

	err := discovery.AddSource(source)
	if err != nil {
		t.Fatalf("Failed to add source: %v", err)
	}

	// Test discovery
	discovered, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Expected successful discovery, got error: %v", err)
	}

	if len(discovered) != 1 {
		t.Errorf("Expected 1 server, got %d", len(discovered))
	}

	// Check metadata was added
	discoveredServer := discovered[0]
	if discoveredServer.Metadata["discovery_source"] != "registry-test" {
		t.Errorf("Expected discovery_source metadata to be set")
	}
	if discoveredServer.Metadata["discovery_type"] != string(SourceRegistry) {
		t.Errorf("Expected discovery_type metadata to be set")
	}
	if discoveredServer.Metadata["registry_url"] == "" {
		t.Errorf("Expected registry_url metadata to be set")
	}
}

func TestDiscoverRegistryError(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Test with invalid URL
	source := DiscoverySource{
		ID:      "registry-error-test",
		Type:    SourceRegistry,
		Config:  map[string]string{"url": "http://non-existent-server:12345"},
		Enabled: true,
	}

	err := discovery.AddSource(source)
	if err != nil {
		t.Fatalf("Failed to add source: %v", err)
	}

	// Discovery should continue despite error from one source
	discovered, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Expected discovery to continue despite source error: %v", err)
	}

	// Should return empty results but not error
	if len(discovered) != 0 {
		t.Errorf("Expected 0 servers from failed source, got %d", len(discovered))
	}
}

func TestDiscoverUnsupportedSource(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Create source with unsupported type
	source := DiscoverySource{
		ID:      "unsupported-test",
		Type:    "unsupported",
		Enabled: true,
	}

	// Should still accept the source
	err := discovery.AddSource(source)
	if err != nil {
		t.Fatalf("Failed to add source: %v", err)
	}

	// Discovery should handle unsupported source gracefully
	discovered, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Expected discovery to handle unsupported source: %v", err)
	}

	if len(discovered) != 0 {
		t.Errorf("Expected 0 servers from unsupported source, got %d", len(discovered))
	}
}

func TestWatch(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Channel to receive events
	events := make(chan DiscoveryEvent, 10)
	servers := make(chan *MCPServer, 10)

	// Add watcher
	err := discovery.Watch(context.Background(), func(server *MCPServer, event DiscoveryEvent) {
		events <- event
		servers <- server
	})
	if err != nil {
		t.Fatalf("Failed to add watcher: %v", err)
	}

	// Add static source with initial servers
	initialServers := []*MCPServer{
		{
			ID:       "watch-server-1",
			Name:     "Watch Test Server 1",
			Endpoint: "http://localhost:8080",
			Protocol: ProtocolHTTP,
		},
	}

	serversJSON, _ := json.Marshal(initialServers)
	source := DiscoverySource{
		ID:      "watch-test",
		Type:    SourceStatic,
		Config:  map[string]string{"servers": string(serversJSON)},
		Enabled: true,
	}

	err = discovery.AddSource(source)
	if err != nil {
		t.Fatalf("Failed to add source: %v", err)
	}

	// Trigger discovery
	_, err = discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	// Wait for events
	select {
	case event := <-events:
		if event != EventServerAdded {
			t.Errorf("Expected EventServerAdded, got %s", event)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for server added event")
	}

	select {
	case server := <-servers:
		if server.ID != "watch-server-1" {
			t.Errorf("Expected server ID 'watch-server-1', got %s", server.ID)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for server")
	}
}

func TestDiscoverDisabledSource(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Add disabled source
	source := DiscoverySource{
		ID:      "disabled-test",
		Type:    SourceStatic,
		Config:  map[string]string{"servers": `[{"id":"disabled-server","name":"Disabled Server"}]`},
		Enabled: false, // Disabled
	}

	err := discovery.AddSource(source)
	if err != nil {
		t.Fatalf("Failed to add source: %v", err)
	}

	// Discovery should skip disabled sources
	discovered, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Expected successful discovery: %v", err)
	}

	if len(discovered) != 0 {
		t.Errorf("Expected 0 servers from disabled source, got %d", len(discovered))
	}
}

func TestStop(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Add a watcher to start periodic discovery
	err := discovery.Watch(context.Background(), func(_ *MCPServer, _ DiscoveryEvent) {})
	if err != nil {
		t.Fatalf("Failed to add watcher: %v", err)
	}

	// Stop should not block
	done := make(chan bool)
	go func() {
		discovery.Stop()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Error("Stop operation timed out")
	}
}

func TestPlaceholderImplementations(t *testing.T) {
	logger := zap.NewNop()
	discovery := NewDiscovery(logger)

	// Test placeholder implementations don't crash
	placeholderSources := []DiscoverySource{
		{ID: "k8s-test", Type: SourceKubernetes, Enabled: true},
		{ID: "consul-test", Type: SourceConsul, Enabled: true},
		{ID: "etcd-test", Type: SourceEtcd, Enabled: true},
		{ID: "dns-test", Type: SourceDNS, Enabled: true},
	}

	for _, source := range placeholderSources {
		err := discovery.AddSource(source)
		if err != nil {
			t.Fatalf("Failed to add %s source: %v", source.Type, err)
		}
	}

	// Should complete without error
	discovered, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Expected successful discovery with placeholders: %v", err)
	}

	// Placeholders should return empty results
	if len(discovered) != 0 {
		t.Errorf("Expected 0 servers from placeholder implementations, got %d", len(discovered))
	}
}
