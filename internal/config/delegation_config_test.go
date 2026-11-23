package config

import "testing"

func TestDelegationConfig_DefaultOff(t *testing.T) {
	t.Setenv("MCP_DELEGATION_ENABLED", "")
	t.Setenv("MCP_DELEGATE_TOKEN", "x")
	if c := Load(); c.Delegation != nil {
		t.Fatalf("delegation should be nil by default, got %+v", c.Delegation)
	}
}

func TestDelegationConfig_FromEnv(t *testing.T) {
	t.Setenv("MCP_DELEGATION_ENABLED", "true")
	t.Setenv("MCP_DELEGATE_TOKEN", "tok")
	t.Setenv("MCP_DELEGATION_VERIFY_TOKEN", "vtok")
	t.Setenv("AIQG_DASHBOARD_URL", "http://aiqg:8095")
	t.Setenv("MCP_DELEGATE_NAME", "")
	c := Load().Delegation
	if c == nil || !c.Enabled || c.Token != "tok" || c.VerifyToken != "vtok" || c.DashboardURL != "http://aiqg:8095" ||
		c.Name != "aether-be" || c.CacheTTL != DefaultDelegationCacheTTL || c.Port != DefaultDelegationPort {
		t.Fatalf("got %+v", c)
	}
}
