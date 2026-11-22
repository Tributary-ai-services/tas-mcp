package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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
func (h *HTTPHandlers) handleListServers(w http.ResponseWriter, _ *http.Request) {
	servers, err := h.manager.ListServers()
	if err != nil {
		h.logger.Error("Failed to list servers", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
	var server MCPServer
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		h.logger.Error("Failed to decode server", zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := h.manager.RegisterServer(&server); err != nil {
		h.logger.Error("Failed to register server", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	h.writeJSONResponse(w, http.StatusOK, h.view(server))
}

// handleUnregisterServer removes a server
func (h *HTTPHandlers) handleUnregisterServer(w http.ResponseWriter, r *http.Request) {
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

	ctx := h.withIdentity(r)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// handleCheckHealth performs a health check on a server
func (h *HTTPHandlers) handleCheckHealth(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

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
func (h *HTTPHandlers) handleGetHealthStatus(w http.ResponseWriter, _ *http.Request) {
	status, err := h.manager.GetHealthStatus()
	if err != nil {
		h.logger.Error("Failed to get health status", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"status":    status,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	// Probe results (Plan #22 T.4): reachability as a client sees it, which
	// the registry status above does not track.
	if p, ok := h.manager.(ProbeStatusProvider); ok {
		if probes := p.ProbeStatuses(); probes != nil {
			resp["probes"] = probes
		}
	}
	h.writeJSONResponse(w, http.StatusOK, resp)
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
