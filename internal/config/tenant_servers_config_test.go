package config

import "testing"

func TestTenantServersConfig_DefaultOff(t *testing.T) {
	t.Setenv("MCP_TENANT_SERVERS", "")
	t.Setenv("MCP_ADMIN_TOKEN", "x")
	if c := Load(); c.TenantServers != nil {
		t.Fatalf("tenant servers should be nil by default, got %+v", c.TenantServers)
	}
}

func TestTenantServersConfig_FromEnv(t *testing.T) {
	t.Setenv("MCP_TENANT_SERVERS", "true")
	t.Setenv("MCP_ADMIN_TOKEN", "admin-tok")
	t.Setenv("MCP_TENANT_EGRESS_ALLOWLIST", `{"t1":["api.example.com","*.vendor.example"]}`)
	t.Setenv("MCP_TENANT_CLUSTER_NAMESPACES", "tas-mcp-servers,tenant-mcp")
	c := Load().TenantServers
	if c == nil || !c.Enabled || c.AdminToken != "admin-tok" || len(c.EgressAllowlist["t1"]) != 2 ||
		len(c.ClusterNamespaces) != 2 || c.EgressAllowlistErr != nil {
		t.Fatalf("got %+v", c)
	}
}

// An allowlist that does not parse fails closed: empty, with the error kept.
func TestTenantServersConfig_BadAllowlistFailsClosed(t *testing.T) {
	t.Setenv("MCP_TENANT_SERVERS", "true")
	t.Setenv("MCP_TENANT_EGRESS_ALLOWLIST", `{"t1":`)
	c := Load().TenantServers
	if c.EgressAllowlist != nil || c.EgressAllowlistErr == nil {
		t.Fatalf("got %+v", c)
	}
}
