package config

import (
	"testing"
	"time"
)

func TestServerProbesConfig(t *testing.T) {
	t.Setenv("SERVER_PROBES_ENABLED", "")
	t.Setenv("TOOL_CATALOGUE_EVENTS_ENABLED", "")
	if c := Load(); c.ServerProbes != nil || c.ToolCatalogueEvents != nil {
		t.Fatalf("probes and catalogue must be off by default: %+v %+v", c.ServerProbes, c.ToolCatalogueEvents)
	}
	t.Setenv("SERVER_PROBES_ENABLED", "true")
	c := Load().ServerProbes
	if c == nil || c.Interval != 60*time.Second || c.Timeout != 10*time.Second || c.Concurrency != 4 {
		t.Fatalf("defaults: %+v", c)
	}
	t.Setenv("SERVER_PROBE_INTERVAL", "30s")
	t.Setenv("SERVER_PROBE_TIMEOUT", "3s")
	t.Setenv("SERVER_PROBE_CONCURRENCY", "2")
	if c := Load().ServerProbes; c.Interval != 30*time.Second || c.Timeout != 3*time.Second || c.Concurrency != 2 {
		t.Fatalf("overrides: %+v", c)
	}
	t.Setenv("TOOL_CATALOGUE_EVENTS_ENABLED", "true")
	t.Setenv("AIQG_KAFKA_BROKERS", "k1:9092")
	if c := Load().ToolCatalogueEvents; c == nil || len(c.Brokers) != 1 || c.Topic != DefaultToolCallEventsTopic {
		t.Fatalf("catalogue: %+v", c)
	}
}
