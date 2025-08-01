package grpc

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
)

// Mock Event Forwarder for testing - we'll use nil for most tests
// and create a real forwarder when needed

func TestNewMCPServer(t *testing.T) {
	logger := zap.NewNop()

	server := NewMCPServer(logger, nil)

	if server == nil {
		t.Fatal("NewMCPServer() returned nil")
	}
	if server.log != logger {
		t.Error("Logger not set correctly")
	}
	if server.eventChannel == nil {
		t.Error("Event channel not initialized")
	}
	if server.streams == nil {
		t.Error("Streams map not initialized")
	}
	if server.stats == nil {
		t.Error("Stats not initialized")
	}
}

func TestIngestEvent_ValidRequest(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	req := &mcpv1.IngestEventRequest{
		EventId:   "test-123",
		EventType: "test.event",
		Source:    "test-source",
		Timestamp: time.Now().Unix(),
		Data:      `{"test": "data"}`,
		Metadata:  map[string]string{"key": "value"},
	}

	resp, err := server.IngestEvent(context.Background(), req)

	if err != nil {
		t.Errorf("IngestEvent() error = %v", err)
	}
	if resp == nil {
		t.Fatal("IngestEvent() returned nil response")
	}
	if resp.EventId != req.EventId {
		t.Errorf("Response EventId = %s, want %s", resp.EventId, req.EventId)
	}
	if resp.Status != AcceptedStatus {
		t.Errorf("Response Status = %s, want %s", resp.Status, AcceptedStatus)
	}

	// Check stats were updated
	stats := server.GetStats()
	if stats.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1", stats.TotalEvents)
	}
}

func TestIngestEvent_InvalidRequests(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	tests := []struct {
		name        string
		request     *mcpv1.IngestEventRequest
		expectedErr codes.Code
	}{
		{
			name:        "nil request",
			request:     nil,
			expectedErr: codes.InvalidArgument,
		},
		{
			name: "empty event_id",
			request: &mcpv1.IngestEventRequest{
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{"test": "data"}`,
			},
			expectedErr: codes.InvalidArgument,
		},
		{
			name: "empty event_type",
			request: &mcpv1.IngestEventRequest{
				EventId: "test-123",
				Source:  "test-source",
				Data:    `{"test": "data"}`,
			},
			expectedErr: codes.InvalidArgument,
		},
		{
			name: "empty source",
			request: &mcpv1.IngestEventRequest{
				EventId:   "test-123",
				EventType: "test.event",
				Data:      `{"test": "data"}`,
			},
			expectedErr: codes.InvalidArgument,
		},
		{
			name: "empty data",
			request: &mcpv1.IngestEventRequest{
				EventId:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
			},
			expectedErr: codes.InvalidArgument,
		},
		{
			name: "invalid json data",
			request: &mcpv1.IngestEventRequest{
				EventId:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{invalid json}`,
			},
			expectedErr: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.IngestEvent(context.Background(), tt.request)

			if err == nil {
				t.Error("IngestEvent() should return error for invalid request")
			}

			if status.Code(err) != tt.expectedErr {
				t.Errorf("Error code = %v, want %v", status.Code(err), tt.expectedErr)
			}

			// Check error stats were updated
			stats := server.GetStats()
			if stats.ErrorEvents == 0 {
				t.Error("ErrorEvents should be incremented for invalid request")
			}
		})

		// Reset stats for next test
		server.stats.ErrorEvents = 0
	}
}

func TestIngestEvent_WithForwarder(t *testing.T) {
	logger := zap.NewNop()
	// Test with nil forwarder - this tests the nil check in the server
	server := NewMCPServer(logger, nil)

	req := &mcpv1.IngestEventRequest{
		EventId:   "test-forwarded",
		EventType: "test.forward",
		Source:    "test-source",
		Data:      `{"test": "forwarded"}`,
	}

	resp, err := server.IngestEvent(context.Background(), req)

	if err != nil {
		t.Errorf("IngestEvent() error = %v", err)
	}
	if resp.Status != "accepted" {
		t.Errorf("Response Status = %s, want accepted", resp.Status)
	}

	// With nil forwarder, no events should be forwarded but server should still work
	stats := server.GetStats()
	if stats.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1", stats.TotalEvents)
	}
}

func TestGetHealth(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	// Add a small delay to ensure uptime is measurable
	time.Sleep(time.Millisecond)

	req := &mcpv1.HealthCheckRequest{}
	resp, err := server.GetHealth(context.Background(), req)

	if err != nil {
		t.Errorf("GetHealth() error = %v", err)
	}
	if resp == nil {
		t.Fatal("GetHealth() returned nil response")
	}
	if resp.Status != HealthyStatus {
		t.Errorf("Health status = %s, want %s", resp.Status, HealthyStatus)
	}
	if resp.Uptime <= 0 {
		t.Errorf("Uptime should be greater than 0, got %d", resp.Uptime)
	}
	if resp.Details == nil {
		t.Error("Health details should not be nil")
	}
}

func TestGetMetrics(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	// Generate some test data
	server.stats.TotalEvents = 100
	server.stats.StreamEvents = 50
	server.stats.ForwardedEvents = 75
	server.stats.ErrorEvents = 5
	server.stats.ActiveStreams = 3

	req := &mcpv1.MetricsRequest{}
	resp, err := server.GetMetrics(context.Background(), req)

	if err != nil {
		t.Errorf("GetMetrics() error = %v", err)
	}
	if resp == nil {
		t.Fatal("GetMetrics() returned nil response")
	}
	if resp.TotalEvents != 100 {
		t.Errorf("TotalEvents = %d, want 100", resp.TotalEvents)
	}
	if resp.StreamEvents != 50 {
		t.Errorf("StreamEvents = %d, want 50", resp.StreamEvents)
	}
	if resp.ForwardedEvents != 75 {
		t.Errorf("ForwardedEvents = %d, want 75", resp.ForwardedEvents)
	}
	if resp.ErrorEvents != 5 {
		t.Errorf("ErrorEvents = %d, want 5", resp.ErrorEvents)
	}
	if resp.ActiveStreams != 3 {
		t.Errorf("ActiveStreams = %d, want 3", resp.ActiveStreams)
	}
}

func TestValidateIngestRequest(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	tests := []struct {
		name    string
		request *mcpv1.IngestEventRequest
		wantErr bool
	}{
		{
			name: "valid request",
			request: &mcpv1.IngestEventRequest{
				EventId:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{"test": "data"}`,
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			request: nil,
			wantErr: true,
		},
		{
			name: "empty event_id",
			request: &mcpv1.IngestEventRequest{
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{"test": "data"}`,
			},
			wantErr: true,
		},
		{
			name: "invalid json",
			request: &mcpv1.IngestEventRequest{
				EventId:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{invalid}`,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := server.validateIngestRequest(tt.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIngestRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEvent(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	tests := []struct {
		name    string
		event   *mcpv1.Event
		wantErr bool
	}{
		{
			name: "valid event",
			event: &mcpv1.Event{
				EventId:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{"test": "data"}`,
			},
			wantErr: false,
		},
		{
			name:    "nil event",
			event:   nil,
			wantErr: true,
		},
		{
			name: "empty event_id",
			event: &mcpv1.Event{
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{"test": "data"}`,
			},
			wantErr: true,
		},
		{
			name: "invalid json data",
			event: &mcpv1.Event{
				EventId:   "test-123",
				EventType: "test.event",
				Source:    "test-source",
				Data:      `{invalid json}`,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := server.validateEvent(tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEvent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetStats(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	// Set some test data
	server.stats.TotalEvents = 200
	server.stats.StreamEvents = 100
	server.stats.ForwardedEvents = 150
	server.stats.ErrorEvents = 10
	server.stats.ActiveStreams = 5

	stats := server.GetStats()

	if stats.TotalEvents != 200 {
		t.Errorf("TotalEvents = %d, want 200", stats.TotalEvents)
	}
	if stats.StreamEvents != 100 {
		t.Errorf("StreamEvents = %d, want 100", stats.StreamEvents)
	}
	if stats.ForwardedEvents != 150 {
		t.Errorf("ForwardedEvents = %d, want 150", stats.ForwardedEvents)
	}
	if stats.ErrorEvents != 10 {
		t.Errorf("ErrorEvents = %d, want 10", stats.ErrorEvents)
	}
	if stats.ActiveStreams != 5 {
		t.Errorf("ActiveStreams = %d, want 5", stats.ActiveStreams)
	}
}

func TestConcurrentStatsUpdate(t *testing.T) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	// Test concurrent access to stats
	done := make(chan bool)
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		go func() {
			server.stats.mu.Lock()
			server.stats.TotalEvents++
			server.stats.mu.Unlock()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	stats := server.GetStats()
	if stats.TotalEvents != int64(numGoroutines) {
		t.Errorf("TotalEvents = %d, want %d", stats.TotalEvents, numGoroutines)
	}
}

func TestGenerateStreamID(t *testing.T) {
	id1 := generateStreamID()
	id2 := generateStreamID()

	if id1 == id2 {
		t.Error("generateStreamID() should generate unique IDs")
	}

	if id1 == "" {
		t.Error("generateStreamID() should not return empty string")
	}
}

func BenchmarkIngestEvent(b *testing.B) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	req := &mcpv1.IngestEventRequest{
		EventId:   "bench-test",
		EventType: "benchmark.event",
		Source:    "benchmark",
		Data:      `{"benchmark": true}`,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := server.IngestEvent(ctx, req)
		if err != nil {
			b.Fatalf("IngestEvent() error = %v", err)
		}
	}
}

func BenchmarkValidateIngestRequest(b *testing.B) {
	logger := zap.NewNop()
	server := NewMCPServer(logger, nil)

	req := &mcpv1.IngestEventRequest{
		EventId:   "bench-test",
		EventType: "benchmark.event",
		Source:    "benchmark",
		Data:      `{"benchmark": true}`,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := server.validateIngestRequest(req)
		if err != nil {
			b.Fatalf("validateIngestRequest() error = %v", err)
		}
	}
}
