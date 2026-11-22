package toolevents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

const napkinID = "napkin-mcp"

var probedAt = time.Date(2026, 11, 21, 16, 4, 5, 123_000_000, time.UTC)

func napkinCatalogue(at time.Time, hash string) federation.ToolCatalogue {
	return federation.ToolCatalogue{
		ServerID:   napkinID,
		ObservedAt: at,
		Hash:       hash,
		Tools: []federation.CatalogueTool{
			{Name: "check_visual_status", SchemaBytes: 112},
			{Name: "generate_visual", SchemaBytes: 431},
			{Name: "list_styles", SchemaBytes: 0},
		},
	}
}

// The data contract is fixed with the Spark writer (tas-spark-jobs
// TOOL_CATALOGUE_DATA). This golden is that contract.
func TestCatalogueData_Golden(t *testing.T) {
	golden(t, "tool_catalogue.golden.json", BuildCatalogueData(napkinCatalogue(probedAt, "h1")))
}

func TestCatalogueData_FieldSetAndHour(t *testing.T) {
	b, _ := json.Marshal(BuildCatalogueData(napkinCatalogue(probedAt, "h1")))
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"snapshot_at", "server_id", "observed_by", "tenant_id", "tools"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if len(m) != 5 {
		t.Errorf("field count %d, want 5: %v", len(m), m)
	}
	if m["tenant_id"] != nil || m["observed_by"] != "proxy" || m["snapshot_at"] != "2026-11-21T16:00:00Z" {
		t.Errorf("tenant/observed_by/snapshot_at = %v %v %v", m["tenant_id"], m["observed_by"], m["snapshot_at"])
	}
	empty := napkinCatalogue(probedAt, "h0")
	empty.Tools = nil
	b, _ = json.Marshal(BuildCatalogueData(empty))
	_ = json.Unmarshal(b, &m)
	if tools, ok := m["tools"].([]any); !ok || len(tools) != 0 {
		t.Errorf("an empty toolset must be [] not null: %v", m["tools"])
	}
}

func TestCatalogueEvent_EnvelopeAndKafkaMessage(t *testing.T) {
	e := BuildCatalogueEvent(napkinCatalogue(probedAt, "h1"))
	if e.Type != CatalogueEventType || e.Source != Source || e.Subject != napkinID || e.TenantID != "" {
		t.Fatalf("envelope %+v", e)
	}
	msg, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if string(msg.Key) != napkinID || !isCatalogue(msg) {
		t.Fatalf("key %q catalogue=%v", msg.Key, isCatalogue(msg))
	}
	var env struct {
		Type string        `json:"type"`
		Data CatalogueData `json:"data"`
	}
	if err := json.Unmarshal(msg.Value, &env); err != nil || env.Type != CatalogueEventType || len(env.Data.Tools) != 3 {
		t.Fatalf("value: %v %s", err, msg.Value)
	}
}

// On change, and once an hour; never on every probe.
func TestCatalogueEmitter_ChangeAndHourly(t *testing.T) {
	w := &fakeWriter{}
	p := NewPublisher(w, 64, nil)
	e := NewCatalogueEmitter(p)

	e.ObserveCatalogue(napkinCatalogue(probedAt, "h1"))                    // first: emit
	e.ObserveCatalogue(napkinCatalogue(probedAt.Add(time.Minute), "h1"))   // same hour, same set: skip
	e.ObserveCatalogue(napkinCatalogue(probedAt.Add(2*time.Minute), "h2")) // changed: emit
	e.ObserveCatalogue(napkinCatalogue(probedAt.Add(3*time.Minute), "h2")) // skip
	e.ObserveCatalogue(napkinCatalogue(probedAt.Add(time.Hour), "h2"))     // new hour: emit
	other := napkinCatalogue(probedAt, "h1")
	other.ServerID = "dbhub"
	e.ObserveCatalogue(other) // another server: emit
	_ = p.Close()

	if len(w.written) != 4 {
		t.Fatalf("published %d catalogue events, want 4", len(w.written))
	}
	var env struct {
		Data CatalogueData `json:"data"`
	}
	_ = json.Unmarshal(w.written[2].Value, &env)
	if env.Data.SnapshotAt != "2026-11-21T17:00:00Z" {
		t.Errorf("hourly snapshot_at = %s", env.Data.SnapshotAt)
	}
}

// Catalogue and tool-call events share the publisher and its bound.
func TestPublisher_MixedEventsAndDropWhenClosed(t *testing.T) {
	w := &fakeWriter{}
	p := NewPublisher(w, 16, nil)
	p.ObserveToolCall(attributedRecord())
	p.enqueueCatalogue(BuildCatalogueEvent(napkinCatalogue(probedAt, "h1")))
	_ = p.Close()
	if len(w.written) != 2 || isCatalogue(w.written[0]) || !isCatalogue(w.written[1]) {
		t.Fatalf("written = %d", len(w.written))
	}
	p.enqueueCatalogue(BuildCatalogueEvent(napkinCatalogue(probedAt, "h1"))) // closed: dropped, no panic
}
