// Package testutil provides testing utilities and helpers for the TAS MCP server.
package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
)

// Test configuration constants
const (
	// TestRetryAttempts is the default retry attempts for tests
	TestRetryAttempts = 3
	// TestTimeout is the default timeout for test operations
	TestTimeout = 5 * time.Second
	// TestBufferSize is the default buffer size for tests
	TestBufferSize = 100
	// TestWorkers is the default number of workers for tests
	TestWorkers = 2
	// TestTargetRetryAttempts is the retry attempts for test targets
	TestTargetRetryAttempts = 2
	// TestHTTPPort is the default HTTP port for tests
	TestHTTPPort = 8080
	// TestGRPCPort is the default gRPC port for tests
	TestGRPCPort = 50051
	// TestHealthCheckPort is the default health check port for tests
	TestHealthCheckPort = 8082
	// TestMaxEventSize is the default max event size for tests (1MB)
	TestMaxEventSize = 1024 * 1024
	// TestMaxConnections is the default max connections for tests
	TestMaxConnections = 100
	// WaitConditionTickInterval is the tick interval for waiting on conditions
	WaitConditionTickInterval = 10 * time.Millisecond
)

// CreateTestEvent creates a test event for use in tests
func CreateTestEvent(id, eventType, source string) *mcpv1.Event {
	return &mcpv1.Event{
		EventId:   id,
		EventType: eventType,
		Source:    source,
		Timestamp: time.Now().Unix(),
		Data:      fmt.Sprintf(`{"test": true, "id": %q}`, id),
		Metadata: map[string]string{
			"test":        "true",
			"created_by":  "testutil",
			"environment": "test",
		},
	}
}

// CreateTestIngestRequest creates a test IngestEventRequest
func CreateTestIngestRequest(id, eventType, source string) *mcpv1.IngestEventRequest {
	return &mcpv1.IngestEventRequest{
		EventId:   id,
		EventType: eventType,
		Source:    source,
		Timestamp: time.Now().Unix(),
		Data:      fmt.Sprintf(`{"test": true, "id": %q}`, id),
		Metadata: map[string]string{
			"test":        "true",
			"created_by":  "testutil",
			"environment": "test",
		},
	}
}

// CreateTestForwardingConfig creates a test forwarding configuration
func CreateTestForwardingConfig() *config.ForwardingConfig {
	return &config.ForwardingConfig{
		Enabled:              true,
		DefaultRetryAttempts: TestRetryAttempts,
		DefaultTimeout:       TestTimeout,
		BufferSize:           TestBufferSize,
		Workers:              TestWorkers,
		Targets: []*config.TargetConfiguration{
			{
				ID:       "test-target",
				Name:     "Test Target",
				Type:     "http",
				Endpoint: "http://localhost:8888",
				Config: &config.TargetConfig{
					Timeout:       TestTimeout,
					RetryAttempts: TestTargetRetryAttempts,
					Headers: map[string]string{
						"Content-Type": "application/json",
						"X-Test":       "true",
					},
				},
				Rules: []*config.ForwardingRule{
					{
						ID:      "test-rule",
						Name:    "Test Rule",
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
}

// CreateMockHTTPServer creates a mock HTTP server for testing
func CreateMockHTTPServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log request for debugging
		t.Logf("Mock server received: %s %s", r.Method, r.URL.Path)

		if handler != nil {
			handler(w, r)
		} else {
			// Default successful response
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
}

// AssertEventEqual compares two events for equality in tests
func AssertEventEqual(t *testing.T, expected, actual *mcpv1.Event) {
	t.Helper()

	if expected.EventId != actual.EventId {
		t.Errorf("EventId: expected %s, got %s", expected.EventId, actual.EventId)
	}
	if expected.EventType != actual.EventType {
		t.Errorf("EventType: expected %s, got %s", expected.EventType, actual.EventType)
	}
	if expected.Source != actual.Source {
		t.Errorf("Source: expected %s, got %s", expected.Source, actual.Source)
	}
	if expected.Data != actual.Data {
		t.Errorf("Data: expected %s, got %s", expected.Data, actual.Data)
	}

	// Compare metadata
	if len(expected.Metadata) != len(actual.Metadata) {
		t.Errorf("Metadata length: expected %d, got %d", len(expected.Metadata), len(actual.Metadata))
	}

	for key, expectedValue := range expected.Metadata {
		if actualValue, exists := actual.Metadata[key]; !exists {
			t.Errorf("Metadata key %s not found", key)
		} else if expectedValue != actualValue {
			t.Errorf("Metadata[%s]: expected %s, got %s", key, expectedValue, actualValue)
		}
	}
}

// GetTestLogger returns a test logger (no-op logger for performance)
func GetTestLogger() *zap.Logger {
	return zap.NewNop()
}

// GetTestLoggerWithOutput returns a test logger that outputs to testing.T
func GetTestLoggerWithOutput(t *testing.T) *zap.Logger {
	t.Helper()

	config := zap.NewDevelopmentConfig()
	config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)

	logger, err := config.Build()
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}

	return logger
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	t.Helper()

	ticker := time.NewTicker(WaitConditionTickInterval)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			if condition() {
				return
			}
		case <-timeoutCh:
			t.Fatalf("Timeout waiting for condition: %s", message)
		}
	}
}

// GenerateTestEvents generates multiple test events
func GenerateTestEvents(count int, eventType string) []*mcpv1.Event {
	events := make([]*mcpv1.Event, count)

	for i := 0; i < count; i++ {
		events[i] = CreateTestEvent(
			fmt.Sprintf("test-event-%d", i),
			eventType,
			"test-source",
		)
	}

	return events
}

// CreateTestConfig creates a basic test configuration
func CreateTestConfig() *config.Config {
	return &config.Config{
		HTTPPort:        TestHTTPPort,
		GRPCPort:        TestGRPCPort,
		HealthCheckPort: TestHealthCheckPort,
		LogLevel:        "debug",
		ForwardTo:       []string{},
		ForwardTimeout:  config.DefaultForwardTimeout,
		MaxEventSize:    TestMaxEventSize,
		BufferSize:      config.DefaultBufferSize,
		MaxConnections:  TestMaxConnections,
		Version:         "test",
		Forwarding:      CreateTestForwardingConfig(),
	}
}

// CapturedEvent represents a simplified event without mutex fields
type CapturedEvent struct {
	EventID   string            `json:"event_id"`
	EventType string            `json:"event_type"`
	Source    string            `json:"source"`
	Timestamp int64             `json:"timestamp"`
	Data      string            `json:"data"`
	Metadata  map[string]string `json:"metadata"`
}

// MockEventCapture captures events sent to a mock endpoint
type MockEventCapture struct {
	Events []CapturedEvent
	mutex  sync.RWMutex
}

// NewMockEventCapture creates a new MockEventCapture for testing.
func NewMockEventCapture() *MockEventCapture {
	return &MockEventCapture{
		Events: make([]CapturedEvent, 0),
	}
}

// Handler returns an HTTP handler function for capturing events.
func (m *MockEventCapture) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var event mcpv1.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Convert to simplified struct to avoid copying mutex fields
		capturedEvent := CapturedEvent{
			EventID:   event.EventId,
			EventType: event.EventType,
			Source:    event.Source,
			Timestamp: event.Timestamp,
			Data:      event.Data,
			Metadata:  event.Metadata,
		}

		m.mutex.Lock()
		m.Events = append(m.Events, capturedEvent)
		m.mutex.Unlock()

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "received"})
	}
}

// GetEvents returns all captured events in a thread-safe manner.
func (m *MockEventCapture) GetEvents() []CapturedEvent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Return a copy to avoid race conditions
	events := make([]CapturedEvent, len(m.Events))
	copy(events, m.Events)
	return events
}

// GetEventCount returns the number of captured events.
func (m *MockEventCapture) GetEventCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.Events)
}

// Clear removes all captured events.
func (m *MockEventCapture) Clear() {
	m.mutex.Lock()
	m.Events = m.Events[:0]
	m.mutex.Unlock()
}

// TestEventMatcher provides flexible event matching for tests
type TestEventMatcher struct {
	EventID   *string
	EventType *string
	Source    *string
	DataField map[string]interface{}
}

// Matches checks if an event matches the configured criteria.
func (m *TestEventMatcher) Matches(event *mcpv1.Event) bool {
	if m.EventID != nil && *m.EventID != event.EventId {
		return false
	}

	if m.EventType != nil && *m.EventType != event.EventType {
		return false
	}

	if m.Source != nil && *m.Source != event.Source {
		return false
	}

	if len(m.DataField) > 0 {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return false
		}

		for key, expectedValue := range m.DataField {
			if actualValue, exists := data[key]; !exists || actualValue != expectedValue {
				return false
			}
		}
	}

	return true
}

// MatchEventID creates a matcher that matches events by ID.
func MatchEventID(id string) *TestEventMatcher {
	return &TestEventMatcher{EventID: &id}
}

// MatchEventType creates a matcher that matches events by type.
func MatchEventType(eventType string) *TestEventMatcher {
	return &TestEventMatcher{EventType: &eventType}
}

// MatchSource creates a matcher that matches events by source.
func MatchSource(source string) *TestEventMatcher {
	return &TestEventMatcher{Source: &source}
}

// MatchDataField creates a matcher that matches events by a data field value.
func MatchDataField(field string, value interface{}) *TestEventMatcher {
	return &TestEventMatcher{DataField: map[string]interface{}{field: value}}
}
