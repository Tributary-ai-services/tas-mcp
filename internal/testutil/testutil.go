package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
	"go.uber.org/zap"
)

// CreateTestEvent creates a test event for use in tests
func CreateTestEvent(id, eventType, source string) *mcpv1.Event {
	return &mcpv1.Event{
		EventId:   id,
		EventType: eventType,
		Source:    source,
		Timestamp: time.Now().Unix(),
		Data:      fmt.Sprintf(`{"test": true, "id": "%s"}`, id),
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
		Data:      fmt.Sprintf(`{"test": true, "id": "%s"}`, id),
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
		Enabled:             true,
		DefaultRetryAttempts: 3,
		DefaultTimeout:      5 * time.Second,
		BufferSize:          100,
		Workers:             2,
		Targets: []*config.TargetConfiguration{
			{
				ID:       "test-target",
				Name:     "Test Target",
				Type:     "http",
				Endpoint: "http://localhost:8888",
				Config: &config.TargetConfig{
					Timeout:       5 * time.Second,
					RetryAttempts: 2,
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
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
	
	ticker := time.NewTicker(10 * time.Millisecond)
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
		HTTPPort:         8080,
		GRPCPort:         50051,
		HealthCheckPort:  8082,
		LogLevel:         "debug",
		ForwardTo:        []string{},
		ForwardTimeout:   30 * time.Second,
		MaxEventSize:     1024 * 1024, // 1MB
		BufferSize:       1000,
		MaxConnections:   100,
		Version:          "test",
		Forwarding:       CreateTestForwardingConfig(),
	}
}

// MockEventCapture captures events sent to a mock endpoint
type MockEventCapture struct {
	Events []mcpv1.Event
	mutex  sync.RWMutex
}

func NewMockEventCapture() *MockEventCapture {
	return &MockEventCapture{
		Events: make([]mcpv1.Event, 0),
	}
}

func (m *MockEventCapture) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var event mcpv1.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		
		m.mutex.Lock()
		m.Events = append(m.Events, event)
		m.mutex.Unlock()
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "received"})
	}
}

func (m *MockEventCapture) GetEvents() []mcpv1.Event {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	// Return a copy to avoid race conditions
	events := make([]mcpv1.Event, len(m.Events))
	copy(events, m.Events)
	return events
}

func (m *MockEventCapture) GetEventCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.Events)
}

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

// Helper functions for creating matchers
func MatchEventID(id string) *TestEventMatcher {
	return &TestEventMatcher{EventID: &id}
}

func MatchEventType(eventType string) *TestEventMatcher {
	return &TestEventMatcher{EventType: &eventType}
}

func MatchSource(source string) *TestEventMatcher {
	return &TestEventMatcher{Source: &source}
}

func MatchDataField(field string, value interface{}) *TestEventMatcher {
	return &TestEventMatcher{DataField: map[string]interface{}{field: value}}
}