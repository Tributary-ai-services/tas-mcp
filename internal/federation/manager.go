package federation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultHealthInterval is the default interval for health checks
	DefaultHealthInterval = 30 * time.Second
	// HealthCheckTimeout is the default timeout for health check operations
	HealthCheckTimeout = 10 * time.Second
	// ResponseTimeAverageWeight is the weight used for calculating average response time
	ResponseTimeAverageWeight = 2
)

// Manager implements the TASManager interface
type Manager struct {
	logger    *zap.Logger
	servers   map[string]*MCPServer
	services  map[string]MCPService
	discovery ServiceDiscovery
	bridge    ProtocolBridge
	mu        sync.RWMutex

	// Health monitoring
	healthInterval time.Duration
	healthCtx      context.Context
	healthCancel   context.CancelFunc
	healthWg       sync.WaitGroup

	// Metrics
	metrics *Metrics
}

// Metrics tracks federation manager statistics
type Metrics struct {
	ServersRegistered   int64 `json:"servers_registered"`
	ServersActive       int64 `json:"servers_active"`
	RequestsTotal       int64 `json:"requests_total"`
	RequestsSuccessful  int64 `json:"requests_successful"`
	RequestsFailed      int64 `json:"requests_failed"`
	HealthChecksTotal   int64 `json:"health_checks_total"`
	HealthChecksFailed  int64 `json:"health_checks_failed"`
	AverageResponseTime int64 `json:"average_response_time_ms"`
	mu                  sync.RWMutex
}

// NewManager creates a new federation manager
func NewManager(logger *zap.Logger, discovery ServiceDiscovery, bridge ProtocolBridge) *Manager {
	return &Manager{
		logger:         logger,
		servers:        make(map[string]*MCPServer),
		services:       make(map[string]MCPService),
		discovery:      discovery,
		bridge:         bridge,
		healthInterval: DefaultHealthInterval,
		metrics:        &Metrics{},
	}
}

// NewManagerWithDefaults creates a new federation manager with default implementations
func NewManagerWithDefaults(logger *zap.Logger) *Manager {
	discovery := NewDiscovery(logger)
	bridge := NewBridge(logger)
	return NewManager(logger, discovery, bridge)
}

// DefaultProtocolBridge provides a simple protocol bridge implementation
type DefaultProtocolBridge struct{}

// NewDefaultProtocolBridge creates a new default protocol bridge
func NewDefaultProtocolBridge() *DefaultProtocolBridge {
	return &DefaultProtocolBridge{}
}

// TranslateRequest implements basic request translation
func (b *DefaultProtocolBridge) TranslateRequest(_ context.Context, _, _ Protocol, request *MCPRequest) (*MCPRequest, error) {
	return request, nil
}

// TranslateResponse implements basic response translation
func (b *DefaultProtocolBridge) TranslateResponse(_ context.Context, _, _ Protocol, response *MCPResponse) (*MCPResponse, error) {
	return response, nil
}

// SupportsProtocol checks if a protocol is supported
func (b *DefaultProtocolBridge) SupportsProtocol(protocol Protocol) bool {
	return protocol == ProtocolHTTP || protocol == ProtocolGRPC
}

// SupportedProtocols returns all supported protocols
func (b *DefaultProtocolBridge) SupportedProtocols() []Protocol {
	return []Protocol{ProtocolHTTP, ProtocolGRPC}
}

// RegisterServer registers a new MCP server
func (m *Manager) RegisterServer(server *MCPServer) error {
	if server == nil {
		return fmt.Errorf("server cannot be nil")
	}

	if server.ID == "" {
		return fmt.Errorf("server ID cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if server already exists
	if _, exists := m.servers[server.ID]; exists {
		return fmt.Errorf("server with ID %s already registered", server.ID)
	}

	// Set timestamps
	now := time.Now()
	server.CreatedAt = now
	server.UpdatedAt = now
	server.Status = StatusUnknown

	// Store server
	m.servers[server.ID] = server

	// Create service instance
	service, err := m.createService(server)
	if err != nil {
		delete(m.servers, server.ID)
		return fmt.Errorf("failed to create service for server %s: %w", server.ID, err)
	}

	m.services[server.ID] = service

	// Update metrics
	m.metrics.mu.Lock()
	m.metrics.ServersRegistered++
	m.metrics.mu.Unlock()

	m.logger.Info("Registered MCP server",
		zap.String("server_id", server.ID),
		zap.String("name", server.Name),
		zap.String("category", server.Category),
		zap.String("protocol", string(server.Protocol)),
		zap.String("endpoint", server.Endpoint))

	return nil
}

// UnregisterServer removes an MCP server
func (m *Manager) UnregisterServer(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, exists := m.servers[id]
	if !exists {
		return fmt.Errorf("server with ID %s not found", id)
	}

	// Stop service if it exists
	if service, ok := m.services[id]; ok {
		if err := service.Stop(context.Background()); err != nil {
			m.logger.Warn("Failed to stop service during unregistration",
				zap.String("server_id", id),
				zap.Error(err))
		}
		delete(m.services, id)
	}

	// Remove server
	delete(m.servers, id)

	m.logger.Info("Unregistered MCP server",
		zap.String("server_id", id),
		zap.String("name", server.Name))

	return nil
}

// GetServer retrieves a server by ID
func (m *Manager) GetServer(id string) (*MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	server, exists := m.servers[id]
	if !exists {
		return nil, fmt.Errorf("server with ID %s not found", id)
	}

	// Return a copy to prevent external modification
	serverCopy := *server
	return &serverCopy, nil
}

// ListServers returns all registered servers
func (m *Manager) ListServers() ([]*MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	servers := make([]*MCPServer, 0, len(m.servers))
	for _, server := range m.servers {
		// Return copies to prevent external modification
		serverCopy := *server
		servers = append(servers, &serverCopy)
	}

	return servers, nil
}

// ListServersByCategory returns servers filtered by category
func (m *Manager) ListServersByCategory(category string) ([]*MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var servers []*MCPServer
	for _, server := range m.servers {
		if server.Category == category {
			// Return copies to prevent external modification
			serverCopy := *server
			servers = append(servers, &serverCopy)
		}
	}

	return servers, nil
}

// InvokeServer invokes a method on a specific server
func (m *Manager) InvokeServer(ctx context.Context, serverID string, request *MCPRequest) (*MCPResponse, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		m.updateResponseTimeMetrics(duration)
	}()

	m.mu.RLock()
	service, exists := m.services[serverID]
	m.mu.RUnlock()

	if !exists {
		m.updateFailureMetrics()
		return nil, fmt.Errorf("server with ID %s not found", serverID)
	}

	m.updateRequestMetrics()

	response, err := service.Invoke(ctx, request)
	if err != nil {
		m.updateFailureMetrics()
		m.logger.Error("Failed to invoke server",
			zap.String("server_id", serverID),
			zap.String("method", request.Method),
			zap.Error(err))
		return nil, err
	}

	m.updateSuccessMetrics()
	return response, nil
}

// BroadcastRequest sends a request to all healthy servers
func (m *Manager) BroadcastRequest(ctx context.Context, request *MCPRequest) ([]*MCPResponse, error) {
	m.mu.RLock()
	services := make(map[string]MCPService)
	for id, service := range m.services {
		if m.servers[id].Status == StatusHealthy {
			services[id] = service
		}
	}
	m.mu.RUnlock()

	responses := make([]*MCPResponse, 0, len(services))
	errors := make([]error, 0)

	// Use goroutines for concurrent requests
	type result struct {
		response *MCPResponse
		error    error
	}

	resultChan := make(chan result, len(services))

	for serverID, service := range services {
		go func(_ string, svc MCPService) {
			resp, err := svc.Invoke(ctx, request)
			resultChan <- result{response: resp, error: err}
		}(serverID, service)
	}

	// Collect results
	for i := 0; i < len(services); i++ {
		res := <-resultChan
		if res.error != nil {
			errors = append(errors, res.error)
			m.updateFailureMetrics()
		} else {
			responses = append(responses, res.response)
			m.updateSuccessMetrics()
		}
		m.updateRequestMetrics()
	}

	if len(responses) == 0 && len(errors) > 0 {
		return nil, fmt.Errorf("all broadcast requests failed: %v", errors)
	}

	return responses, nil
}

// CheckHealth performs a health check on a specific server
func (m *Manager) CheckHealth(ctx context.Context, serverID string) error {
	m.mu.RLock()
	service, exists := m.services[serverID]
	server, serverExists := m.servers[serverID]
	m.mu.RUnlock()

	if !exists || !serverExists {
		return fmt.Errorf("server with ID %s not found", serverID)
	}

	m.metrics.mu.Lock()
	m.metrics.HealthChecksTotal++
	m.metrics.mu.Unlock()

	err := service.Health(ctx)

	// Update server status based on health check
	m.mu.Lock()
	if err != nil {
		server.Status = StatusUnhealthy
		m.metrics.mu.Lock()
		m.metrics.HealthChecksFailed++
		m.metrics.mu.Unlock()
	} else {
		server.Status = StatusHealthy
	}
	server.UpdatedAt = time.Now()
	m.mu.Unlock()

	return err
}

// GetHealthStatus returns the health status of all servers
func (m *Manager) GetHealthStatus() (map[string]ServerStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]ServerStatus)
	for id, server := range m.servers {
		status[id] = server.Status
	}

	return status, nil
}

// Start starts the federation manager
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting federation manager")

	// Initialize health monitoring
	m.healthCtx, m.healthCancel = context.WithCancel(ctx)
	m.healthWg.Add(1)
	go m.healthMonitor()

	// Start discovery if available
	if m.discovery != nil {
		if err := m.discovery.Watch(ctx, m.handleDiscoveryEvent); err != nil {
			m.logger.Error("Failed to start service discovery", zap.Error(err))
			return err
		}
	}

	// Start all registered services
	m.mu.RLock()
	services := make([]MCPService, 0, len(m.services))
	for _, service := range m.services {
		services = append(services, service)
	}
	m.mu.RUnlock()

	for _, service := range services {
		if err := service.Start(ctx); err != nil {
			m.logger.Error("Failed to start service",
				zap.String("service_id", service.ID()),
				zap.Error(err))
		}
	}

	m.logger.Info("Federation manager started")
	return nil
}

// Stop stops the federation manager
func (m *Manager) Stop(ctx context.Context) error {
	m.logger.Info("Stopping federation manager")

	// Cancel health monitoring
	if m.healthCancel != nil {
		m.healthCancel()
		m.healthWg.Wait()
	}

	// Stop all services
	m.mu.RLock()
	services := make([]MCPService, 0, len(m.services))
	for _, service := range m.services {
		services = append(services, service)
	}
	m.mu.RUnlock()

	for _, service := range services {
		if err := service.Stop(ctx); err != nil {
			m.logger.Error("Failed to stop service",
				zap.String("service_id", service.ID()),
				zap.Error(err))
		}
	}

	m.logger.Info("Federation manager stopped")
	return nil
}

// GetMetrics returns current federation metrics
func (m *Manager) GetMetrics() *Metrics {
	m.metrics.mu.RLock()
	defer m.metrics.mu.RUnlock()

	// Count active servers
	m.mu.RLock()
	activeCount := int64(0)
	for _, server := range m.servers {
		if server.Status == StatusHealthy {
			activeCount++
		}
	}
	m.mu.RUnlock()

	return &Metrics{
		ServersRegistered:   m.metrics.ServersRegistered,
		ServersActive:       activeCount,
		RequestsTotal:       m.metrics.RequestsTotal,
		RequestsSuccessful:  m.metrics.RequestsSuccessful,
		RequestsFailed:      m.metrics.RequestsFailed,
		HealthChecksTotal:   m.metrics.HealthChecksTotal,
		HealthChecksFailed:  m.metrics.HealthChecksFailed,
		AverageResponseTime: m.metrics.AverageResponseTime,
	}
}

// createService creates a service instance for a server
func (m *Manager) createService(server *MCPServer) (MCPService, error) {
	// This would create the appropriate service based on the server's protocol
	// For now, we'll return a placeholder
	return NewGenericService(server, m.logger, m.bridge)
}

// healthMonitor runs periodic health checks
func (m *Manager) healthMonitor() {
	defer m.healthWg.Done()

	ticker := time.NewTicker(m.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.healthCtx.Done():
			return
		case <-ticker.C:
			m.performHealthChecks()
		}
	}
}

// performHealthChecks performs health checks on all servers
func (m *Manager) performHealthChecks() {
	m.mu.RLock()
	servers := make([]string, 0, len(m.servers))
	for id, server := range m.servers {
		if server.HealthCheck.Enabled {
			servers = append(servers, id)
		}
	}
	m.mu.RUnlock()

	for _, serverID := range servers {
		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), HealthCheckTimeout)
			defer cancel()

			if err := m.CheckHealth(ctx, id); err != nil {
				m.logger.Debug("Health check failed",
					zap.String("server_id", id),
					zap.Error(err))
			}
		}(serverID)
	}
}

// handleDiscoveryEvent processes service discovery events
func (m *Manager) handleDiscoveryEvent(server *MCPServer, event DiscoveryEvent) {
	switch event {
	case EventServerAdded:
		if err := m.RegisterServer(server); err != nil {
			m.logger.Error("Failed to register discovered server",
				zap.String("server_id", server.ID),
				zap.Error(err))
		}
	case EventServerRemoved:
		if err := m.UnregisterServer(server.ID); err != nil {
			m.logger.Error("Failed to unregister server",
				zap.String("server_id", server.ID),
				zap.Error(err))
		}
	case EventServerUpdated:
		// Update existing server
		m.mu.Lock()
		if existing, exists := m.servers[server.ID]; exists {
			existing.UpdatedAt = time.Now()
			existing.Endpoint = server.Endpoint
			existing.Status = server.Status
			existing.Metadata = server.Metadata
		}
		m.mu.Unlock()
	}
}

// updateRequestMetrics increments request counter
func (m *Manager) updateRequestMetrics() {
	m.metrics.mu.Lock()
	m.metrics.RequestsTotal++
	m.metrics.mu.Unlock()
}

// updateSuccessMetrics increments success counter
func (m *Manager) updateSuccessMetrics() {
	m.metrics.mu.Lock()
	m.metrics.RequestsSuccessful++
	m.metrics.mu.Unlock()
}

// updateFailureMetrics increments failure counter
func (m *Manager) updateFailureMetrics() {
	m.metrics.mu.Lock()
	m.metrics.RequestsFailed++
	m.metrics.mu.Unlock()
}

// updateResponseTimeMetrics updates average response time
func (m *Manager) updateResponseTimeMetrics(duration time.Duration) {
	m.metrics.mu.Lock()
	// Simple moving average - could be improved with more sophisticated calculation
	if m.metrics.AverageResponseTime == 0 {
		m.metrics.AverageResponseTime = duration.Milliseconds()
	} else {
		m.metrics.AverageResponseTime = (m.metrics.AverageResponseTime + duration.Milliseconds()) / ResponseTimeAverageWeight
	}
	m.metrics.mu.Unlock()
}
