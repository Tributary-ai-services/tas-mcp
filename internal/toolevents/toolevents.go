// Package toolevents publishes one CloudEvent per federated tools/call
// (Plan #22 T.2, design §9.5 "Two sources, one event").
//
// Type com.tas.aiqg.tool_call.v1 on topic tas.aiqg.events.v1 — the topic the
// LLM gateway's AIQG events use, so the Spark job lands tool calls next to LLM
// events. The envelope is aether-shared go-events (tasevents.Event, encoded by
// kafkabind in structured mode).
//
// Publishing never touches the tool call: ObserveToolCall only does a
// non-blocking channel send. A full buffer drops the event and counts it; a
// Kafka failure is counted and logged by the background worker. Nothing here
// can fail, slow or alter the call.
package toolevents

import (
	"context"
	"errors"
	"sync"
	"time"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"github.com/Tributary-ai-services/aether-shared/go-events/kafkabind"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

const (
	// EventType is the CloudEvent type (design §9.5).
	EventType = "com.tas.aiqg.tool_call.v1"
	// DefaultTopic is the AIQG event topic shared with tas-llm-router.
	DefaultTopic = "tas.aiqg.events.v1"
	// Source is the CloudEvent source, in Gatekeeper's urn:tas:service scheme.
	Source = "urn:tas:service:tas-mcp"
	// ObservedByProxy is this source's observed_by value.
	ObservedByProxy = "proxy"

	// DefaultBufferSize bounds events waiting for Kafka. Past it, events drop.
	DefaultBufferSize = 1024
	maxBatch          = 100
	writeTimeout      = 5 * time.Second
	batchTimeout      = 50 * time.Millisecond
	closeTimeout      = 5 * time.Second
	// bytesPerToken is the result_tokens_est divisor: ceil(result_bytes / 4),
	// the usual rough English-text ratio. An estimate, not a tokenizer count.
	bytesPerToken = 4
	// occurredAtLayout is RFC 3339 in UTC with millisecond precision.
	occurredAtLayout = "2006-01-02T15:04:05.000Z07:00"
)

// Data is the event's data payload. Field names are a contract with the
// Timescale table (aether-shared 009_tool_call.sql) and the Spark job: change
// them only together. Identity fields are null when unknown — always present,
// never omitted, so every row has the same shape.
//
// findings is Gatekeeper pattern id → count, e.g. {"pii-email": 2,
// "cred-api-key": 1}: the categorized ids the gateway uses for
// event_metrics.tags, category = prefix before the first "-".
type Data struct {
	TenantID        *string        `json:"tenant_id"`
	TokenID         *string        `json:"token_id"`
	OwnerSub        *string        `json:"owner_sub"`
	UserID          *string        `json:"user_id"`
	AgentID         *string        `json:"agent_id"`
	SourceApp       *string        `json:"source_app"`
	FlowID          *string        `json:"flow_id"`
	StepID          *string        `json:"step_id"`
	ToolCallID      string         `json:"tool_call_id"`
	ToolName        string         `json:"tool_name"`
	ServerID        string         `json:"server_id"`
	ObservedBy      string         `json:"observed_by"`
	Outcome         string         `json:"outcome"`
	DurationMS      int64          `json:"duration_ms"`
	ResultBytes     int            `json:"result_bytes"`
	ResultTokensEst int            `json:"result_tokens_est"`
	ReducedBytes    int            `json:"reduced_bytes"`
	Findings        map[string]int `json:"findings"`
	ScanSkipped     bool           `json:"scan_skipped"`
	OccurredAt      string         `json:"occurred_at"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// BuildData maps a federation record to the event payload.
func BuildData(rec federation.ToolCallRecord) Data { //nolint:gocritic // value record by design
	d := Data{
		ToolCallID:      rec.ToolCallID,
		ToolName:        rec.ToolName,
		ServerID:        rec.ServerID,
		ObservedBy:      ObservedByProxy,
		Outcome:         rec.Outcome,
		DurationMS:      rec.Duration.Milliseconds(),
		ResultBytes:     rec.ResultBytes,
		ResultTokensEst: (rec.ResultBytes + bytesPerToken - 1) / bytesPerToken,
		ReducedBytes:    rec.ReducedBytes,
		Findings:        rec.Findings,
		ScanSkipped:     rec.ScanSkipped,
		OccurredAt:      rec.OccurredAt.UTC().Format(occurredAtLayout),
	}
	if d.Findings == nil {
		d.Findings = map[string]int{}
	}
	if id := rec.Identity; id != nil && id.Attributed {
		d.TenantID = nullable(id.TenantID)
		d.TokenID = nullable(id.TokenID)
		d.OwnerSub = nullable(id.OwnerSub)
		d.UserID = nullable(id.UserID)
		d.AgentID = nullable(id.AgentID)
		d.SourceApp = nullable(id.SourceApp)
		d.FlowID = nullable(id.FlowID)
		d.StepID = nullable(id.StepID)
	}
	return d
}

// BuildEvent wraps the payload in a go-events CloudEvent. The CE id is fresh
// per event; tool_call_id is the subject, and the join key, not the id, since
// the gateway emits its own event for the same call.
func BuildEvent(rec federation.ToolCallRecord) tasevents.Event { //nolint:gocritic // value record by design
	d := BuildData(rec)
	opts := []tasevents.Option{
		tasevents.WithTime(rec.OccurredAt),
		tasevents.WithSubject(rec.ToolCallID),
		tasevents.WithData(d),
	}
	if d.TenantID != nil {
		opts = append(opts, tasevents.WithTenant(*d.TenantID, ""))
	}
	if d.UserID != nil {
		opts = append(opts, tasevents.WithUser(*d.UserID))
	}
	return tasevents.New(EventType, Source, opts...)
}

// Encode returns the Kafka message for an event: kafkabind structured mode
// (content-type, ce_type), plus the ce-* binary-mode headers tas-llm-router
// sets on its AIQG events, so a consumer can route either way. Keyed by
// tool_call_id.
func Encode(e tasevents.Event) (kafka.Message, error) { //nolint:gocritic // value event by design
	value, headers, err := kafkabind.Encode(e)
	if err != nil {
		return kafka.Message{}, err
	}
	headers = append(headers,
		kafka.Header{Key: "ce-specversion", Value: []byte(tasevents.SpecVersion)},
		kafka.Header{Key: "ce-type", Value: []byte(e.Type)},
		kafka.Header{Key: "ce-id", Value: []byte(e.ID)},
		kafka.Header{Key: "ce-source", Value: []byte(e.Source)},
	)
	key := e.Subject
	if key == "" {
		key = e.ID
	}
	return kafka.Message{Key: []byte(key), Value: value, Headers: headers}, nil
}

// Writer is the subset of *kafka.Writer the publisher uses.
type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

var events = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tas_mcp",
	Name:      "tool_call_events_total",
	Help:      "Tool-call CloudEvents, by result: published, dropped (buffer full or closed), failed (encode or Kafka error).",
}, []string{"result"})

// Publisher is a federation.ToolCallObserver that publishes asynchronously.
type Publisher struct {
	w      Writer
	log    *zap.Logger
	ch     chan pending
	done   chan struct{}
	mu     sync.RWMutex
	closed bool
}

// NewPublisher starts a publisher writing through w. bufferSize<=0 uses
// DefaultBufferSize.
func NewPublisher(w Writer, bufferSize int, log *zap.Logger) *Publisher {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	if log == nil {
		log = zap.NewNop()
	}
	p := &Publisher{w: w, log: log, ch: make(chan pending, bufferSize), done: make(chan struct{})}
	go p.run()
	return p
}

// NewKafkaPublisher builds a publisher on a kafka-go Writer for brokers/topic.
func NewKafkaPublisher(brokers []string, topic string, bufferSize int, log *zap.Logger) (*Publisher, error) {
	if len(brokers) == 0 {
		return nil, errors.New("toolevents: at least one broker required")
	}
	if topic == "" {
		topic = DefaultTopic
	}
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		BatchTimeout: batchTimeout,
		WriteTimeout: writeTimeout,
		Compression:  kafka.Snappy,
	}
	return NewPublisher(w, bufferSize, log), nil
}

// ObserveToolCall implements federation.ToolCallObserver. Never blocks.
func (p *Publisher) ObserveToolCall(rec federation.ToolCallRecord) { //nolint:gocritic // interface signature
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		events.WithLabelValues("dropped").Inc()
		return
	}
	select {
	case p.ch <- pending{call: &rec}:
	default:
		events.WithLabelValues("dropped").Inc()
	}
}

// enqueueCatalogue is ObserveCatalogue's non-blocking send.
func (p *Publisher) enqueueCatalogue(e tasevents.Event) { //nolint:gocritic // value event
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		catalogueEvents.WithLabelValues("dropped").Inc()
		return
	}
	select {
	case p.ch <- pending{catalogue: &e}:
	default:
		catalogueEvents.WithLabelValues("dropped").Inc()
	}
}

// pending is one queued event: a tool call, or a prebuilt catalogue event.
type pending struct {
	call      *federation.ToolCallRecord
	catalogue *tasevents.Event
}

func (p *Publisher) run() {
	defer close(p.done)
	batch := make([]kafka.Message, 0, maxBatch)
	for it := range p.ch {
		batch = p.appendEncoded(batch[:0], it)
	drain:
		for len(batch) < maxBatch {
			select {
			case r, ok := <-p.ch:
				if !ok {
					break drain
				}
				batch = p.appendEncoded(batch, r)
			default:
				break drain
			}
		}
		p.write(batch)
	}
}

func (p *Publisher) appendEncoded(batch []kafka.Message, it pending) []kafka.Message {
	if it.catalogue != nil {
		msg, err := Encode(*it.catalogue)
		if err != nil {
			catalogueEvents.WithLabelValues("failed").Inc()
			p.log.Warn("tool catalogue event: encode failed", zap.Error(err))
			return batch
		}
		return append(batch, msg)
	}
	msg, err := Encode(BuildEvent(*it.call))
	if err != nil {
		events.WithLabelValues("failed").Inc()
		p.log.Warn("tool-call event: encode failed", zap.Error(err))
		return batch
	}
	return append(batch, msg)
}

func (p *Publisher) write(batch []kafka.Message) {
	if len(batch) == 0 {
		return
	}
	calls, cats := 0, 0
	for i := range batch {
		if isCatalogue(batch[i]) {
			cats++
		} else {
			calls++
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := p.w.WriteMessages(ctx, batch...); err != nil {
		events.WithLabelValues("failed").Add(float64(calls))
		catalogueEvents.WithLabelValues("failed").Add(float64(cats))
		p.log.Warn("tool events: publish failed; events dropped",
			zap.Int("events", len(batch)), zap.Error(err))
		return
	}
	events.WithLabelValues("published").Add(float64(calls))
	catalogueEvents.WithLabelValues("published").Add(float64(cats))
}

func isCatalogue(m kafka.Message) bool { //nolint:gocritic // read-only
	for _, h := range m.Headers {
		if h.Key == "ce-type" {
			return string(h.Value) == CatalogueEventType
		}
	}
	return false
}

// Close stops accepting events, flushes what is buffered (bounded wait), and
// closes the writer. Safe to call more than once.
func (p *Publisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.ch)
	p.mu.Unlock()

	select {
	case <-p.done:
	case <-time.After(closeTimeout):
		p.log.Warn("tool-call events: close timed out; buffered events dropped")
	}
	return p.w.Close()
}
