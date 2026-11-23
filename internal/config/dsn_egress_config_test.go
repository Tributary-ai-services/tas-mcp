package config

import "testing"

// MCP-19: MCP_DSN_EGRESS_ALLOW rides on the connection injection config, raw;
// federation.ParseDSNEgressAllow validates it at install.
func TestConnectionInjection_DSNEgressAllow(t *testing.T) {
	t.Setenv("MCP_CONNECTION_INJECTION", "true")
	t.Setenv("MCP_DSN_EGRESS_ALLOW", "postgres-shared.tas-shared.svc.cluster.local:5432")
	c := Load().ConnectionInjection
	if c == nil || c.DSNEgressAllow != "postgres-shared.tas-shared.svc.cluster.local:5432" {
		t.Fatalf("got %+v", c)
	}
	t.Setenv("MCP_DSN_EGRESS_ALLOW", "")
	if c := Load().ConnectionInjection; c.DSNEgressAllow != "" {
		t.Fatalf("default should be empty (deny), got %q", c.DSNEgressAllow)
	}
}
