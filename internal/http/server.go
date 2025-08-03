// Package http provides HTTP server functionality for the TAS MCP server.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/forwarding"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
)

// Constants for the HTTP server
const (
	// DefaultVersion is the default version string
	DefaultVersion = "1.0.0"
	// MaxEventsLimit is the maximum number of events to return in a single request
	MaxEventsLimit = 1000
)

// Server holds the HTTP server configuration
type Server struct {
	log       *zap.Logger
	mcpServer *grpcserver.MCPServer
	forwarder *forwarding.EventForwarder
	version   string
	startTime time.Time
}

// NewServer creates a new HTTP server instance
func NewServer(log *zap.Logger, mcpServer *grpcserver.MCPServer, forwarder *forwarding.EventForwarder) *Server {
	return &Server{
		log:       log,
		mcpServer: mcpServer,
		forwarder: forwarder,
		version:   DefaultVersion,
		startTime: time.Now(),
	}
}

// EventRequest represents the HTTP request structure for MCP events
type EventRequest struct {
	EventID   string            `json:"event_id"`
	EventType string            `json:"event_type"`
	Source    string            `json:"source"`
	Timestamp int64             `json:"timestamp"`
	Data      string            `json:"data"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string                  `json:"status"`
	Timestamp time.Time               `json:"timestamp"`
	Version   string                  `json:"version"`
	Uptime    string                  `json:"uptime"`
	Stats     *grpcserver.ServerStats `json:"stats,omitempty"`
}

// Handler returns the HTTP handler with all routes
func (s *Server) Handler() http.Handler {
	r := mux.NewRouter()

	// API routes
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(s.loggingMiddleware)
	api.Use(s.corsMiddleware)

	// Event ingestion endpoints
	api.HandleFunc("/events", s.handleIngestEvent).Methods("POST")
	api.HandleFunc("/events/batch", s.handleBatchIngestEvents).Methods("POST")

	// Forwarding management endpoints
	api.HandleFunc("/forwarding/targets", s.handleListTargets).Methods("GET")
	api.HandleFunc("/forwarding/targets", s.handleAddTarget).Methods("POST")
	api.HandleFunc("/forwarding/targets/{id}", s.handleGetTarget).Methods("GET")
	api.HandleFunc("/forwarding/targets/{id}", s.handleUpdateTarget).Methods("PUT")
	api.HandleFunc("/forwarding/targets/{id}", s.handleDeleteTarget).Methods("DELETE")
	api.HandleFunc("/forwarding/metrics", s.handleForwardingMetrics).Methods("GET")

	// Server management endpoints
	api.HandleFunc("/metrics", s.handleMetrics).Methods("GET")
	api.HandleFunc("/stats", s.handleStats).Methods("GET")

	// Health check endpoints
	r.HandleFunc("/health", s.handleHealth).Methods("GET")
	r.HandleFunc("/ready", s.handleReady).Methods("GET")

	// Legacy MCP endpoint for backward compatibility
	r.HandleFunc("/mcp", s.handleLegacyMCPEvent).Methods("POST")

	return r
}

// Middleware
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(lrw, r)

		s.log.Info("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
			zap.Int("status", lrw.statusCode),
			zap.Duration("duration", time.Since(start)))
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Event ingestion handlers
func (s *Server) handleIngestEvent(w http.ResponseWriter, r *http.Request) {
	var eventReq EventRequest
	if err := json.NewDecoder(r.Body).Decode(&eventReq); err != nil {
		s.log.Error("Failed to decode event request", zap.Error(err))
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if eventReq.EventID == "" {
		http.Error(w, "event_id is required", http.StatusBadRequest)
		return
	}
	if eventReq.EventType == "" {
		http.Error(w, "event_type is required", http.StatusBadRequest)
		return
	}
	if eventReq.Source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	if eventReq.Data == "" {
		http.Error(w, "data is required", http.StatusBadRequest)
		return
	}

	// Set timestamp if not provided
	if eventReq.Timestamp == 0 {
		eventReq.Timestamp = time.Now().Unix()
	}

	// Create gRPC request
	req := &mcpv1.IngestEventRequest{
		EventId:   eventReq.EventID,
		EventType: eventReq.EventType,
		Source:    eventReq.Source,
		Timestamp: eventReq.Timestamp,
		Data:      eventReq.Data,
		Metadata:  eventReq.Metadata,
	}

	// Call gRPC service
	resp, err := s.mcpServer.IngestEvent(r.Context(), req)
	if err != nil {
		s.log.Error("Failed to ingest event", zap.Error(err))
		http.Error(w, "Failed to process event", http.StatusInternalServerError)
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"event_id": resp.EventId,
		"status":   resp.Status,
	})
}

func (s *Server) handleBatchIngestEvents(w http.ResponseWriter, r *http.Request) {
	var events []EventRequest
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if len(events) == 0 {
		http.Error(w, "No events provided", http.StatusBadRequest)
		return
	}

	if len(events) > MaxEventsLimit {
		http.Error(w, "Too many events (max 1000)", http.StatusRequestEntityTooLarge)
		return
	}

	results := make([]map[string]interface{}, 0, len(events))

	for _, eventReq := range events {
		// Set timestamp if not provided
		if eventReq.Timestamp == 0 {
			eventReq.Timestamp = time.Now().Unix()
		}

		req := &mcpv1.IngestEventRequest{
			EventId:   eventReq.EventID,
			EventType: eventReq.EventType,
			Source:    eventReq.Source,
			Timestamp: eventReq.Timestamp,
			Data:      eventReq.Data,
			Metadata:  eventReq.Metadata,
		}

		resp, err := s.mcpServer.IngestEvent(r.Context(), req)
		if err != nil {
			results = append(results, map[string]interface{}{
				"event_id": eventReq.EventID,
				"status":   "error",
				"error":    err.Error(),
			})
		} else {
			results = append(results, map[string]interface{}{
				"event_id": resp.EventId,
				"status":   resp.Status,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"processed": len(results),
		"results":   results,
	})
}

// Forwarding management handlers
func (s *Server) handleListTargets(w http.ResponseWriter, _ *http.Request) {
	if s.forwarder == nil {
		http.Error(w, "Forwarding not enabled", http.StatusServiceUnavailable)
		return
	}

	targets := s.forwarder.GetTargets()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(targets)
}

func (s *Server) handleAddTarget(w http.ResponseWriter, r *http.Request) {
	if s.forwarder == nil {
		http.Error(w, "Forwarding not enabled", http.StatusServiceUnavailable)
		return
	}

	var target forwarding.ForwardingTarget
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if err := s.forwarder.AddTarget(&target); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "created",
		"target_id": target.ID,
	})
}

func (s *Server) handleGetTarget(w http.ResponseWriter, r *http.Request) {
	if s.forwarder == nil {
		http.Error(w, "Forwarding not enabled", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	targetID := vars["id"]

	targets := s.forwarder.GetTargets()
	target, exists := targets[targetID]
	if !exists {
		http.Error(w, "Target not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(target)
}

func (s *Server) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	if s.forwarder == nil {
		http.Error(w, "Forwarding not enabled", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	targetID := vars["id"]

	var target forwarding.ForwardingTarget
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	target.ID = targetID

	// Remove existing target and add updated one
	_ = s.forwarder.RemoveTarget(targetID)
	if err := s.forwarder.AddTarget(&target); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "updated",
		"target_id": target.ID,
	})
}

func (s *Server) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	if s.forwarder == nil {
		http.Error(w, "Forwarding not enabled", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	targetID := vars["id"]

	if err := s.forwarder.RemoveTarget(targetID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "deleted",
		"target_id": targetID,
	})
}

func (s *Server) handleForwardingMetrics(w http.ResponseWriter, _ *http.Request) {
	if s.forwarder == nil {
		http.Error(w, "Forwarding not enabled", http.StatusServiceUnavailable)
		return
	}

	metrics := s.forwarder.GetMetrics()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(metrics)
}

// Server management handlers
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	resp, err := s.mcpServer.GetMetrics(r.Context(), &mcpv1.MetricsRequest{})
	if err != nil {
		http.Error(w, "Failed to get metrics", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	stats := s.mcpServer.GetStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// Health check handlers
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	stats := s.mcpServer.GetStats()

	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   s.version,
		Uptime:    time.Since(s.startTime).String(),
		Stats:     stats,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// Legacy handler for backward compatibility
func (s *Server) handleLegacyMCPEvent(w http.ResponseWriter, r *http.Request) {
	var legacyReq struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&legacyReq); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Convert to new format
	eventReq := EventRequest{
		EventID:   legacyReq.ID,
		EventType: "legacy.event",
		Source:    "legacy-api",
		Timestamp: time.Now().Unix(),
		Data:      legacyReq.Data,
	}

	// Create gRPC request
	req := &mcpv1.IngestEventRequest{
		EventId:   eventReq.EventID,
		EventType: eventReq.EventType,
		Source:    eventReq.Source,
		Timestamp: eventReq.Timestamp,
		Data:      eventReq.Data,
		Metadata:  eventReq.Metadata,
	}

	resp, err := s.mcpServer.IngestEvent(r.Context(), req)
	if err != nil {
		http.Error(w, "Failed to process event", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": resp.Status})
}
