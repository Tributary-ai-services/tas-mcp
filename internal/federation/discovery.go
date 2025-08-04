// Package federation provides service discovery for MCP servers
package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// DiscoveryInterval is the default interval for periodic discovery
	DiscoveryInterval = 60 * time.Second
	// HTTPClientTimeout is the timeout for HTTP discovery requests
	HTTPClientTimeout = 30 * time.Second
)

// Discovery implements ServiceDiscovery interface
type Discovery struct {
	logger    *zap.Logger
	sources   map[string]DiscoverySource
	watchers  []func(*MCPServer, DiscoveryEvent)
	servers   map[string]*MCPServer
	mu        sync.RWMutex
	watchCtx  context.Context
	watchStop context.CancelFunc
	watchWg   sync.WaitGroup
	started   bool // Track if watch is started
}

// NewDiscovery creates a new service discovery instance
func NewDiscovery(logger *zap.Logger) *Discovery {
	ctx, cancel := context.WithCancel(context.Background())
	return &Discovery{
		logger:    logger,
		sources:   make(map[string]DiscoverySource),
		watchers:  make([]func(*MCPServer, DiscoveryEvent), 0),
		servers:   make(map[string]*MCPServer),
		watchCtx:  ctx,
		watchStop: cancel,
	}
}

// Discover discovers MCP servers from all configured sources
func (d *Discovery) Discover(ctx context.Context) ([]*MCPServer, error) {
	d.mu.RLock()
	sources := make([]DiscoverySource, 0, len(d.sources))
	for _, source := range d.sources {
		if source.Enabled {
			sources = append(sources, source)
		}
	}
	d.mu.RUnlock()

	var allServers []*MCPServer
	for _, source := range sources {
		servers, err := d.discoverFromSource(ctx, source)
		if err != nil {
			d.logger.Error("Failed to discover from source",
				zap.String("source_id", source.ID),
				zap.String("source_type", string(source.Type)),
				zap.Error(err))
			continue
		}
		allServers = append(allServers, servers...)
	}

	// Update internal server cache
	d.mu.Lock()
	oldServers := make(map[string]*MCPServer)
	for k, v := range d.servers {
		oldServers[k] = v
	}

	// Clear current servers and repopulate
	d.servers = make(map[string]*MCPServer)
	for _, server := range allServers {
		d.servers[server.ID] = server
	}
	d.mu.Unlock()

	// Notify watchers of changes
	d.notifyWatchers(oldServers, d.servers)

	return allServers, nil
}

// Watch starts watching for server changes
func (d *Discovery) Watch(_ context.Context, callback func(*MCPServer, DiscoveryEvent)) error {
	d.mu.Lock()
	d.watchers = append(d.watchers, callback)

	// Start periodic discovery if not already running
	if !d.started {
		d.started = true
		d.watchWg.Add(1)
		go d.startPeriodicDiscovery()
	}
	d.mu.Unlock()

	return nil
}

// AddSource adds a discovery source
func (d *Discovery) AddSource(source DiscoverySource) error {
	if source.ID == "" {
		return fmt.Errorf("source ID cannot be empty")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.sources[source.ID] = source
	d.logger.Info("Added discovery source",
		zap.String("source_id", source.ID),
		zap.String("source_type", string(source.Type)),
		zap.Bool("enabled", source.Enabled))

	return nil
}

// RemoveSource removes a discovery source
func (d *Discovery) RemoveSource(sourceID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.sources[sourceID]; !exists {
		return fmt.Errorf("source %s not found", sourceID)
	}

	delete(d.sources, sourceID)
	d.logger.Info("Removed discovery source", zap.String("source_id", sourceID))

	return nil
}

// ListSources returns all configured discovery sources
func (d *Discovery) ListSources() ([]DiscoverySource, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	sources := make([]DiscoverySource, 0, len(d.sources))
	for _, source := range d.sources {
		sources = append(sources, source)
	}

	return sources, nil
}

// Stop stops the discovery service
func (d *Discovery) Stop() {
	d.mu.Lock()
	if d.watchStop != nil {
		d.watchStop()
	}
	d.mu.Unlock()

	d.watchWg.Wait()
}

// discoverFromSource discovers servers from a specific source
func (d *Discovery) discoverFromSource(ctx context.Context, source DiscoverySource) ([]*MCPServer, error) {
	switch source.Type {
	case SourceStatic:
		return d.discoverStatic(source)
	case SourceRegistry:
		return d.discoverRegistry(ctx, source)
	case SourceKubernetes:
		return d.discoverKubernetes(ctx, source)
	case SourceConsul:
		return d.discoverConsul(ctx, source)
	case SourceEtcd:
		return d.discoverEtcd(ctx, source)
	case SourceDNS:
		return d.discoverDNS(ctx, source)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", source.Type)
	}
}

// discoverStatic discovers servers from static configuration
func (d *Discovery) discoverStatic(source DiscoverySource) ([]*MCPServer, error) {
	serversJSON, ok := source.Config["servers"]
	if !ok {
		return []*MCPServer{}, nil
	}

	var servers []*MCPServer
	if err := json.Unmarshal([]byte(serversJSON), &servers); err != nil {
		return nil, fmt.Errorf("failed to parse static servers: %w", err)
	}

	// Set discovery metadata
	for _, server := range servers {
		if server.Metadata == nil {
			server.Metadata = make(map[string]string)
		}
		server.Metadata["discovery_source"] = source.ID
		server.Metadata["discovery_type"] = string(source.Type)
		server.CreatedAt = time.Now()
		server.UpdatedAt = time.Now()
	}

	return servers, nil
}

// discoverRegistry discovers servers from a remote registry
func (d *Discovery) discoverRegistry(ctx context.Context, source DiscoverySource) ([]*MCPServer, error) {
	registryURL, ok := source.Config["url"]
	if !ok {
		return nil, fmt.Errorf("registry URL not configured")
	}

	client := &http.Client{
		Timeout: HTTPClientTimeout,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", registryURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	// Add authentication if configured
	if apiKey, ok := source.Config["api_key"]; ok {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Warn("Failed to close response body", zap.Error(err))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var registryResponse struct {
		Servers []*MCPServer `json:"servers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&registryResponse); err != nil {
		return nil, fmt.Errorf("failed to parse registry response: %w", err)
	}

	// Set discovery metadata
	for _, server := range registryResponse.Servers {
		if server.Metadata == nil {
			server.Metadata = make(map[string]string)
		}
		server.Metadata["discovery_source"] = source.ID
		server.Metadata["discovery_type"] = string(source.Type)
		server.Metadata["registry_url"] = registryURL
		server.UpdatedAt = time.Now()
	}

	return registryResponse.Servers, nil
}

// discoverKubernetes discovers servers from Kubernetes API
func (d *Discovery) discoverKubernetes(_ context.Context, source DiscoverySource) ([]*MCPServer, error) {
	// Placeholder implementation for Kubernetes discovery
	// In a real implementation, you would:
	// 1. Connect to Kubernetes API
	// 2. List services with specific labels/annotations
	// 3. Extract MCP server information from service metadata
	// 4. Return discovered servers

	d.logger.Debug("Kubernetes discovery not yet implemented", zap.String("source_id", source.ID))
	return []*MCPServer{}, nil
}

// discoverConsul discovers servers from Consul service catalog
func (d *Discovery) discoverConsul(_ context.Context, source DiscoverySource) ([]*MCPServer, error) {
	// Placeholder implementation for Consul discovery
	// In a real implementation, you would:
	// 1. Connect to Consul API
	// 2. Query service catalog for MCP services
	// 3. Extract server information from service metadata
	// 4. Return discovered servers

	d.logger.Debug("Consul discovery not yet implemented", zap.String("source_id", source.ID))
	return []*MCPServer{}, nil
}

// discoverEtcd discovers servers from etcd registry
func (d *Discovery) discoverEtcd(_ context.Context, source DiscoverySource) ([]*MCPServer, error) {
	// Placeholder implementation for etcd discovery
	// In a real implementation, you would:
	// 1. Connect to etcd cluster
	// 2. Query for MCP server registrations
	// 3. Parse server information from stored data
	// 4. Return discovered servers

	d.logger.Debug("etcd discovery not yet implemented", zap.String("source_id", source.ID))
	return []*MCPServer{}, nil
}

// discoverDNS discovers servers from DNS records
func (d *Discovery) discoverDNS(_ context.Context, source DiscoverySource) ([]*MCPServer, error) {
	// Placeholder implementation for DNS discovery
	// In a real implementation, you would:
	// 1. Perform DNS lookups for SRV/TXT records
	// 2. Parse MCP server information from DNS records
	// 3. Return discovered servers

	d.logger.Debug("DNS discovery not yet implemented", zap.String("source_id", source.ID))
	return []*MCPServer{}, nil
}

// startPeriodicDiscovery starts periodic discovery in a goroutine
func (d *Discovery) startPeriodicDiscovery() {
	defer d.watchWg.Done()

	ticker := time.NewTicker(DiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.watchCtx.Done():
			return
		case <-ticker.C:
			if _, err := d.Discover(d.watchCtx); err != nil {
				d.logger.Error("Periodic discovery failed", zap.Error(err))
			}
		}
	}
}

// notifyWatchers notifies all watchers of server changes
func (d *Discovery) notifyWatchers(oldServers, newServers map[string]*MCPServer) {
	d.mu.RLock()
	watchers := make([]func(*MCPServer, DiscoveryEvent), len(d.watchers))
	copy(watchers, d.watchers)
	d.mu.RUnlock()

	// Find added servers
	for id, server := range newServers {
		if _, exists := oldServers[id]; !exists {
			for _, watcher := range watchers {
				go watcher(server, EventServerAdded)
			}
		}
	}

	// Find removed servers
	for id, server := range oldServers {
		if _, exists := newServers[id]; !exists {
			for _, watcher := range watchers {
				go watcher(server, EventServerRemoved)
			}
		}
	}

	// Find updated servers
	for id, newServer := range newServers {
		if oldServer, exists := oldServers[id]; exists {
			if oldServer.UpdatedAt.Before(newServer.UpdatedAt) {
				for _, watcher := range watchers {
					go watcher(newServer, EventServerUpdated)
				}
			}
		}
	}
}
