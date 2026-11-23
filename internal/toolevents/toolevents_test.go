package toolevents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testToolCallID = "toolu_01ABCdef"

var occurred = time.Date(2026, 11, 21, 16, 4, 5, 123_000_000, time.UTC)

func attributedRecord() federation.ToolCallRecord {
	return federation.ToolCallRecord{
		ServerID:     "napkin-mcp",
		ToolName:     "create_napkin",
		ToolCallID:   testToolCallID,
		Outcome:      federation.OutcomeOK,
		Duration:     842 * time.Millisecond,
		ResultBytes:  4097,
		ReducedBytes: 1200,
		Findings:     map[string]int{"pii-email": 2, "pii-ssn": 1},
		ScanSkipped:  false,
		OccurredAt:   occurred,
		Identity: &identity.Identity{
			Attributed: true, Result: identity.ResultAttributed,
			TenantID: "a689c0b2-0000-4000-8000-000000000001", TokenID: "6f1d2c3b-0000-4000-8000-000000000002",
			OwnerSub: "kc-sub-1234", UserID: "u_42", AgentID: "research-agent",
			SourceApp: "claude-code", FlowID: "flow-9", StepID: "step-3",
		},
	}
}

func unattributedRecord() federation.ToolCallRecord {
	r := attributedRecord()
	r.Identity = &identity.Identity{Result: identity.ResultNoToken}
	r.Outcome = federation.OutcomeTimeout
	r.ResultBytes, r.ReducedBytes = 0, 0
	r.Findings = nil
	r.ScanSkipped = true
	return r
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s mismatch\n got:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestData_GoldenAttributed(t *testing.T) {
	golden(t, "tool_call_attributed.golden.json", BuildData(attributedRecord()))
}

func TestData_GoldenUnattributed(t *testing.T) {
	golden(t, "tool_call_unattributed.golden.json", BuildData(unattributedRecord()))
}

// The exact field set is a contract with the Timescale table and Spark job.
func TestData_FieldSet(t *testing.T) {
	b, _ := json.Marshal(BuildData(unattributedRecord()))
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	want := []string{"tenant_id", "token_id", "owner_sub", "user_id", "agent_id", "source_app", "flow_id", "step_id",
		"tool_call_id", "tool_name", "server_id", "observed_by", "outcome", "duration_ms", "result_bytes",
		"result_tokens_est", "reduced_bytes", "findings", "scan_skipped", "occurred_at",
		// AIQG-88: additive. The Spark job reads an explicit schema, so it
		// ignores the field until 009 gains a column for it.
		"delegated_by"}
	if len(m) != len(want) {
		t.Fatalf("field count %d, want %d: %v", len(m), len(want), m)
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	for _, k := range want[:8] {
		if m[k] != nil {
			t.Errorf("unattributed %s = %v, want null", k, m[k])
		}
	}
	if f, ok := m["findings"].(map[string]any); !ok || len(f) != 0 {
		t.Errorf("findings must be {} not null: %v", m["findings"])
	}
}

func TestEvent_EnvelopeAndKafkaMessage(t *testing.T) {
	e := BuildEvent(attributedRecord())
	if e.Type != EventType || e.Source != Source || e.Subject != testToolCallID ||
		e.TenantID != "a689c0b2-0000-4000-8000-000000000001" || !e.Time.Equal(occurred) {
		t.Fatalf("envelope %+v", e)
	}
	msg, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if string(msg.Key) != testToolCallID {
		t.Fatalf("key %q", msg.Key)
	}
	h := map[string]string{}
	for _, x := range msg.Headers {
		h[x.Key] = string(x.Value)
	}
	if h["ce_type"] != EventType || h["ce-type"] != EventType || h["content-type"] != "application/cloudevents+json" || h["ce-id"] != e.ID {
		t.Fatalf("headers %v", h)
	}
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(msg.Value, &env); err != nil || env.Type != EventType {
		t.Fatalf("value: %v %s", err, msg.Value)
	}
	var d Data
	if err := json.Unmarshal(env.Data, &d); err != nil || d.ToolCallID != testToolCallID || d.ResultTokensEst != 1025 {
		t.Fatalf("data: %v %+v", err, d)
	}
}

// fakeWriter can fail or block, and records what it wrote.
type fakeWriter struct {
	mu      sync.Mutex
	err     error
	block   chan struct{}
	written []kafka.Message
	closed  bool
}

func (w *fakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	if w.block != nil {
		<-w.block
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	w.written = append(w.written, msgs...)
	return nil
}

func (w *fakeWriter) Close() error { w.closed = true; return nil }

func TestPublisher_PublishesAndFlushesOnClose(t *testing.T) {
	w := &fakeWriter{}
	p := NewPublisher(w, 16, nil)
	for i := 0; i < 5; i++ {
		p.ObserveToolCall(attributedRecord())
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if len(w.written) != 5 || !w.closed {
		t.Fatalf("written=%d closed=%v", len(w.written), w.closed)
	}
	p.ObserveToolCall(attributedRecord()) // after close: dropped, no panic
	_ = p.Close()                         // idempotent
}

// A Kafka failure is absorbed by the worker: ObserveToolCall returns at once.
func TestPublisher_FailureDoesNotBlockOrPanic(t *testing.T) {
	w := &fakeWriter{err: errors.New("kafka: broker not available")}
	p := NewPublisher(w, 4, nil)
	start := time.Now()
	for i := 0; i < 50; i++ {
		p.ObserveToolCall(attributedRecord())
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("ObserveToolCall blocked for %v", d)
	}
	_ = p.Close()
}

// A wedged Kafka must not stall callers: the buffer fills, then events drop.
func TestPublisher_FullBufferDropsWithoutBlocking(t *testing.T) {
	w := &fakeWriter{block: make(chan struct{})}
	p := NewPublisher(w, 2, nil)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			p.ObserveToolCall(attributedRecord())
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ObserveToolCall blocked on a wedged writer")
	}
	close(w.block)
	_ = p.Close()
}

func TestNewKafkaPublisher_RequiresBrokers(t *testing.T) {
	if _, err := NewKafkaPublisher(nil, "", 0, nil); err == nil {
		t.Fatal("want error without brokers")
	}
}
