//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
	"github.com/tributary-ai-services/tas-mcp/internal/forwarding"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
	httpserver "github.com/tributary-ai-services/tas-mcp/internal/http"
	"github.com/tributary-ai-services/tas-mcp/internal/logger"
)

// TestServer represents a test instance of the MCP server
type TestServer struct {
	grpcServer *grpcserver.MCPServer
	httpServer *httpserver.Server
	forwarder  *forwarding.EventForwarder
	httpTS     *httptest.Server
	grpcConn   *grpc.ClientConn
	grpcClient mcpv1.MCPServiceClient
}

// NewTestServer creates a new test server instance
func NewTestServer(t *testing.T) *TestServer {
	t.Helper()

	// Create logger
	zapLogger, err := logger.NewLogger("debug")
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create forwarder
	forwardingConfig := &config.ForwardingConfig{
		Enabled:             true,
		DefaultRetryAttempts: 3,
		DefaultTimeout:      5 * time.Second,
		BufferSize:          100,
		Workers:             2,
		Targets: []*config.TargetConfiguration{},
	}
	
	forwarder := forwarding.NewEventForwarder(zapLogger, forwardingConfig)
	if err := forwarder.Start(); err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}

	// Create gRPC server
	grpcServer := grpcserver.NewMCPServer(zapLogger, forwarder)

	// Create HTTP server
	httpServer := httpserver.NewServer(zapLogger, grpcServer, forwarder)

	// Create HTTP test server
	httpTS := httptest.NewServer(httpServer.Handler())

	// Create gRPC client connection (we'll use a mock for integration tests)
	return &TestServer{
		grpcServer: grpcServer,
		httpServer: httpServer,
		forwarder:  forwarder,
		httpTS:     httpTS,
	}
}

// Close cleans up the test server
func (ts *TestServer) Close() {
	if ts.grpcConn != nil {
		ts.grpcConn.Close()
	}
	if ts.httpTS != nil {
		ts.httpTS.Close()
	}
	if ts.forwarder != nil {
		ts.forwarder.Stop()
	}
}

func TestHTTPEventIngestion(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()

	tests := []struct {
		name           string
		event          map[string]interface{}
		expectedStatus int
	}{
		{
			name: "valid single event",
			event: map[string]interface{}{
				"event_id":   "test-http-1",
				"event_type": "test.http.event",
				"source":     "integration-test",
				"data":       `{"message": "test event"}`,
				"metadata": map[string]string{
					"test": "true",
				},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "event with complex data",
			event: map[string]interface{}{
				"event_id":   "test-http-2",
				"event_type": "user.registration",
				"source":     "auth-service",
				"data":       `{"user_id": "123", "email": "test@example.com", "preferences": {"notifications": true}}`,
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.event)
			resp, err := http.Post(
				ts.httpTS.URL+"/api/v1/events",
				"application/json",
				bytes.NewReader(body),
			)
			if err != nil {
				t.Fatalf("Failed to make HTTP request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Status code = %d, want %d", resp.StatusCode, tt.expectedStatus)
			}

			var response map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				t.Errorf("Failed to decode response: %v", err)
			}

			if eventID, ok := response["event_id"].(string); !ok || eventID != tt.event["event_id"] {
				t.Errorf("Response event_id = %v, want %v", response["event_id"], tt.event["event_id"])
			}

			if status, ok := response["status"].(string); !ok || status != "accepted" {
				t.Errorf("Response status = %v, want accepted", response["status"])
			}
		})
	}
}

func TestHTTPBatchEventIngestion(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()

	events := []map[string]interface{}{
		{
			"event_id":   "batch-1",
			"event_type": "batch.test.1",
			"source":     "batch-test",
			"data":       `{"index": 1}`,
		},
		{
			"event_id":   "batch-2",
			"event_type": "batch.test.2",
			"source":     "batch-test",
			"data":       `{"index": 2}`,
		},
		{
			"event_id":   "batch-3",
			"event_type": "batch.test.3",
			"source":     "batch-test",
			"data":       `{"index": 3}`,
		},
	}

	body, _ := json.Marshal(events)
	resp, err := http.Post(
		ts.httpTS.URL+"/api/v1/events/batch",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to make HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if processed, ok := response["processed"].(float64); !ok || int(processed) != len(events) {
		t.Errorf("Processed count = %v, want %d", response["processed"], len(events))
	}

	if results, ok := response["results"].([]interface{}); !ok || len(results) != len(events) {
		t.Errorf("Results count = %v, want %d", len(results), len(events))
	}
}

func TestHealthEndpoints(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()

	tests := []struct {
		name     string
		endpoint string
		status   int
	}{
		{
			name:     "health check",
			endpoint: "/health",
			status:   http.StatusOK,
		},
		{
			name:     "readiness check",
			endpoint: "/ready",
			status:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.httpTS.URL + tt.endpoint)
			if err != nil {
				t.Fatalf("Failed to make HTTP request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.status {
				t.Errorf("Status code = %d, want %d", resp.StatusCode, tt.status)
			}

			var response map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				t.Errorf("Failed to decode response: %v", err)
			}

			// Verify response structure
			if tt.endpoint == "/health" {
				if status, ok := response["status"].(string); !ok || status != "healthy" {
					t.Errorf("Health status = %v, want healthy", response["status"])
				}
			}

			if tt.endpoint == "/ready" {
				if status, ok := response["status"].(string); !ok || status != "ready" {
					t.Errorf("Ready status = %v, want ready", response["status"])
				}
			}
		})
	}
}

func TestEventForwardingIntegration(t *testing.T) {
	// Create a mock target server
	targetEvents := make([]*mcpv1.Event, 0)
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event mcpv1.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("Failed to decode forwarded event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		
		targetEvents = append(targetEvents, &event)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "received"}`))
	}))
	defer targetServer.Close()

	// Create test server with forwarding target
	zapLogger, _ := logger.NewLogger("debug")
	forwardingConfig := &config.ForwardingConfig{
		Enabled:             true,
		DefaultRetryAttempts: 3,
		DefaultTimeout:      5 * time.Second,
		BufferSize:          100,
		Workers:             2,
		Targets: []*config.TargetConfiguration{
			{
				ID:       "test-target",
				Name:     "Test HTTP Target",
				Type:     "http",
				Endpoint: targetServer.URL,
				Config: &config.TargetConfig{
					Timeout:       5 * time.Second,
					RetryAttempts: 2,
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
				},
				Rules: []*config.ForwardingRule{
					{
						ID:      "forward-all",
						Enabled: true,
						Conditions: []*config.RuleCondition{
							{
								Field:    "event_type",
								Operator: "contains",
								Value:    "forward",
							},
						},
					},
				},
			},
		},
	}

	forwarder := forwarding.NewEventForwarder(zapLogger, forwardingConfig)
	forwarder.Start()
	defer forwarder.Stop()

	grpcServer := grpcserver.NewMCPServer(zapLogger, forwarder)
	httpServer := httpserver.NewServer(zapLogger, grpcServer, forwarder)
	httpTS := httptest.NewServer(httpServer.Handler())
	defer httpTS.Close()

	// Send an event that should be forwarded
	event := map[string]interface{}{
		"event_id":   "forward-test-1",
		"event_type": "test.forward.event",
		"source":     "integration-test",
		"data":       `{"should_forward": true}`,
	}

	body, _ := json.Marshal(event)
	resp, err := http.Post(
		httpTS.URL+"/api/v1/events",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to make HTTP request: %v", err)
	}
	resp.Body.Close()

	// Wait for forwarding to complete
	time.Sleep(100 * time.Millisecond)

	// Verify event was forwarded
	if len(targetEvents) != 1 {
		t.Errorf("Number of forwarded events = %d, want 1", len(targetEvents))
	}

	if len(targetEvents) > 0 {
		forwardedEvent := targetEvents[0]
		if forwardedEvent.EventId != event["event_id"] {
			t.Errorf("Forwarded event ID = %s, want %s", forwardedEvent.EventId, event["event_id"])
		}
		if forwardedEvent.EventType != event["event_type"] {
			t.Errorf("Forwarded event type = %s, want %s", forwardedEvent.EventType, event["event_type"])
		}
	}
}

func TestGRPCEventIngestion(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()

	// For this test, we'll directly test the gRPC server methods
	// since setting up a full gRPC server in integration tests is complex

	ctx := context.Background()
	req := &mcpv1.IngestEventRequest{
		EventId:   "grpc-test-1",
		EventType: "test.grpc.event",
		Source:    "grpc-integration-test",
		Timestamp: time.Now().Unix(),
		Data:      `{"grpc": true}`,
		Metadata: map[string]string{
			"protocol": "grpc",
		},
	}

	resp, err := ts.grpcServer.IngestEvent(ctx, req)
	if err != nil {
		t.Errorf("IngestEvent() error = %v", err)
	}

	if resp.EventId != req.EventId {
		t.Errorf("Response EventId = %s, want %s", resp.EventId, req.EventId)
	}
	if resp.Status != "accepted" {
		t.Errorf("Response Status = %s, want accepted", resp.Status)
	}

	// Verify stats were updated
	stats := ts.grpcServer.GetStats()
	if stats.TotalEvents == 0 {
		t.Error("TotalEvents should be greater than 0")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()

	// Send some events first to generate metrics
	events := []map[string]interface{}{
		{
			"event_id":   "metrics-test-1",
			"event_type": "metrics.test",
			"source":     "metrics-test",
			"data":       `{"metrics": true}`,
		},
		{
			"event_id":   "metrics-test-2",
			"event_type": "metrics.test",
			"source":     "metrics-test",
			"data":       `{"metrics": true}`,
		},
	}

	for _, event := range events {
		body, _ := json.Marshal(event)
		resp, err := http.Post(
			ts.httpTS.URL+"/api/v1/events",
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			t.Fatalf("Failed to send test event: %v", err)
		}
		resp.Body.Close()
	}

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Check metrics endpoint
	resp, err := http.Get(ts.httpTS.URL + "/api/v1/metrics")
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Metrics endpoint status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// The metrics endpoint returns Prometheus format, so we just verify it's not empty
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if buf.Len() == 0 {
		t.Error("Metrics response should not be empty")
	}

	// Check stats endpoint
	resp, err = http.Get(ts.httpTS.URL + "/api/v1/stats")
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Stats endpoint status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Errorf("Failed to decode stats response: %v", err)
	}

	if totalEvents, ok := stats["total_events"].(float64); !ok || totalEvents < 2 {
		t.Errorf("Total events = %v, want >= 2", stats["total_events"])
	}
}

func TestConcurrentEventIngestion(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()

	numGoroutines := 10
	eventsPerGoroutine := 5
	done := make(chan bool, numGoroutines)

	// Send events concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for j := 0; j < eventsPerGoroutine; j++ {
				event := map[string]interface{}{
					"event_id":   fmt.Sprintf("concurrent-%d-%d", goroutineID, j),
					"event_type": "concurrent.test",
					"source":     "concurrent-test",
					"data":       fmt.Sprintf(`{"goroutine": %d, "event": %d}`, goroutineID, j),
				}

				body, _ := json.Marshal(event)
				resp, err := http.Post(
					ts.httpTS.URL+"/api/v1/events",
					"application/json",
					bytes.NewReader(body),
				)
				if err != nil {
					t.Errorf("Failed to send event: %v", err)
					return
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Event status = %d, want %d", resp.StatusCode, http.StatusOK)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all events were processed
	stats := ts.grpcServer.GetStats()
	expectedEvents := int64(numGoroutines * eventsPerGoroutine)
	if stats.TotalEvents < expectedEvents {
		t.Errorf("Total events = %d, want >= %d", stats.TotalEvents, expectedEvents)
	}
}