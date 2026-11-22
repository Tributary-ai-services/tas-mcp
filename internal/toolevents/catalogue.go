package toolevents

// Tool catalogue snapshots (Plan #22 T.4, design §9.5 "Never called").
//
// After each successful server probe the federation prober hands over the
// server's tools/list. CatalogueEmitter publishes it as one
// com.tas.aiqg.tool_catalogue.v1 event when the toolset changed since the last
// event for that server, and otherwise once per clock hour. The event's
// snapshot_at is the hour, so a re-sent or duplicate snapshot (every proxy
// replica probes, and each emits its own) lands on the same
// aiqg.tool_catalogue key and is a no-op at the writer (aether-shared
// migration 009, unique on snapshot_at, server_id, tool_name, observed_by,
// tenant_id).
//
// The data contract is fixed with the Spark writer (tas-spark-jobs
// TOOL_CATALOGUE_DATA); change it only together.

import (
	"sync"
	"time"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

// CatalogueEventType is the CloudEvent type of a catalogue snapshot.
const CatalogueEventType = "com.tas.aiqg.tool_catalogue.v1"

// snapshotLayout is RFC 3339 in UTC, to the second (the value is an hour).
const snapshotLayout = "2006-01-02T15:04:05Z07:00"

// CatalogueTool is one tool in a snapshot.
type CatalogueTool struct {
	Name        string `json:"name"`
	SchemaBytes int    `json:"schema_bytes"`
}

// CatalogueData is the catalogue event's data payload. tenant_id is always
// null from the proxy: the federated set has no tenant.
type CatalogueData struct {
	SnapshotAt string          `json:"snapshot_at"`
	ServerID   string          `json:"server_id"`
	ObservedBy string          `json:"observed_by"`
	TenantID   *string         `json:"tenant_id"`
	Tools      []CatalogueTool `json:"tools"`
}

var catalogueEvents = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tas_mcp",
	Name:      "tool_catalogue_events_total",
	Help:      "Tool-catalogue CloudEvents, by result: published, dropped (buffer full or closed), failed (encode or Kafka error).",
}, []string{"result"})

// BuildCatalogueData maps a probe's catalogue to the event payload.
func BuildCatalogueData(c federation.ToolCatalogue) CatalogueData { //nolint:gocritic // value by design
	tools := make([]CatalogueTool, 0, len(c.Tools))
	for _, t := range c.Tools {
		tools = append(tools, CatalogueTool{Name: t.Name, SchemaBytes: t.SchemaBytes})
	}
	return CatalogueData{
		SnapshotAt: c.ObservedAt.UTC().Truncate(time.Hour).Format(snapshotLayout),
		ServerID:   c.ServerID,
		ObservedBy: ObservedByProxy,
		Tools:      tools,
	}
}

// BuildCatalogueEvent wraps the payload in a go-events CloudEvent, subject =
// server id (also the Kafka key).
func BuildCatalogueEvent(c federation.ToolCatalogue) tasevents.Event { //nolint:gocritic // value by design
	return tasevents.New(CatalogueEventType, Source,
		tasevents.WithTime(c.ObservedAt),
		tasevents.WithSubject(c.ServerID),
		tasevents.WithData(BuildCatalogueData(c)))
}

// catalogueSink is where the emitter sends events (the Publisher).
type catalogueSink interface {
	enqueueCatalogue(e tasevents.Event)
}

// CatalogueEmitter is a federation.CatalogueObserver that publishes a server's
// catalogue on change and hourly.
type CatalogueEmitter struct {
	sink catalogueSink
	mu   sync.Mutex
	last map[string]emitted
}

type emitted struct {
	hash string
	hour time.Time
}

// NewCatalogueEmitter publishes through p.
func NewCatalogueEmitter(p *Publisher) *CatalogueEmitter {
	return &CatalogueEmitter{sink: p, last: map[string]emitted{}}
}

// ObserveCatalogue implements federation.CatalogueObserver. Never blocks.
func (e *CatalogueEmitter) ObserveCatalogue(c federation.ToolCatalogue) { //nolint:gocritic // interface signature
	hour := c.ObservedAt.UTC().Truncate(time.Hour)
	e.mu.Lock()
	prev, ok := e.last[c.ServerID]
	if ok && prev.hash == c.Hash && prev.hour.Equal(hour) {
		e.mu.Unlock()
		return
	}
	e.last[c.ServerID] = emitted{hash: c.Hash, hour: hour}
	e.mu.Unlock()
	e.sink.enqueueCatalogue(BuildCatalogueEvent(c))
}
