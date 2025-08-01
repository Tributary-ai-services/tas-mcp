package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
	"github.com/tributary-ai-services/tas-mcp/internal/forwarding"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
	httpserver "github.com/tributary-ai-services/tas-mcp/internal/http"
	"github.com/tributary-ai-services/tas-mcp/internal/logger"
	"go.uber.org/zap"
)

// BenchmarkSetup holds common benchmark setup
type BenchmarkSetup struct {
	grpcServer *grpcserver.MCPServer
	httpServer *httpserver.Server
	forwarder  *forwarding.EventForwarder
	logger     *zap.Logger
}

func NewBenchmarkSetup(b *testing.B) *BenchmarkSetup {
	b.Helper()

	logger := zap.NewNop() // Use no-op logger for benchmarks

	forwardingConfig := &config.ForwardingConfig{
		Enabled:             false, // Disable forwarding for pure ingestion benchmarks
		DefaultRetryAttempts: 1,
		DefaultTimeout:      1 * time.Second,
		BufferSize:          10000,
		Workers:             4,
		Targets:             []*config.TargetConfiguration{},
	}

	forwarder := forwarding.NewEventForwarder(logger, forwardingConfig)
	grpcServer := grpcserver.NewMCPServer(logger, forwarder)
	httpServer := httpserver.NewServer(logger, grpcServer, forwarder)

	return &BenchmarkSetup{
		grpcServer: grpcServer,
		httpServer: httpServer,
		forwarder:  forwarder,
		logger:     logger,
	}
}

func BenchmarkGRPCEventIngestion(b *testing.B) {
	setup := NewBenchmarkSetup(b)
	ctx := context.Background()

	req := &mcpv1.IngestEventRequest{
		EventId:   "bench-event",
		EventType: "benchmark.event",
		Source:    "benchmark-source",
		Timestamp: time.Now().Unix(),
		Data:      `{"benchmark": true, "iteration": 0}`,
		Metadata: map[string]string{
			"benchmark": "true",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req.EventId = fmt.Sprintf("bench-event-%d", i)
		req.Data = fmt.Sprintf(`{"benchmark": true, "iteration": %d}`, i)

		_, err := setup.grpcServer.IngestEvent(ctx, req)
		if err != nil {
			b.Fatalf("IngestEvent failed: %v", err)
		}
	}
}

func BenchmarkHTTPEventIngestion(b *testing.B) {
	setup := NewBenchmarkSetup(b)
	ts := httptest.NewServer(setup.httpServer.Handler())
	defer ts.Close()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	eventTemplate := `{
		"event_id": "bench-event-%d",
		"event_type": "benchmark.event",
		"source": "benchmark-source",
		"data": "{\"benchmark\": true, \"iteration\": %d}",
		"metadata": {"benchmark": "true"}
	}`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		eventJSON := fmt.Sprintf(eventTemplate, i, i)
		resp, err := client.Post(
			ts.URL+"/api/v1/events",
			"application/json",
			strings.NewReader(eventJSON),
		)
		if err != nil {
			b.Fatalf("HTTP request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b.Fatalf("HTTP status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	}
}

func BenchmarkHTTPBatchEventIngestion(b *testing.B) {
	setup := NewBenchmarkSetup(b)
	ts := httptest.NewServer(setup.httpServer.Handler())
	defer ts.Close()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Test different batch sizes
	batchSizes := []int{1, 10, 50, 100, 500}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize%d", batchSize), func(b *testing.B) {
			// Pre-generate batch data
			events := make([]map[string]interface{}, batchSize)
			for i := 0; i < batchSize; i++ {
				events[i] = map[string]interface{}{
					"event_id":   fmt.Sprintf("batch-event-%d", i),
					"event_type": "benchmark.batch.event",
					"source":     "benchmark-batch",
					"data":       fmt.Sprintf(`{"batch": true, "index": %d}`, i),
				}
			}

			batchJSON, _ := json.Marshal(events)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				resp, err := client.Post(
					ts.URL+"/api/v1/events/batch",
					"application/json",
					strings.NewReader(string(batchJSON)),
				)
				if err != nil {
					b.Fatalf("HTTP batch request failed: %v", err)
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					b.Fatalf("HTTP batch status = %d, want %d", resp.StatusCode, http.StatusOK)
				}
			}
		})
	}
}

func BenchmarkEventForwarding(b *testing.B) {
	// Create a mock target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer targetServer.Close()

	logger := zap.NewNop()
	forwardingConfig := &config.ForwardingConfig{
		Enabled:             true,
		DefaultRetryAttempts: 1,
		DefaultTimeout:      5 * time.Second,
		BufferSize:          10000,
		Workers:             4,
		Targets: []*config.TargetConfiguration{
			{
				ID:       "bench-target",
				Name:     "Benchmark Target",
				Type:     "http",
				Endpoint: targetServer.URL,
				Config: &config.TargetConfig{
					Timeout:       5 * time.Second,
					RetryAttempts: 1,
				},
				Rules: []*config.ForwardingRule{
					{
						ID:      "forward-all",
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

	forwarder := forwarding.NewEventForwarder(logger, forwardingConfig)
	forwarder.Start()
	defer forwarder.Stop()

	event := &mcpv1.Event{
		EventId:   "forward-bench-event",
		EventType: "benchmark.forward.event",
		Source:    "benchmark-forward",
		Data:      `{"forward": true}`,
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		event.EventId = fmt.Sprintf("forward-bench-event-%d", i)
		err := forwarder.ForwardEvent(ctx, event)
		if err != nil {
			b.Fatalf("ForwardEvent failed: %v", err)
		}
	}
}

func BenchmarkRuleEvaluation(b *testing.B) {
	logger := zap.NewNop()
	forwarder := &forwarding.EventForwarder{}

	event := &mcpv1.Event{
		EventId:   "rule-bench-event",
		EventType: "user.created",
		Source:    "auth-service",
		Data:      `{"user_id": "123", "email": "test@example.com", "age": 25}`,
		Metadata: map[string]string{
			"environment": "production",
			"version":     "1.0",
		},
	}

	conditions := []*config.RuleCondition{
		{Field: "event_type", Operator: "eq", Value: "user.created"},
		{Field: "source", Operator: "contains", Value: "auth"},
		{Field: "data.age", Operator: "gte", Value: 18},
		{Field: "metadata.environment", Operator: "eq", Value: "production"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, condition := range conditions {
			result := forwarder.EvaluateCondition(event, condition)
			if !result {
				b.Error("Expected condition to match")
			}
		}
	}
}

func BenchmarkJSONParsing(b *testing.B) {
	eventJSON := `{
		"event_id": "json-bench-event",
		"event_type": "benchmark.json.event",
		"source": "json-benchmark",
		"timestamp": 1640995200,
		"data": "{\"user_id\": \"123\", \"action\": \"login\", \"metadata\": {\"ip\": \"192.168.1.1\", \"user_agent\": \"Mozilla/5.0\"}}",
		"metadata": {
			"environment": "production",
			"version": "1.0",
			"correlation_id": "abc-123-def"
		}
	}`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var event httpserver.EventRequest
		err := json.Unmarshal([]byte(eventJSON), &event)
		if err != nil {
			b.Fatalf("JSON unmarshal failed: %v", err)
		}

		// Also parse the inner data JSON
		var data map[string]interface{}
		err = json.Unmarshal([]byte(event.Data), &data)
		if err != nil {
			b.Fatalf("Data JSON unmarshal failed: %v", err)
		}
	}
}

func BenchmarkMemoryAllocation(b *testing.B) {
	setup := NewBenchmarkSetup(b)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := &mcpv1.IngestEventRequest{
			EventId:   fmt.Sprintf("mem-bench-event-%d", i),
			EventType: "benchmark.memory.event",
			Source:    "memory-benchmark",
			Timestamp: time.Now().Unix(),
			Data:      fmt.Sprintf(`{"iteration": %d, "data": "some test data"}`, i),
			Metadata: map[string]string{
				"benchmark": "memory",
				"iteration": fmt.Sprintf("%d", i),
			},
		}

		_, err := setup.grpcServer.IngestEvent(ctx, req)
		if err != nil {
			b.Fatalf("IngestEvent failed: %v", err)
		}
	}
}

func BenchmarkConcurrentIngestion(b *testing.B) {
	setup := NewBenchmarkSetup(b)
	ctx := context.Background()

	numWorkers := []int{1, 2, 4, 8, 16}

	for _, workers := range numWorkers {
		b.Run(fmt.Sprintf("Workers%d", workers), func(b *testing.B) {
			work := make(chan int, b.N)
			done := make(chan bool, workers)

			// Fill work channel
			for i := 0; i < b.N; i++ {
				work <- i
			}
			close(work)

			b.ResetTimer()
			b.ReportAllocs()

			// Start workers
			for w := 0; w < workers; w++ {
				go func() {
					defer func() { done <- true }()

					for i := range work {
						req := &mcpv1.IngestEventRequest{
							EventId:   fmt.Sprintf("concurrent-bench-event-%d", i),
							EventType: "benchmark.concurrent.event",
							Source:    "concurrent-benchmark",
							Data:      fmt.Sprintf(`{"worker": %d, "iteration": %d}`, w, i),
						}

						_, err := setup.grpcServer.IngestEvent(ctx, req)
						if err != nil {
							b.Errorf("IngestEvent failed: %v", err)
							return
						}
					}
				}()
			}

			// Wait for all workers to complete
			for w := 0; w < workers; w++ {
				<-done
			}
		})
	}
}

func BenchmarkHealthCheck(b *testing.B) {
	setup := NewBenchmarkSetup(b)
	ts := httptest.NewServer(setup.httpServer.Handler())
	defer ts.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Get(ts.URL + "/health")
		if err != nil {
			b.Fatalf("Health check failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b.Fatalf("Health check status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	}
}

// Helper function to run all benchmarks with different configurations
func BenchmarkAll(b *testing.B) {
	benchmarks := []struct {
		name string
		fn   func(*testing.B)
	}{
		{"GRPCEventIngestion", BenchmarkGRPCEventIngestion},
		{"HTTPEventIngestion", BenchmarkHTTPEventIngestion},
		{"JSONParsing", BenchmarkJSONParsing},
		{"MemoryAllocation", BenchmarkMemoryAllocation},
		{"HealthCheck", BenchmarkHealthCheck},
	}

	for _, bench := range benchmarks {
		b.Run(bench.name, bench.fn)
	}
}