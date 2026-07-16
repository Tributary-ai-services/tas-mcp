package config

import (
	"os"
	"testing"
)

func TestLoad_FederationNilByDefault(t *testing.T) {
	os.Unsetenv("FEDERATION_REGISTRY")
	if c := Load(); c.Federation != nil {
		t.Errorf("Federation should be nil when FEDERATION_REGISTRY is unset, got %+v", c.Federation)
	}
}

func TestLoad_FederationRedis(t *testing.T) {
	os.Setenv("FEDERATION_REGISTRY", "redis")
	os.Setenv("FEDERATION_REDIS_URL", "redis://custom:6379/1")
	defer func() {
		os.Unsetenv("FEDERATION_REGISTRY")
		os.Unsetenv("FEDERATION_REDIS_URL")
	}()

	fc := Load().Federation
	if fc == nil {
		t.Fatal("Federation should be loaded when FEDERATION_REGISTRY=redis")
	}
	if fc.Registry != "redis" || fc.RedisURL != "redis://custom:6379/1" {
		t.Errorf("unexpected federation config: %+v", fc)
	}
}

func TestLoad_FederationRedisURLDefault(t *testing.T) {
	os.Setenv("FEDERATION_REGISTRY", "redis")
	os.Unsetenv("FEDERATION_REDIS_URL")
	defer os.Unsetenv("FEDERATION_REGISTRY")

	fc := Load().Federation
	if fc == nil || fc.RedisURL != DefaultFederationRedisURL {
		t.Errorf("RedisURL should default to %q, got %+v", DefaultFederationRedisURL, fc)
	}
}
