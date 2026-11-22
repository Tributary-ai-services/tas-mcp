package config

import (
	"testing"
	"time"
)

func TestIdentityConfig_DefaultOff(t *testing.T) {
	t.Setenv("IDENTITY_ENABLED", "")
	if c := Load(); c.Identity != nil {
		t.Fatalf("identity should be nil by default, got %+v", c.Identity)
	}
}

func TestIdentityConfig_FromEnv(t *testing.T) {
	t.Setenv("IDENTITY_ENABLED", "true")
	t.Setenv("AIQG_DASHBOARD_URL", "http://dash:8095")
	t.Setenv("AIQG_DASHBOARD_INTERNAL_AUTH_TOKEN", "s3cret")
	c := Load()
	if c.Identity == nil || !c.Identity.Enabled || c.Identity.DashboardURL != "http://dash:8095" ||
		c.Identity.DashboardInternalAuthToken != "s3cret" || c.Identity.CacheTTL != 60*time.Second {
		t.Fatalf("got %+v", c.Identity)
	}
	t.Setenv("IDENTITY_CACHE_TTL", "5s")
	if c := Load(); c.Identity.CacheTTL != 5*time.Second {
		t.Fatalf("ttl = %v", c.Identity.CacheTTL)
	}
}

func TestToolCallEventsConfig(t *testing.T) {
	t.Setenv("TOOL_CALL_EVENTS_ENABLED", "")
	if c := Load(); c.ToolCallEvents != nil {
		t.Fatalf("tool-call events should be nil by default, got %+v", c.ToolCallEvents)
	}
	t.Setenv("TOOL_CALL_EVENTS_ENABLED", "true")
	t.Setenv("AIQG_KAFKA_BROKERS", "k1:9092,k2:9092")
	c := Load().ToolCallEvents
	if c == nil || len(c.Brokers) != 2 || c.Topic != "tas.aiqg.events.v1" || c.BufferSize != 1024 {
		t.Fatalf("got %+v", c)
	}
}

func TestScanAttestConfig(t *testing.T) {
	t.Setenv("SCANNING_ENABLED", "true")
	t.Setenv("SCAN_ATTEST_ENABLED", "")
	if c := Load(); c.Scanning == nil || c.Scanning.Attest != nil {
		t.Fatalf("attest must be off by default: %+v", c.Scanning)
	}
	t.Setenv("SCAN_ATTEST_ENABLED", "true")
	t.Setenv("SCAN_ATTEST_GATEWAY_BUILDS", "d6f4b1a,abc1234")
	a := Load().Scanning.Attest
	if a == nil || !a.Enabled || len(a.GatewayBuilds) != 2 || a.TTL != 24*time.Hour || a.RedisURL != DefaultFederationRedisURL || a.GatewayTier != "internal" {
		t.Fatalf("got %+v", a)
	}
	t.Setenv("FEDERATION_REDIS_URL", "redis://r:6379/0")
	if a := Load().Scanning.Attest; a.RedisURL != "redis://r:6379/0" {
		t.Fatalf("redis fallback = %s", a.RedisURL)
	}
	t.Setenv("SCAN_ATTEST_REDIS_URL", "redis://s:6379/0")
	t.Setenv("SCAN_ATTEST_TTL", "1h")
	if a := Load().Scanning.Attest; a.RedisURL != "redis://s:6379/0" || a.TTL != time.Hour {
		t.Fatalf("overrides = %+v", a)
	}
}
