package federation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
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
	logger *zap.Logger
	// registry is the source of truth for server definitions (shared across
	// replicas by a non-memory impl). services holds the live per-pod
	// connections built from those definitions.
	registry  Registry
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

	// resultProcessor reduces tool-call results at the source, once, before the
	// agent caches them (cache-safe reduce-at-source, AIQG_CACHE_SAFE_REDUCTION.md
	// §9.A). Never nil after NewManager — defaults to a no-op; SetResultProcessor
	// installs a Gatekeeper-backed one. Reduction is a per-server opt-in
	// optimization (gated on srv.Reduce at the call site).
	resultProcessor ResultProcessor

	// scanner compliance-scans tool-call results at the federation boundary
	// (docs/AIQG-GATEKEEPER-INTEGRATION.md §4 / tas-llm-router#101 G2). Distinct
	// from resultProcessor: scanning is the SECURITY control, so it runs on every
	// external tool result regardless of srv.Reduce — a tool result is
	// TierExternal content on its way into an LLM prompt. Nil until
	// SetScanner installs a Gatekeeper-backed one (scanning off by default).
	scanner ResultScanner

	// toolCallObserver receives one ToolCallRecord per tools/call (Plan #22
	// T.2, the tool-call event publisher). Nil = no events; the
	// tas_mcp_tool_call_seconds histogram is recorded regardless.
	toolCallObserver ToolCallObserver

	// prober probes every registered server on an interval (Plan #22 T.4).
	// Nil = probes off; ProbeStatus then reports nothing.
	prober *Prober

	// tenancy is the tenant-scoped registry (Plan #22 C.1). Nil =
	// MCP_TENANT_SERVERS off: every server is visible to and callable by
	// every caller, as before C.1.
	tenancy *TenantPolicy

	// connections injects the caller tenant's connection on invoke (Plan #22
	// C.4). Nil = MCP_CONNECTION_INJECTION off: nothing is resolved or added.
	connections *ConnectionInjector

	// connRequired lists the servers that refuse any call not carrying the
	// caller tenant's stored connection (MCP_CONNECTION_REQUIRED_SERVERS).
	connRequired []string
}

// SetConnectionInjector turns per-tenant connection injection on (non-nil) or
// off (nil).
func (m *Manager) SetConnectionInjector(ci *ConnectionInjector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connections = ci
}

// injectConnection decides what the upstream call carries (Plan #22 C.4) and
// returns the context and request it uses, plus the redactor for everything
// the call produces. With injection off it returns (ctx, req, nil, nil).
//
//   - A caller-supplied DSN argument is stripped on a server that takes
//     database_dsn connections, from every caller.
//   - The tenant is the one the caller's TAS-Auth token resolved to. An
//     unattributed caller gets nothing injected; if the reason is that the
//     token could not be checked (validator unavailable or unconfigured) and
//     the server takes connections, the call is refused (fail closed).
//   - A suspended account's stored connection is never used: 403.
func (m *Manager) injectConnection(ctx context.Context, serverID string, req *MCPRequest) (context.Context, *MCPRequest, *redactor, error) {
	m.mu.RLock()
	ci := m.connections
	m.mu.RUnlock()
	if ci == nil {
		return ctx, req, nil, nil
	}
	srv, _ := m.registry.Get(ctx, serverID)
	capab := ci.capabilityOf(ctx, srv, serverID)
	out := stripCallerDSN(req, capab)

	id := identity.FromContext(ctx)
	tenant := callerTenant(id)
	if tenant == "" {
		if err := m.refuseUncheckedCaller(id, serverID, capab); err != nil {
			return ctx, req, nil, err
		}
		ConnectionInjections.WithLabelValues("unattributed").Inc()
		return ctx, out, nil, nil
	}
	if id.Suspended {
		if err := m.refuseSuspended(ctx, ci, tenant, serverID, capab); err != nil {
			return ctx, req, nil, err
		}
		ConnectionInjections.WithLabelValues("none").Inc()
		return ctx, out, nil, nil
	}
	if err := m.checkExpectedConnection(ctx, ci, id, tenant, serverID); err != nil {
		return ctx, req, nil, err
	}
	inj, err := ci.prepare(ctx, tenant, serverID, out)
	if err != nil {
		ConnectionInjections.WithLabelValues("failed").Inc()
		// err never carries a connection's secret: prepare builds its own
		// messages and never quotes a token endpoint. It is logged here and
		// the caller gets only the generic refusal.
		m.logger.Warn("federation: connection injection failed; call refused",
			zap.String("server_id", serverID),
			zap.String("caller_tenant_id", tenant),
			zap.Error(err))
		return ctx, req, nil, ErrConnectionUnavailable
	}
	if inj == nil {
		ConnectionInjections.WithLabelValues("none").Inc()
		return ctx, out, nil, nil
	}
	ConnectionInjections.WithLabelValues("injected").Inc()
	if inj.req != nil {
		out = inj.req
	}
	return withRedactor(withCallHeaders(ctx, inj.headers), inj.red), out, inj.red, nil
}

// stripCallerDSN removes caller-supplied DSN arguments from a tools/call on
// a server that takes database_dsn connections; while the server list is
// unknown, the default argument name is removed from every call.
func stripCallerDSN(req *MCPRequest, capab capability) *MCPRequest {
	if req.Method != methodToolsCall {
		return req
	}
	strip := capab.arguments
	if !capab.known && !slices.Contains(strip, defaultConnArgument) {
		strip = append(slices.Clone(strip), defaultConnArgument)
	}
	if c := withoutArguments(req, strip); c != nil {
		ConnectionInjections.WithLabelValues("stripped").Inc()
		return c
	}
	return req
}

// ErrConnectionMismatch — a delegated call named the stored connection its
// record is bound to, and the tenant's connection for the server is another
// one, or none (AIQG-88 LOW-2). The HTTP answer is 409.
var ErrConnectionMismatch = errors.New("connection mismatch: the tenant's stored connection is not the one this record is bound to")

// checkExpectedConnection refuses a call whose identity names an expected
// connection that is not the tenant's current one for the server. It runs
// before prepare, so a mismatch injects nothing and reaches no server. It
// resolves through the same cache prepare uses.
func (m *Manager) checkExpectedConnection(ctx context.Context, ci *ConnectionInjector, id *identity.Identity,
	tenant, serverID string) error {
	if id == nil || id.ExpectedConnectionID == "" {
		return nil
	}
	conn, err := ci.Resolver.Resolve(ctx, tenant, serverID)
	if err != nil {
		ConnectionInjections.WithLabelValues("failed").Inc()
		return ErrConnectionUnavailable
	}
	if conn == nil || conn.ID != id.ExpectedConnectionID {
		ConnectionInjections.WithLabelValues("mismatch").Inc()
		m.logger.Warn("federation: stored connection is not the one the record is bound to; call refused",
			zap.String("server_id", serverID), zap.String("caller_tenant_id", tenant))
		return ErrConnectionMismatch
	}
	return nil
}

// refuseUncheckedCaller fails closed when the caller's token could not be
// checked and the server takes connections (or that is unknown): serving it
// unattributed would run a tenant's call on the server's own credentials.
func (m *Manager) refuseUncheckedCaller(id *identity.Identity, serverID string, capab capability) error {
	if id == nil || (id.Result != identity.ResultValidatorUnavailable && id.Result != identity.ResultValidatorUnconfigured) {
		return nil
	}
	if capab.known && !capab.takesConnections() {
		return nil
	}
	ConnectionInjections.WithLabelValues("failed").Inc()
	m.logger.Warn("federation: caller could not be checked and the server takes connections; call refused",
		zap.String("server_id", serverID), zap.String("identity_result", id.Result))
	return ErrConnectionUnavailable
}

// refuseSuspended refuses a suspended account on a server where its stored
// connection would be used (or the server takes connections). Checked before
// prepare, so no OAuth grant runs with the suspended tenant's secret.
func (m *Manager) refuseSuspended(ctx context.Context, ci *ConnectionInjector, tenant, serverID string, capab capability) error {
	refuse := capab.takesConnections()
	if !refuse {
		conn, err := ci.Resolver.Resolve(ctx, tenant, serverID)
		refuse = err != nil || conn != nil
	}
	if !refuse {
		return nil
	}
	ConnectionInjections.WithLabelValues("suspended").Inc()
	m.logger.Warn("federation: suspended account on a server that takes connections; call refused",
		zap.String("server_id", serverID), zap.String("caller_tenant_id", tenant))
	return ErrAccountSuspended
}

// SetTenantPolicy turns the tenant-scoped registry on (non-nil) or off (nil).
func (m *Manager) SetTenantPolicy(p *TenantPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tenancy = p
}

// TenantPolicy returns the installed tenant policy, nil when off.
func (m *Manager) TenantPolicy() *TenantPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tenancy
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
	return NewManagerWithRegistry(logger, discovery, bridge, NewMemoryRegistry())
}

// NewManagerWithRegistry creates a federation manager backed by a specific
// Registry (e.g. a shared redisRegistry so replicas agree on the federated set).
func NewManagerWithRegistry(logger *zap.Logger, discovery ServiceDiscovery, bridge ProtocolBridge, registry Registry) *Manager {
	return &Manager{
		logger:          logger,
		registry:        newInstrumentedRegistry(registry),
		services:        make(map[string]MCPService),
		discovery:       discovery,
		bridge:          bridge,
		healthInterval:  DefaultHealthInterval,
		metrics:         &Metrics{},
		resultProcessor: noopResultProcessor{},
	}
}

// SetScanner installs the boundary content scanner for federated tool results
// (docs/AIQG-GATEKEEPER-INTEGRATION.md §2, G2). A nil argument turns scanning
// off. Safe to call at runtime; the hot path reads it under the manager lock.
func (m *Manager) SetScanner(s ResultScanner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scanner = s // nil is valid: scanning off
}

// SetResultProcessor installs the cache-safe reduce/scan processor for tool
// results (AIQG_CACHE_SAFE_REDUCTION.md §9.A). A nil argument resets to the
// no-op (reduction off). Safe to call at runtime; the hot path reads it under
// the manager lock.
func (m *Manager) SetResultProcessor(p ResultProcessor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p == nil {
		p = noopResultProcessor{}
	}
	m.resultProcessor = p
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

	if m.tenancy != nil {
		m.tenancy.Normalize(server)
		if err := m.tenancy.Validate(server); err != nil {
			return err
		}
	}

	// Check if server already exists
	if _, err := m.registry.Get(context.Background(), server.ID); err == nil {
		return fmt.Errorf("server with ID %s already registered", server.ID)
	}

	// Set timestamps
	now := time.Now()
	server.CreatedAt = now
	server.UpdatedAt = now
	server.Status = StatusUnknown

	// Store server
	if err := m.registry.Put(context.Background(), server); err != nil {
		return fmt.Errorf("failed to store server %s: %w", server.ID, err)
	}

	// Create service instance
	service, err := m.createService(server)
	if err != nil {
		_ = m.registry.Delete(context.Background(), server.ID)
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

// UpdateServer replaces an existing server's definition (PUT, Plan #22 C.1).
// It keeps the original CreatedAt, resets Status to unknown and drops this
// replica's live connection so the next invoke rebuilds it from the new
// definition; other replicas drop theirs on the registry put event (see
// watchRegistry). Returns an error wrapping ErrServerNotFound when the id is
// not registered.
func (m *Manager) UpdateServer(server *MCPServer) error {
	if server == nil {
		return fmt.Errorf("server cannot be nil")
	}
	if server.ID == "" {
		return fmt.Errorf("server ID cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, err := m.registry.Get(context.Background(), server.ID)
	if err != nil {
		return fmt.Errorf("server with ID %s: %w", server.ID, ErrServerNotFound)
	}
	if m.tenancy != nil {
		m.tenancy.Normalize(server)
		if err := m.tenancy.Validate(server); err != nil {
			return err
		}
	}
	server.CreatedAt = existing.CreatedAt
	server.UpdatedAt = time.Now()
	server.Status = StatusUnknown
	if err := m.registry.Put(context.Background(), server); err != nil {
		return fmt.Errorf("failed to store server %s: %w", server.ID, err)
	}
	m.dropServiceLocked(server.ID)

	m.logger.Info("Updated MCP server",
		zap.String("server_id", server.ID),
		zap.String("tenant_id", server.TenantID),
		zap.String("managed_by", string(server.ManagedBy)),
		zap.Bool("enabled", server.IsEnabled()),
		zap.String("endpoint", server.Endpoint))
	return nil
}

// dropServiceLocked stops and forgets this replica's live connection to id.
// Caller holds m.mu.
func (m *Manager) dropServiceLocked(id string) {
	if svc, ok := m.services[id]; ok {
		if err := svc.Stop(context.Background()); err != nil {
			m.logger.Warn("Failed to stop service", zap.String("server_id", id), zap.Error(err))
		}
		delete(m.services, id)
	}
}

// UnregisterServer removes an MCP server
func (m *Manager) UnregisterServer(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, err := m.registry.Get(context.Background(), id)
	if err != nil {
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
	_ = m.registry.Delete(context.Background(), id)

	m.logger.Info("Unregistered MCP server",
		zap.String("server_id", id),
		zap.String("name", server.Name))

	return nil
}

// GetServer retrieves a server by ID
func (m *Manager) GetServer(id string) (*MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	server, err := m.registry.Get(context.Background(), id)
	if err != nil {
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

	all, err := m.registry.List(context.Background())
	if err != nil {
		return nil, err
	}
	servers := make([]*MCPServer, 0, len(all))
	for _, server := range all {
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

	all, err := m.registry.List(context.Background())
	if err != nil {
		return nil, err
	}
	var servers []*MCPServer
	for _, server := range all {
		if server.Category == category {
			// Return copies to prevent external modification
			serverCopy := *server
			servers = append(servers, &serverCopy)
		}
	}

	return servers, nil
}

// ensureService returns this replica's live MCPService for serverID, building it
// lazily from the shared registry definition if it isn't cached yet. This is the
// core of the multi-replica fix: a definition registered on any replica becomes
// invokable on every replica without a broadcast. Returns a not-found error if
// no definition exists.
func (m *Manager) ensureService(ctx context.Context, serverID string) (MCPService, error) {
	m.mu.RLock()
	svc, ok := m.services[serverID]
	m.mu.RUnlock()
	if ok {
		return svc, nil
	}

	server, err := m.registry.Get(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("server with ID %s not found", serverID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check: another goroutine may have built it while we fetched the def.
	if svc, ok := m.services[serverID]; ok {
		return svc, nil
	}
	svc, err = m.createService(server)
	if err != nil {
		return nil, fmt.Errorf("failed to create service for server %s: %w", serverID, err)
	}
	m.services[serverID] = svc
	return svc, nil
}

// InvokeServer invokes a method on a specific server, building the local service
// lazily from the shared registry if needed (see ensureService).
func (m *Manager) InvokeServer(ctx context.Context, serverID string, request *MCPRequest) (*MCPResponse, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		m.updateResponseTimeMetrics(duration)
	}()

	// Self-heal: with a shared registry, the definition may exist even if this
	// replica never built the local MCPService (a register on another pod). Build
	// it on demand from the shared definition so any replica can serve any invoke.
	service, err := m.ensureService(ctx, serverID)
	if err != nil {
		m.updateFailureMetrics()
		return nil, err
	}

	m.updateRequestMetrics()

	isToolCall := request.Method == methodToolsCall
	var obs *callObservation
	if isToolCall {
		ctx, obs = withCallObservation(ctx)
	}

	// Tenancy (Plan #22 C.1): who may call this server, and which tools.
	srvDef, err := m.authorizeInvoke(ctx, serverID, request, obs)
	if err != nil {
		m.updateFailureMetrics()
		return nil, err
	}

	// Connection injection (Plan #22 C.4): the upstream call alone sees the
	// tenant's connection. The scanner, the reducer and the tool-call event
	// keep the original request, and the redactor scrubs the injected values
	// from the response and the error before anything else reads them.
	required := m.connectionRequired(serverID)
	toInject := request
	if required {
		toInject = withoutCallerConnection(request)
	}
	upCtx, upReq, red, err := m.injectConnection(ctx, serverID, toInject)
	if err != nil {
		m.updateFailureMetrics()
		return nil, err
	}
	if required {
		if err := m.requireConnection(serverID, upReq); err != nil {
			m.updateFailureMetrics()
			return nil, err
		}
	}

	callStart := time.Now()
	response, err := service.Invoke(upCtx, upReq)
	callDuration := time.Since(callStart)
	response, err = red.response(response), red.err(err)
	if isToolCall {
		// Observe on every exit from here on, success or failure, after the
		// response is final. Registered first so it runs last.
		defer func() { m.observeToolCall(ctx, serverID, request, response, err, obs, callStart, callDuration) }()
	}
	if err != nil {
		m.updateFailureMetrics()
		m.logger.Error("Failed to invoke server",
			zap.String("server_id", serverID),
			zap.String("method", request.Method),
			zap.Error(err))
		return nil, err
	}

	m.updateSuccessMetrics()

	if srvDef != nil && request.Method == methodToolsList && !onBehalf(ctx) {
		response = filterToolsList(response, srvDef)
	}

	// Boundary processing of tool-call results, ONCE, here at production, before
	// the agent caches them. Order is scan → reduce (both fail-open — the
	// original response is returned on any error). Only tools/call carries
	// reducible/scannable external content, so structured-output methods pass
	// through untouched.
	if isToolCall && response != nil {
		response = m.processToolResult(ctx, serverID, request, response)
	}
	return response, nil
}

// processToolResult scans, then reduces, one tools/call result (see
// InvokeServer). It sees the caller's request, never the copy carrying an
// injected connection, and a response the redactor has already scrubbed.
func (m *Manager) processToolResult(ctx context.Context, serverID string, request *MCPRequest, response *MCPResponse) *MCPResponse {
	m.mu.RLock()
	sc := m.scanner
	rp := m.resultProcessor
	m.mu.RUnlock()

	// Scan is the SECURITY control at the boundary: it runs on every
	// external tool result, independent of the per-server reduce opt-in
	// (docs/AIQG-GATEKEEPER-INTEGRATION.md §2 — scan at the boundary, not
	// only where reduction happens to be enabled). It scans the ORIGINAL
	// content and redacts (when enabled) BEFORE reduce runs — reduction is
	// lossy, so scanning reduced text would under-report.
	if sc != nil {
		response = sc.ScanResult(ctx, serverID, request, response)
	}

	// Reduce is a per-server opt-in optimization, gated on srv.Reduce (set
	// via the FederatedMCPServer CR).
	if rp != nil {
		if srv, gerr := m.registry.Get(ctx, serverID); gerr == nil && srv.Reduce {
			response = rp.ProcessResult(ctx, request, response)
		}
	}
	return response
}

// onBehalf reports whether this is an admin-token call made for a tenant
// (aiqg-dashboard-be's Test connection, C.4). Its tools/list is not filtered
// by allowed_tools, so the UI can choose the allowlist from every tool the
// server offers; tools/call is still refused outside the allowlist.
func onBehalf(ctx context.Context) bool {
	id := identity.FromContext(ctx)
	return id != nil && id.Result == identity.ResultOnBehalf
}

// authorizeInvoke applies the tenant policy to one invoke. With the policy off
// it returns (nil, nil) and costs nothing. With it on it returns the server's
// definition, or an error: ErrServerDisabled or ErrTenantForbidden for the
// server, ErrToolNotAllowed for a tools/call outside allowed_tools.
//
// A refused tool is recorded as a tool-call event with outcome "blocked". A
// tenant refusal is not: the event would be attributed to the caller's
// tenant and would name another tenant's server in it. It is logged instead.
func (m *Manager) authorizeInvoke(ctx context.Context, serverID string, req *MCPRequest, obs *callObservation) (*MCPServer, error) {
	p := m.TenantPolicy()
	if p == nil {
		return nil, nil
	}
	srv, err := m.registry.Get(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("server with ID %s not found", serverID)
	}
	id := identity.FromContext(ctx)
	if err := p.CheckInvoke(srv, id); err != nil {
		m.logger.Warn("federation: invoke refused",
			zap.String("server_id", serverID),
			zap.String("method", req.Method),
			zap.String("caller_tenant_id", callerTenant(id)),
			zap.Error(err))
		return nil, err
	}
	if req.Method == methodToolsCall && !srv.ToolAllowed(toolCallName(req)) {
		if obs != nil {
			obs.mu.Lock()
			obs.blocked = true
			obs.mu.Unlock()
		}
		now := time.Now()
		m.observeToolCall(ctx, serverID, req, nil, ErrToolNotAllowed, obs, now, 0)
		return nil, ErrToolNotAllowed
	}
	return srv, nil
}

// observeToolCall records the histogram and hands the record to the observer.
// It must never affect the call: it runs after the response is final, and a
// panic in an observer is contained here.
func (m *Manager) observeToolCall(ctx context.Context, serverID string, req *MCPRequest, resp *MCPResponse,
	invokeErr error, obs *callObservation, start time.Time, dur time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("tool-call observer panicked; call unaffected", zap.Any("panic", r))
		}
	}()

	upstreamErr := invokeErr
	scanned, blocked := false, false
	findings := map[string]int{}
	if obs != nil {
		obs.mu.Lock()
		if upstreamErr == nil {
			upstreamErr = obs.upstreamErr
		}
		scanned, blocked = obs.scanned, obs.blocked
		for k, v := range obs.findings {
			findings[k] = v
		}
		obs.mu.Unlock()
	}

	tool := toolCallName(req)
	outcome := classifyOutcome(upstreamErr, resp, blocked)
	ToolCallSeconds.WithLabelValues(serverID, toolLabels.label(serverID, tool), outcome).Observe(dur.Seconds())

	m.mu.RLock()
	o := m.toolCallObserver
	m.mu.RUnlock()
	if o == nil {
		return
	}

	id := identity.FromContext(ctx)
	rec := ToolCallRecord{
		ServerID:     serverID,
		ToolName:     tool,
		ToolCallID:   toolCallID(ctx, req, id),
		Outcome:      outcome,
		Duration:     dur,
		ResultBytes:  resultBytes(resp),
		ReducedBytes: reducedBytes(resp),
		Findings:     findings,
		ScanSkipped:  !scanned,
		OccurredAt:   start.UTC(),
		Identity:     id,
	}
	o.ObserveToolCall(rec)
}

// toolCallID picks the id that joins this call to the gateway's view of it:
// the TAS-Tool-Call-Id header (the LLM's tool_call_id) when the agent sends
// it, else the client's JSON-RPC request id, else a fresh UUID (never the
// handler's synthetic req_<nanos>, which identifies nothing outside this pod).
func toolCallID(ctx context.Context, req *MCPRequest, id *identity.Identity) string {
	if id != nil && id.ToolCallID != "" {
		return id.ToolCallID
	}
	if req != nil && req.ID != "" && !requestIDGenerated(ctx) {
		return req.ID
	}
	return uuid.NewString()
}

// BroadcastRequest sends a request to all healthy servers
func (m *Manager) BroadcastRequest(ctx context.Context, request *MCPRequest) ([]*MCPResponse, error) {
	m.mu.RLock()
	services := make(map[string]MCPService)
	caller := identity.FromContext(ctx)
	for id, service := range m.services {
		// Broadcast injects nothing and strips nothing, so a server that
		// requires a connection takes only tools/list from it.
		if request.Method != methodToolsList && slices.Contains(m.connRequired, id) {
			continue
		}
		if srv, err := m.registry.Get(ctx, id); err == nil && srv.Status == StatusHealthy &&
			m.tenancy.CheckInvoke(srv, caller) == nil {
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
	server, serverErr := m.registry.Get(ctx, serverID)
	m.mu.RUnlock()

	if !exists || serverErr != nil {
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

	all, err := m.registry.List(context.Background())
	if err != nil {
		return nil, err
	}
	status := make(map[string]ServerStatus)
	for _, server := range all {
		status[server.ID] = server.Status
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

	// Converge local state with the shared registry (drop the live service for a
	// server another replica unregistered).
	m.healthWg.Add(1)
	go m.watchRegistry()

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
	if all, err := m.registry.List(context.Background()); err == nil {
		for _, server := range all {
			if server.Status == StatusHealthy {
				activeCount++
			}
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

// watchRegistry consumes registry change events and converges this replica's
// local services. On a delete (possibly from another replica) it stops and drops
// the local MCPService so this pod stops serving the removed server. New/updated
// definitions are picked up lazily by ensureService on the next invoke.
func (m *Manager) watchRegistry() {
	defer m.healthWg.Done()

	events, err := m.registry.Watch(m.healthCtx)
	if err != nil {
		m.logger.Error("Failed to watch registry", zap.Error(err))
		return
	}
	for {
		select {
		case <-m.healthCtx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			m.mu.Lock()
			// With the tenant registry on, a definition can change in place
			// (PUT), so a put drops the live connection too; the next invoke
			// rebuilds it from the new definition.
			if evt.Type == RegistryDelete || (evt.Type == RegistryPut && m.tenancy != nil) {
				m.dropServiceLocked(evt.ID)
			}
			m.mu.Unlock()
		}
	}
}

// performHealthChecks performs health checks on all servers
func (m *Manager) performHealthChecks() {
	m.mu.RLock()
	all, _ := m.registry.List(context.Background())
	servers := make([]string, 0, len(all))
	for _, server := range all {
		if m.tenancy != nil && !server.IsEnabled() {
			continue // a disabled server is not health-checked
		}
		if server.HealthCheck.Enabled {
			servers = append(servers, server.ID)
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
		// Update existing server (read-modify-Put so a serializing registry
		// persists the change; for memoryRegistry this mutates the live pointer).
		m.mu.Lock()
		if existing, err := m.registry.Get(context.Background(), server.ID); err == nil {
			existing.UpdatedAt = time.Now()
			existing.Endpoint = server.Endpoint
			existing.Status = server.Status
			existing.Metadata = server.Metadata
			_ = m.registry.Put(context.Background(), existing)
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
