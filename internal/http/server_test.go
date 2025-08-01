package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/forwarding"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
	"go.uber.org/zap"
)

// Mock MCP Server for testing
type mockMCPServer struct {
	ingestEventFunc func(*mcpv1.IngestEventRequest) (*mcpv1.IngestEventResponse, error)
	getStatsFunc    func() *grpcserver.ServerStats
}

func (m *mockMCPServer) IngestEvent(req *mcpv1.IngestEventRequest) (*mcpv1.IngestEventResponse, error) {
	if m.ingestEventFunc != nil {
		return m.ingestEventFunc(req)
	}
	return &mcpv1.IngestEventResponse{
		EventId: req.EventId,
		Status:  "accepted",
	}, nil
}

func (m *mockMCPServer) GetStats() *grpcserver.ServerStats {
	if m.getStatsFunc != nil {
		return m.getStatsFunc()
	}
	return &grpcserver.ServerStats{
		TotalEvents:     100,
		StreamEvents:    50,
		ForwardedEvents: 75,
		ErrorEvents:     5,
		ActiveStreams:   3,
		StartTime:       time.Now().Add(-time.Hour),
	}
}

// Mock Event Forwarder for testing
type mockForwarder struct {
	getTargetsFunc  func() map[string]*forwarding.ForwardingTarget
	addTargetFunc   func(*forwarding.ForwardingTarget) error
	removeTargetFunc func(string) error
	getMetricsFunc  func() *forwarding.ForwardingMetrics
}

func (m *mockForwarder) GetTargets() map[string]*forwarding.ForwardingTarget {
	if m.getTargetsFunc != nil {
		return m.getTargetsFunc()
	}
	return make(map[string]*forwarding.ForwardingTarget)
}

func (m *mockForwarder) AddTarget(target *forwarding.ForwardingTarget) error {
	if m.addTargetFunc != nil {
		return m.addTargetFunc(target)
	}
	return nil
}

func (m *mockForwarder) RemoveTarget(id string) error {
	if m.removeTargetFunc != nil {
		return m.removeTargetFunc(id)
	}
	return nil
}

func (m *mockForwarder) GetMetrics() *forwarding.ForwardingMetrics {
	if m.getMetricsFunc != nil {
		return m.getMetricsFunc()
	}
	return &forwarding.ForwardingMetrics{
		TotalEvents:     200,
		ForwardedEvents: 180,
		FailedEvents:    10,
		DroppedEvents:   5,
	}
}

func TestNewServer(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	forwarder := &mockForwarder{}

	server := NewServer(logger, mcpServer, forwarder)

	if server == nil {
		t.Error("NewServer() returned nil")
	}
	if server.version != "1.0.0" {
		t.Errorf("Server version = %s, want 1.0.0", server.version)
	}
}

func TestHandleIngestEvent(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil)

	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "valid event",
			requestBody: EventRequest{
				EventID:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{"test": "data"}`,
				Metadata:  map[string]string{"key": "value"},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `{"event_id":"test-123","status":"accepted"}`,
		},
		{
			name: "missing event_id",
			requestBody: EventRequest{
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{"test": "data"}`,
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing event_type",
			requestBody: EventRequest{
				EventID: "test-123",
				Source:  "test-source",
				Data:    `{"test": "data"}`,
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing source",
			requestBody: EventRequest{
				EventID:   "test-123",
				EventType: "test.event",
				Data:      `{"test": "data"}`,
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing data",
			requestBody: EventRequest{
				EventID:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			
			rr := httptest.NewRecorder()
			server.handleIngestEvent(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("handleIngestEvent() status = %d, want %d", rr.Code, tt.expectedStatus)
			}

			if tt.expectedBody != "" {
				if rr.Body.String() != tt.expectedBody+"\n" {
					t.Errorf("handleIngestEvent() body = %s, want %s", rr.Body.String(), tt.expectedBody)
				}
			}
		})
	}
}

func TestHandleBatchIngestEvents(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil)

	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
		expectedCount  int
	}{
		{
			name: "valid batch",
			requestBody: []EventRequest{
				{
					EventID:   "test-1",
					EventType: "test.event",
					Source:    "test-source",
					Data:      `{"test": "data1"}`,
				},
				{
					EventID:   "test-2",
					EventType: "test.event",
					Source:    "test-source",
					Data:      `{"test": "data2"}`,
				},
			},
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:           "empty batch",
			requestBody:    []EventRequest{},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid json",
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			
			rr := httptest.NewRecorder()
			server.handleBatchIngestEvents(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("handleBatchIngestEvents() status = %d, want %d", rr.Code, tt.expectedStatus)
			}

			if tt.expectedStatus == http.StatusOK {
				var response map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Errorf("Failed to unmarshal response: %v", err)
				}
				
				if processed, ok := response["processed"].(float64); !ok || int(processed) != tt.expectedCount {
					t.Errorf("Expected processed count = %d, got %v", tt.expectedCount, response["processed"])
				}
			}
		})
	}
}

func TestHandleHealth(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to unmarshal health response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("Health status = %s, want healthy", response.Status)
	}
	if response.Version != "1.0.0" {
		t.Errorf("Health version = %s, want 1.0.0", response.Version)
	}
}

func TestHandleReady(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()

	server.handleReady(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleReady() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to unmarshal ready response: %v", err)
	}

	if response["status"] != "ready" {
		t.Errorf("Ready status = %s, want ready", response["status"])
	}
}

func TestHandleListTargets(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	
	// Mock forwarder with test targets
	forwarder := &mockForwarder{
		getTargetsFunc: func() map[string]*forwarding.ForwardingTarget {
			return map[string]*forwarding.ForwardingTarget{
				"target-1": {
					ID:       "target-1",
					Name:     "Test Target 1",
					Type:     "http",
					Endpoint: "http://example.com",
				},
			}
		},
	}
	
	server := NewServer(logger, mcpServer, forwarder)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/forwarding/targets", nil)
	rr := httptest.NewRecorder()

	server.handleListTargets(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleListTargets() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]*forwarding.ForwardingTarget
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to unmarshal targets response: %v", err)
	}

	if len(response) != 1 {
		t.Errorf("Number of targets = %d, want 1", len(response))
	}

	if target, exists := response["target-1"]; !exists {
		t.Error("Expected target-1 not found")
	} else if target.Name != "Test Target 1" {
		t.Errorf("Target name = %s, want Test Target 1", target.Name)
	}
}

func TestHandleListTargets_NoForwarder(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil) // No forwarder

	req := httptest.NewRequest(http.MethodGet, "/api/v1/forwarding/targets", nil)
	rr := httptest.NewRecorder()

	server.handleListTargets(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("handleListTargets() status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestCorsMiddleware(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil)

	// Test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with CORS middleware
	corsHandler := server.corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	rr := httptest.NewRecorder()

	corsHandler.ServeHTTP(rr, req)

	// Check CORS headers
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Access-Control-Allow-Origin header not set correctly")
	}
	if rr.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Access-Control-Allow-Methods header not set")
	}
	if rr.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("Access-Control-Allow-Headers header not set")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("OPTIONS request status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRouterSetup(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil)

	handler := server.Handler()
	router, ok := handler.(*mux.Router)
	if !ok {
		t.Error("Handler is not a mux.Router")
	}

	// Test that routes are set up correctly by making requests
	testRoutes := []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/ready", http.StatusOK},
		{http.MethodPost, "/api/v1/events", http.StatusBadRequest}, // Bad request due to empty body
		{http.MethodGet, "/api/v1/forwarding/targets", http.StatusServiceUnavailable}, // No forwarder
	}

	for _, route := range testRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != route.status {
				t.Errorf("Route %s %s status = %d, want %d", route.method, route.path, rr.Code, route.status)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := &mockMCPServer{}
	server := NewServer(logger, mcpServer, nil)

	// Test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap with logging middleware
	loggingHandler := server.loggingMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	loggingHandler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Logging middleware status = %d, want %d", rr.Code, http.StatusOK)
	}

	if rr.Body.String() != "test response" {
		t.Errorf("Response body = %s, want test response", rr.Body.String())
	}
}