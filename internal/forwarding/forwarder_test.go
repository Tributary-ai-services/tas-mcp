package forwarding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
	"go.uber.org/zap"
)

func TestNewEventForwarder(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Enabled:             true,
		DefaultRetryAttempts: 3,
		DefaultTimeout:      30 * time.Second,
		BufferSize:          1000,
		Workers:             5,
		Targets:             []*config.TargetConfiguration{},
	}

	forwarder := NewEventForwarder(logger, config)

	if forwarder == nil {
		t.Error("NewEventForwarder() returned nil")
	}
	if forwarder.logger != logger {
		t.Error("Logger not set correctly")
	}
	if forwarder.config != config {
		t.Error("Config not set correctly")
	}
	if forwarder.targets == nil {
		t.Error("Targets map not initialized")
	}
	if forwarder.metrics == nil {
		t.Error("Metrics not initialized")
	}
}

func TestForwardingTarget_HTTPTarget(t *testing.T) {
	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		
		var event mcpv1.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}
		
		if event.EventId != "test-123" {
			t.Errorf("Expected event ID test-123, got %s", event.EventId)
		}
		
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "accepted"}`))
	}))
	defer server.Close()

	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Enabled:             true,
		DefaultRetryAttempts: 1,
		DefaultTimeout:      5 * time.Second,
		BufferSize:          100,
		Workers:             1,
		Targets: []*config.TargetConfiguration{
			{
				ID:       "http-target",
				Name:     "HTTP Test Target",
				Type:     "http",
				Endpoint: server.URL,
				Config: &config.TargetConfig{
					Timeout:       5 * time.Second,
					RetryAttempts: 1,
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
				},
				Rules: []*config.ForwardingRule{
					{
						ID:      "all-events",
						Enabled: true,
						Conditions: []*config.RuleCondition{
							{
								Field:    "event_type",
								Operator: "ne",
								Value:    "",
							},
						},
					},
				},
			},
		},
	}

	forwarder := NewEventForwarder(logger, config)
	err := forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer forwarder.Stop()

	// Test event forwarding
	event := &mcpv1.Event{
		EventId:   "test-123",
		EventType: "test.event",
		Source:    "test-source",
		Data:      `{"test": "data"}`,
	}

	err = forwarder.ForwardEvent(context.Background(), event)
	if err != nil {
		t.Errorf("ForwardEvent() error = %v", err)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check metrics
	metrics := forwarder.GetMetrics()
	if metrics.TotalEvents != 1 {
		t.Errorf("TotalEvents = %d, want 1", metrics.TotalEvents)
	}
	if metrics.ForwardedEvents != 1 {
		t.Errorf("ForwardedEvents = %d, want 1", metrics.ForwardedEvents)
	}
}

func TestRuleEvaluation(t *testing.T) {
	logger := zap.NewNop()
	forwarder := &EventForwarder{logger: logger}

	tests := []struct {
		name      string
		event     *mcpv1.Event
		condition *config.RuleCondition
		expected  bool
	}{
		{
			name: "string equality - match",
			event: &mcpv1.Event{
				EventType: "user.created",
				Source:    "auth-service",
			},
			condition: &config.RuleCondition{
				Field:    "event_type",
				Operator: "eq",
				Value:    "user.created",
			},
			expected: true,
		},
		{
			name: "string equality - no match",
			event: &mcpv1.Event{
				EventType: "user.updated",
				Source:    "auth-service",
			},
			condition: &config.RuleCondition{
				Field:    "event_type",
				Operator: "eq",
				Value:    "user.created",
			},
			expected: false,
		},
		{
			name: "string contains - match",
			event: &mcpv1.Event{
				EventType: "user.created",
				Source:    "auth-service",
			},
			condition: &config.RuleCondition{
				Field:    "event_type",
				Operator: "contains",
				Value:    "user",
			},
			expected: true,
		},
		{
			name: "string contains - no match",
			event: &mcpv1.Event{
				EventType: "order.created",
				Source:    "order-service",
			},
			condition: &config.RuleCondition{
				Field:    "event_type",
				Operator: "contains",
				Value:    "user",
			},
			expected: false,
		},
		{
			name: "not equal - match",
			event: &mcpv1.Event{
				EventType: "user.created",
				Source:    "auth-service",
			},
			condition: &config.RuleCondition{
				Field:    "event_type",
				Operator: "ne",
				Value:    "user.deleted",
			},
			expected: true,
		},
		{
			name: "data field extraction - match",
			event: &mcpv1.Event{
				EventType: "user.created",
				Data:      `{"user_id": "123", "email": "test@example.com"}`,
			},
			condition: &config.RuleCondition{
				Field:    "data.user_id",
				Operator: "eq",
				Value:    "123",
			},
			expected: true,
		},
		{
			name: "data field extraction - no match",
			event: &mcpv1.Event{
				EventType: "user.created",
				Data:      `{"user_id": "456", "email": "test@example.com"}`,
			},
			condition: &config.RuleCondition{
				Field:    "data.user_id",
				Operator: "eq",
				Value:    "123",
			},
			expected: false,
		},
		{
			name: "metadata field - match",
			event: &mcpv1.Event{
				EventType: "user.created",
				Metadata: map[string]string{
					"environment": "production",
					"version":     "1.0",
				},
			},
			condition: &config.RuleCondition{
				Field:    "metadata.environment",
				Operator: "eq",
				Value:    "production",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := forwarder.EvaluateCondition(tt.event, tt.condition)
			if result != tt.expected {
				t.Errorf("evaluateCondition() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetTargets(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Targets: []*config.TargetConfiguration{
			{
				ID:       "target-1",
				Name:     "Target 1",
				Type:     "http",
				Endpoint: "http://example.com",
			},
			{
				ID:       "target-2",
				Name:     "Target 2",
				Type:     "grpc",
				Endpoint: "grpc://example.com:50051",
			},
		},
	}

	forwarder := NewEventForwarder(logger, config)

	// Start the forwarder to load targets from config
	err := forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer forwarder.Stop()

	targets := forwarder.GetTargets()
	if len(targets) != 2 {
		t.Errorf("Number of targets = %d, want 2", len(targets))
	}

	if target, exists := targets["target-1"]; !exists {
		t.Error("target-1 should exist")
	} else if target.Name != "Target 1" {
		t.Errorf("Target name = %s, want Target 1", target.Name)
	}

	if target, exists := targets["target-2"]; !exists {
		t.Error("target-2 should exist")
	} else if target.Type != ForwardingTypeGRPC {
		t.Errorf("Target type = %v, want %v", target.Type, ForwardingTypeGRPC)
	}
}

func TestAddTarget(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Targets: []*config.TargetConfiguration{},
	}

	forwarder := NewEventForwarder(logger, config)

	target := &ForwardingTarget{
		ID:       "new-target",
		Name:     "New Target",
		Type:     ForwardingTypeHTTP,
		Endpoint: "http://new.example.com",
		Status:   TargetStatusHealthy,
	}

	err := forwarder.AddTarget(target)
	if err != nil {
		t.Errorf("AddTarget() error = %v", err)
	}

	targets := forwarder.GetTargets()
	if len(targets) != 1 {
		t.Errorf("Number of targets = %d, want 1", len(targets))
	}

	if addedTarget, exists := targets["new-target"]; !exists {
		t.Error("new-target should exist after adding")
	} else if addedTarget.Name != "New Target" {
		t.Errorf("Added target name = %s, want New Target", addedTarget.Name)
	}
}

func TestAddTarget_Duplicate(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Targets: []*config.TargetConfiguration{
			{
				ID:       "existing-target",
				Name:     "Existing Target", 
				Type:     "http",
				Endpoint: "http://example.com",
			},
		},
	}

	forwarder := NewEventForwarder(logger, config)

	// Start the forwarder to load targets from config
	err := forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer forwarder.Stop()

	target := &ForwardingTarget{
		ID:       "existing-target",
		Name:     "Duplicate Target",
		Type:     ForwardingTypeHTTP,
		Endpoint: "http://duplicate.example.com",
	}

	err = forwarder.AddTarget(target)
	if err == nil {
		t.Error("AddTarget() should return error for duplicate ID")
	}
}

func TestRemoveTarget(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Targets: []*config.TargetConfiguration{
			{
				ID:       "target-to-remove",
				Name:     "Target To Remove",
				Type:     "http",
				Endpoint: "http://example.com",
			},
		},
	}

	forwarder := NewEventForwarder(logger, config)

	// Start the forwarder to load targets from config
	err := forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer forwarder.Stop()

	// Verify target exists
	targets := forwarder.GetTargets()
	if len(targets) != 1 {
		t.Fatalf("Expected 1 target initially, got %d", len(targets))
	}

	err = forwarder.RemoveTarget("target-to-remove")
	if err != nil {
		t.Errorf("RemoveTarget() error = %v", err)
	}

	// Verify target was removed
	targets = forwarder.GetTargets()
	if len(targets) != 0 {
		t.Errorf("Number of targets after removal = %d, want 0", len(targets))
	}
}

func TestRemoveTarget_NotFound(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Targets: []*config.TargetConfiguration{},
	}

	forwarder := NewEventForwarder(logger, config)

	err := forwarder.RemoveTarget("non-existent")
	if err == nil {
		t.Error("RemoveTarget() should return error for non-existent target")
	}
}

func TestGetMetrics(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{}
	forwarder := NewEventForwarder(logger, config)

	// Set some test metrics
	forwarder.metrics.TotalEvents = 100
	forwarder.metrics.ForwardedEvents = 90
	forwarder.metrics.FailedEvents = 5
	forwarder.metrics.DroppedEvents = 5

	metrics := forwarder.GetMetrics()
	if metrics.TotalEvents != 100 {
		t.Errorf("TotalEvents = %d, want 100", metrics.TotalEvents)
	}
	if metrics.ForwardedEvents != 90 {
		t.Errorf("ForwardedEvents = %d, want 90", metrics.ForwardedEvents)
	}
	if metrics.FailedEvents != 5 {
		t.Errorf("FailedEvents = %d, want 5", metrics.FailedEvents)
	}
	if metrics.DroppedEvents != 5 {
		t.Errorf("DroppedEvents = %d, want 5", metrics.DroppedEvents)
	}
}

func TestForwardingTarget_StatusTransitions(t *testing.T) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Targets: []*config.TargetConfiguration{
			{
				ID:       "test-target",
				Name:     "Test Target",
				Type:     "http",
				Endpoint: "http://unreachable.example.com",
				Config: &config.TargetConfig{
					Timeout:       1 * time.Second,
					RetryAttempts: 1,
				},
			},
		},
	}

	forwarder := NewEventForwarder(logger, config)

	// Start the forwarder to load targets from config
	err := forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer forwarder.Stop()

	// Get the target
	targets := forwarder.GetTargets()
	target, exists := targets["test-target"]
	if !exists {
		t.Fatal("test-target should exist")
	}

	// Initially should be healthy
	if target.Status != TargetStatusHealthy {
		t.Errorf("Initial status = %v, want %v", target.Status, TargetStatusHealthy)
	}

	// Simulate failure (this would normally happen during forwarding)
	forwarder.mu.Lock()
	target.Status = TargetStatusUnhealthy
	forwarder.mu.Unlock()

	// Verify status changed
	targets = forwarder.GetTargets()
	target = targets["test-target"]
	if target.Status != TargetStatusUnhealthy {
		t.Errorf("Status after failure = %v, want %v", target.Status, TargetStatusUnhealthy)
	}
}

func BenchmarkForwardEvent(b *testing.B) {
	logger := zap.NewNop()
	config := &config.ForwardingConfig{
		Enabled:             true,
		DefaultRetryAttempts: 1,
		DefaultTimeout:      1 * time.Second,
		BufferSize:          1000,
		Workers:             5,
		Targets:             []*config.TargetConfiguration{},
	}

	forwarder := NewEventForwarder(logger, config)
	forwarder.Start()
	defer forwarder.Stop()

	event := &mcpv1.Event{
		EventId:   "bench-test",
		EventType: "benchmark.event",
		Source:    "benchmark",
		Data:      `{"benchmark": true}`,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := forwarder.ForwardEvent(ctx, event)
		if err != nil {
			b.Fatalf("ForwardEvent() error = %v", err)
		}
	}
}

func BenchmarkRuleEvaluation(b *testing.B) {
	logger := zap.NewNop()
	forwarder := &EventForwarder{logger: logger}

	event := &mcpv1.Event{
		EventId:   "bench-test",
		EventType: "user.created",
		Source:    "auth-service",
		Data:      `{"user_id": "123", "email": "test@example.com"}`,
		Metadata: map[string]string{
			"environment": "production",
		},
	}

	condition := &config.RuleCondition{
		Field:    "event_type",
		Operator: "contains",
		Value:    "user",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := forwarder.EvaluateCondition(event, condition)
		if !result {
			b.Error("Expected condition to match")
		}
	}
}