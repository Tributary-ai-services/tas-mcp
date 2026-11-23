package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

// HTTPHandlers provides HTTP endpoints for federation management
type HTTPHandlers struct {
	manager TASManager
	logger  *zap.Logger

	// identity resolves the caller (TAS-Auth + baggage) on invoke/broadcast.
	// Nil = identity disabled: no resolution, no validator call, and the
	// request context carries no Identity (Plan #22 T.1).
	identity *identity.Resolver

	// delegation is the delegate credential (AIQG-88). Nil = off: the
	// delegate headers mean nothing, as before.
	delegation *Delegation
}

// SetDelegation installs the delegate credential. Nil turns it off.
func (h *HTTPHandlers) SetDelegation(d *Delegation) {
	h.delegation = d
}

// SetIdentityResolver installs caller-identity resolution (observe mode). A nil
// resolver turns it off.
func (h *HTTPHandlers) SetIdentityResolver(r *identity.Resolver) {
	h.identity = r
}

// withIdentity resolves the caller into the request context. Observe mode: it
// never rejects; an unresolvable caller is carried as unattributed.
func (h *HTTPHandlers) withIdentity(r *http.Request) context.Context {
	ctx := r.Context()
	if h.identity == nil {
		return ctx
	}
	return identity.WithIdentity(ctx, h.identity.Resolve(ctx, r.Header))
}

// TenantPolicyProvider is implemented by a manager that hosts the
// tenant-scoped registry (Plan #22 C.1). *Manager implements it.
type TenantPolicyProvider interface {
	TenantPolicy() *TenantPolicy
}

// ServerUpdater is implemented by a manager that supports PUT (C.1).
type ServerUpdater interface {
	UpdateServer(server *MCPServer) error
}

// tenancy returns the manager's tenant policy, nil when MCP_TENANT_SERVERS is
// off (or the manager does not support it).
func (h *HTTPHandlers) tenancy() *TenantPolicy {
	if p, ok := h.manager.(TenantPolicyProvider); ok {
		return p.TenantPolicy()
	}
	return nil
}

// requireAdmin refuses a write without MCP_ADMIN_TOKEN when the tenant
// registry is on, and reports whether the handler may continue. Off, every
// write is allowed, as before C.1.
func (h *HTTPHandlers) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	p := h.tenancy()
	if p == nil {
		return true
	}
	switch p.AdminStatus(r) {
	case 0:
		return true
	case http.StatusServiceUnavailable:
		http.Error(w, "federation writes are disabled: MCP_ADMIN_TOKEN is not configured", http.StatusServiceUnavailable)
	case http.StatusUnauthorized:
		http.Error(w, "missing "+AdminTokenHeader+" header", http.StatusUnauthorized)
	default:
		http.Error(w, "invalid "+AdminTokenHeader, http.StatusForbidden)
	}
	h.logger.Warn("federation: write refused without a valid admin token",
		zap.String("method", r.Method), zap.String("path", r.URL.Path))
	return false
}

// visibleTo returns the filter a read applies: nil (everything) when the
// tenant registry is off or the request carries the admin token; otherwise
// platform servers plus the caller's own tenant's, enabled only. The tenant
// comes from the resolved TAS-Auth token, never from the request.
func (h *HTTPHandlers) visibleTo(r *http.Request) func(*MCPServer) bool {
	p := h.tenancy()
	if p == nil || p.IsAdmin(r) {
		return nil
	}
	id := identity.FromContext(h.withIdentity(r))
	return func(s *MCPServer) bool { return p.Visible(s, id) }
}

// filterServers applies visibleTo's filter.
func filterServers(servers []*MCPServer, keep func(*MCPServer) bool) []*MCPServer {
	if keep == nil {
		return servers
	}
	out := make([]*MCPServer, 0, len(servers))
	for _, s := range servers {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

// serverVisible reports whether the server id exists and r may see it.
func (h *HTTPHandlers) serverVisible(r *http.Request, id string) bool {
	keep := h.visibleTo(r)
	if keep == nil {
		return true
	}
	s, err := h.manager.GetServer(id)
	return err == nil && keep(s)
}

// statusFor maps a tenancy error to its HTTP status, or fallback.
func statusFor(err error, fallback int) int {
	switch {
	case errors.Is(err, ErrTenantForbidden), errors.Is(err, ErrServerDisabled), errors.Is(err, ErrToolNotAllowed):
		return http.StatusForbidden
	case errors.Is(err, ErrServerNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrInvalidServer), errors.Is(err, ErrEgressDenied):
		return http.StatusBadRequest
	}
	return fallback
}

// NewHTTPHandlers creates new HTTP handlers for federation
func NewHTTPHandlers(manager TASManager, logger *zap.Logger) *HTTPHandlers {
	return &HTTPHandlers{
		manager: manager,
		logger:  logger,
	}
}

// RegisterRoutes registers federation HTTP routes
func (h *HTTPHandlers) RegisterRoutes(router *mux.Router) {
	// Federation server management
	router.HandleFunc("/api/v1/federation/servers", h.handleListServers).Methods("GET")
	router.HandleFunc("/api/v1/federation/servers", h.handleRegisterServer).Methods("POST")
	router.HandleFunc("/api/v1/federation/servers/{id}", h.handleGetServer).Methods("GET")
	if h.tenancy() != nil {
		// PUT exists only with the tenant registry on (Plan #22 C.1); off,
		// the route table is exactly as before.
		router.HandleFunc("/api/v1/federation/servers/{id}", h.handleUpdateServer).Methods("PUT")
	}
	router.HandleFunc("/api/v1/federation/servers/{id}", h.handleUnregisterServer).Methods("DELETE")

	// Server operations
	router.HandleFunc("/api/v1/federation/servers/{id}/invoke", h.handleInvokeServer).Methods("POST")
	router.HandleFunc("/api/v1/federation/servers/{id}/health", h.handleCheckHealth).Methods("GET")
	router.HandleFunc("/api/v1/federation/servers/{id}/probe", h.handleGetProbe).Methods("GET")

	// Broadcast operations
	router.HandleFunc("/api/v1/federation/broadcast", h.handleBroadcast).Methods("POST")

	// Health and metrics
	router.HandleFunc("/api/v1/federation/health", h.handleGetHealthStatus).Methods("GET")
	router.HandleFunc("/api/v1/federation/metrics", h.handleGetMetrics).Methods("GET")

	// Category filtering
	router.HandleFunc("/api/v1/federation/categories/{category}/servers", h.handleListServersByCategory).Methods("GET")
}

// handleListServers returns all registered servers
func (h *HTTPHandlers) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.manager.ListServers()
	if err != nil {
		h.logger.Error("Failed to list servers", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	servers = filterServers(servers, h.visibleTo(r))

	out := make([]serverView, 0, len(servers))
	for _, s := range servers {
		out = append(out, h.view(s))
	}
	h.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"servers": out,
		"total":   len(servers),
	})
}

// serverView is a registered server plus this replica's last probe of it
// (Plan #22 T.4). Probe is omitted when probes are off or the server has not
// been probed yet; the operator reads that as Reachable=Unknown.
type serverView struct {
	*MCPServer
	Probe *ProbeResult `json:"probe,omitempty"`
}

func (h *HTTPHandlers) view(s *MCPServer) serverView {
	if p := h.tenancy(); p != nil {
		// Show the lazily-migrated defaults explicitly, so a reader (the
		// operator's ownership check, the AIQG reconcile) never has to know
		// that an absent field means operator / enabled.
		c := *s
		p.Normalize(&c)
		s = &c
	}
	v := serverView{MCPServer: s}
	if p, ok := h.manager.(ProbeStatusProvider); ok {
		if r, ok := p.ProbeStatus(s.ID); ok {
			v.Probe = &r
		}
	}
	return v
}

// handleGetProbe returns this replica's last probe of a server: 200 with the
// result, 404 when the server is unknown or not probed yet.
func (h *HTTPHandlers) handleGetProbe(w http.ResponseWriter, r *http.Request) {
	serverID := mux.Vars(r)["id"]
	if !h.serverVisible(r, serverID) {
		http.Error(w, "no probe result for server "+serverID, http.StatusNotFound)
		return
	}
	p, ok := h.manager.(ProbeStatusProvider)
	if !ok {
		http.Error(w, "probes not available", http.StatusNotFound)
		return
	}
	res, ok := p.ProbeStatus(serverID)
	if !ok {
		http.Error(w, "no probe result for server "+serverID, http.StatusNotFound)
		return
	}
	h.writeJSONResponse(w, http.StatusOK, res)
}

// handleRegisterServer registers a new MCP server
func (h *HTTPHandlers) handleRegisterServer(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	var server MCPServer
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		h.logger.Error("Failed to decode server", zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := h.manager.RegisterServer(&server); err != nil {
		h.logger.Error("Failed to register server", zap.Error(err))
		http.Error(w, err.Error(), statusFor(err, http.StatusBadRequest))
		return
	}

	h.logger.Info("Registered server via HTTP API",
		zap.String("server_id", server.ID),
		zap.String("name", server.Name))

	h.writeJSONResponse(w, http.StatusCreated, map[string]interface{}{
		"message":   "Server registered successfully",
		"server_id": server.ID,
	})
}

// handleGetServer returns a specific server
func (h *HTTPHandlers) handleGetServer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

	server, err := h.manager.GetServer(serverID)
	if err != nil {
		h.logger.Error("Failed to get server", zap.String("server_id", serverID), zap.Error(err))
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if keep := h.visibleTo(r); keep != nil && !keep(server) {
		// Invisible reads as absent: a caller learns nothing about another
		// tenant's servers, not even that the id is taken.
		http.Error(w, fmt.Sprintf("server with ID %s not found", serverID), http.StatusNotFound)
		return
	}

	h.writeJSONResponse(w, http.StatusOK, h.view(server))
}

// handleUpdateServer replaces a server's definition (PUT, tenant registry
// on only). The path id wins; a body id that disagrees is a 400.
func (h *HTTPHandlers) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	u, ok := h.manager.(ServerUpdater)
	if !ok {
		http.Error(w, "update not supported", http.StatusNotImplemented)
		return
	}
	serverID := mux.Vars(r)["id"]
	var server MCPServer
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if server.ID != "" && server.ID != serverID {
		http.Error(w, "body id does not match the path id", http.StatusBadRequest)
		return
	}
	server.ID = serverID
	if err := u.UpdateServer(&server); err != nil {
		h.logger.Error("Failed to update server", zap.String("server_id", serverID), zap.Error(err))
		http.Error(w, err.Error(), statusFor(err, http.StatusBadRequest))
		return
	}
	h.logger.Info("Updated server via HTTP API", zap.String("server_id", serverID))
	h.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message":   "Server updated successfully",
		"server_id": serverID,
	})
}

// handleUnregisterServer removes a server
func (h *HTTPHandlers) handleUnregisterServer(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	vars := mux.Vars(r)
	serverID := vars["id"]

	if err := h.manager.UnregisterServer(serverID); err != nil {
		h.logger.Error("Failed to unregister server", zap.String("server_id", serverID), zap.Error(err))
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	h.logger.Info("Unregistered server via HTTP API", zap.String("server_id", serverID))

	h.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message":   "Server unregistered successfully",
		"server_id": serverID,
	})
}

// handleInvokeServer invokes a method on a specific server
func (h *HTTPHandlers) handleInvokeServer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

	var request MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx, ok := h.invokeIdentity(w, r)
	if !ok {
		return
	}
	if id := identity.FromContext(ctx); id != nil && id.Result == identity.ResultDelegated && !DelegableMethod(request.Method) {
		delegationOutcomes.WithLabelValues("wrong_method").Inc()
		http.Error(w, "the delegate credential may only run tools/list and tools/call", http.StatusForbidden)
		return
	}
	// Set request ID if not provided, and say so: the tool-call event must
	// not use a proxy-minted id as a join key.
	if request.ID == "" {
		request.ID = generateRequestID()
		ctx = WithGeneratedRequestID(ctx)
	}

	response, err := h.manager.InvokeServer(ctx, serverID, &request)
	if err != nil {
		h.logger.Error("Failed to invoke server",
			zap.String("server_id", serverID),
			zap.String("method", request.Method),
			zap.Error(err))
		http.Error(w, err.Error(), h.invokeStatus(err))
		return
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// OnBehalfOfTenantHeader names the tenant an admin-token invoke runs as
// (Plan #22 C.4). aiqg-dashboard-be's Test connection uses it: dashboard-be
// holds no tenant's TAS-Auth token (only their hashes), so it calls with the
// service token and names the tenant whose org owner asked, after checking
// that owner's role itself. The header is ignored without a valid admin token,
// so a tenant can never assert another tenant through it.
const OnBehalfOfTenantHeader = "TAS-MCP-On-Behalf-Of-Tenant"

// invokeIdentity is the caller identity for one invoke: the resolved TAS-Auth
// token, or, for an admin-token call naming a tenant in
// OnBehalfOfTenantHeader, that tenant. A malformed tenant id is a 400.
func (h *HTTPHandlers) invokeIdentity(w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	if h.delegation != nil && HasDelegateHeader(r) {
		return h.delegatedIdentity(w, r)
	}
	t := r.Header.Get(OnBehalfOfTenantHeader)
	p := h.tenancy()
	if t == "" || p == nil || !p.IsAdmin(r) {
		return h.withIdentity(r), true
	}
	if !isUUID(t) {
		http.Error(w, OnBehalfOfTenantHeader+" must be a tenant UUID", http.StatusBadRequest)
		return nil, false
	}
	return identity.WithIdentity(r.Context(), &identity.Identity{
		Attributed: true, Result: identity.ResultOnBehalf, TenantID: strings.ToLower(t),
	}), true
}

// delegatedIdentity authenticates a delegate-credential invoke and verifies,
// through aiqg-dashboard-be, the org, tenant and user it names. It fails
// closed: a bad credential or a refusal is a 403, an unanswerable verify is a
// 503, and in neither case does the call reach a server. The credential is
// never logged.
func (h *HTTPHandlers) delegatedIdentity(w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	d := h.delegation
	if r.Header.Get("TAS-Auth") != "" || r.Header.Get(AdminTokenHeader) != "" {
		delegationOutcomes.WithLabelValues("ambiguous").Inc()
		http.Error(w, "send the delegate credential alone, without TAS-Auth or an admin token", http.StatusBadRequest)
		return nil, false
	}
	if !d.Matches(r.Header.Get(DelegateTokenHeader)) {
		delegationOutcomes.WithLabelValues("bad_token").Inc()
		h.logger.Warn("federation: delegated invoke refused: invalid delegate credential", zap.String("path", r.URL.Path))
		http.Error(w, "invalid "+DelegateTokenHeader, http.StatusForbidden)
		return nil, false
	}
	org := strings.TrimSpace(r.Header.Get(OnBehalfOfOrgHeader))
	user := strings.TrimSpace(r.Header.Get(OnBehalfOfUserHeader))
	tenant := strings.ToLower(strings.TrimSpace(r.Header.Get(OnBehalfOfTenantHeader)))
	if !ValidDelegateID(org) || !ValidDelegateID(user) || (tenant != "" && !isUUID(tenant)) {
		delegationOutcomes.WithLabelValues("bad_headers").Inc()
		http.Error(w, "a delegated call needs "+OnBehalfOfOrgHeader+" and "+OnBehalfOfUserHeader+
			" (and, if sent, a UUID in "+OnBehalfOfTenantHeader+")", http.StatusBadRequest)
		return nil, false
	}
	g, err := d.Verifier.Verify(r.Context(), org, tenant, user)
	switch {
	case err == nil && g != nil:
	case errors.Is(err, ErrDelegationRefused):
		delegationOutcomes.WithLabelValues("refused").Inc()
		code := refusalUnspecified
		var ref *DelegationRefusal
		if errors.As(err, &ref) {
			code = ref.Code
		}
		h.logger.Warn("federation: delegated invoke refused",
			zap.String("delegated_by", d.Name), zap.String("aether_org_id", org), zap.String("code", code))
		http.Error(w, "delegated call refused: "+code, http.StatusForbidden)
		return nil, false
	default:
		delegationOutcomes.WithLabelValues("unavailable").Inc()
		h.logger.Warn("federation: delegated invoke refused: verify unavailable", zap.Error(err))
		http.Error(w, "delegated call could not be verified; retry", http.StatusServiceUnavailable)
		return nil, false
	}
	delegationOutcomes.WithLabelValues("verified").Inc()
	return identity.WithIdentity(r.Context(), &identity.Identity{
		Attributed:  true,
		Result:      identity.ResultDelegated,
		TenantID:    g.TenantID,
		Suspended:   g.Suspended,
		UserID:      g.UserSub,
		DelegatedBy: d.Name,
		AetherOrgID: org,
		SourceApp:   d.Name,
		ToolCallID:  clipHeader(r.Header.Get("TAS-Tool-Call-Id")),
		AgentID:     clipHeader(r.Header.Get("TAS-Agent-Id")),
		FlowID:      clipHeader(r.Header.Get("TAS-Flow-Id")),
		StepID:      clipHeader(r.Header.Get("TAS-Step-Id")),
	}), true
}

// maxDelegatedHeaderLen caps a self-asserted header copied into an event, as
// the identity resolver does.
const maxDelegatedHeaderLen = 256

// clipHeader bounds a self-asserted header copied into an event.
func clipHeader(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxDelegatedHeaderLen {
		return s[:maxDelegatedHeaderLen]
	}
	return s
}

// isUUID reports whether s is a UUID in the 8-4-4-4-12 form.
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil && len(s) == uuidTextLen
}

// uuidTextLen is the length of a UUID's 8-4-4-4-12 text form.
const uuidTextLen = 36

// invokeStatus is 500 for every invoke error, as before C.1, except the
// tenancy refusals, which are 403 when the tenant registry is on.
func (h *HTTPHandlers) invokeStatus(err error) int {
	if errors.Is(err, ErrConnectionUnavailable) {
		// Only produced with MCP_CONNECTION_INJECTION on: retryable.
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, ErrConnectionRequired) {
		// Only produced for MCP_CONNECTION_REQUIRED_SERVERS; not retryable.
		return http.StatusForbidden
	}
	if errors.Is(err, ErrAccountSuspended) {
		// Only produced with MCP_CONNECTION_INJECTION on; the gateway's 403.
		return http.StatusForbidden
	}
	if h.tenancy() == nil {
		return http.StatusInternalServerError
	}
	if st := statusFor(err, 0); st == http.StatusForbidden {
		return st
	}
	return http.StatusInternalServerError
}

// handleCheckHealth performs a health check on a server
func (h *HTTPHandlers) handleCheckHealth(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]
	if !h.serverVisible(r, serverID) {
		http.Error(w, fmt.Sprintf("server with ID %s not found", serverID), http.StatusNotFound)
		return
	}

	err := h.manager.CheckHealth(r.Context(), serverID)

	status := "healthy"
	statusCode := http.StatusOK

	if err != nil {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	}

	h.writeJSONResponse(w, statusCode, map[string]interface{}{
		"server_id": serverID,
		"status":    status,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"error":     formatError(err),
	})
}

// handleBroadcast broadcasts a request to all healthy servers
func (h *HTTPHandlers) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	var request MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.logger.Error("Failed to decode broadcast request", zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set request ID if not provided
	if request.ID == "" {
		request.ID = generateRequestID()
	}

	responses, err := h.manager.BroadcastRequest(h.withIdentity(r), &request)
	if err != nil {
		h.logger.Error("Failed to broadcast request",
			zap.String("method", request.Method),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"request_id": request.ID,
		"responses":  responses,
		"total":      len(responses),
	})
}

// handleGetHealthStatus returns health status of all servers
func (h *HTTPHandlers) handleGetHealthStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.manager.GetHealthStatus()
	if err != nil {
		h.logger.Error("Failed to get health status", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	visible := h.visibleIDs(r)
	if visible != nil {
		for id := range status {
			if !visible[id] {
				delete(status, id)
			}
		}
	}

	resp := map[string]interface{}{
		"status":    status,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	// Probe results (Plan #22 T.4): reachability as a client sees it, which
	// the registry status above does not track.
	if p, ok := h.manager.(ProbeStatusProvider); ok {
		if probes := p.ProbeStatuses(); probes != nil {
			for id := range probes {
				if visible != nil && !visible[id] {
					delete(probes, id)
				}
			}
			resp["probes"] = probes
		}
	}
	h.writeJSONResponse(w, http.StatusOK, resp)
}

// visibleIDs is the set of server ids r may see, nil when it may see all.
func (h *HTTPHandlers) visibleIDs(r *http.Request) map[string]bool {
	keep := h.visibleTo(r)
	if keep == nil {
		return nil
	}
	servers, err := h.manager.ListServers()
	ids := map[string]bool{}
	if err != nil {
		return ids
	}
	for _, s := range servers {
		if keep(s) {
			ids[s.ID] = true
		}
	}
	return ids
}

// handleGetMetrics returns federation metrics
func (h *HTTPHandlers) handleGetMetrics(w http.ResponseWriter, _ *http.Request) {
	manager, ok := h.manager.(*Manager)
	if !ok {
		http.Error(w, "Metrics not available", http.StatusInternalServerError)
		return
	}

	metrics := manager.GetMetrics()
	h.writeJSONResponse(w, http.StatusOK, metrics)
}

// handleListServersByCategory returns servers filtered by category
func (h *HTTPHandlers) handleListServersByCategory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	category := vars["category"]

	servers, err := h.manager.ListServersByCategory(category)
	if err != nil {
		h.logger.Error("Failed to list servers by category",
			zap.String("category", category),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	servers = filterServers(servers, h.visibleTo(r))

	h.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"category": category,
		"servers":  servers,
		"total":    len(servers),
	})
}

// writeJSONResponse writes a JSON response
func (h *HTTPHandlers) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", zap.Error(err))
	}
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// formatError formats an error for JSON response
func formatError(err error) interface{} {
	if err == nil {
		return nil
	}
	return err.Error()
}
