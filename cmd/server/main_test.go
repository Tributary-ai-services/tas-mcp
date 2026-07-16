package main

import (
	"testing"

	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/config"
)

// The gateway always hosts a federation Manager (so the FederatedMCPServer CRD
// controller has an instance to register servers into), regardless of whether
// reduce-at-source is enabled.
func TestSetupFederation_AlwaysBuildsManager(t *testing.T) {
	mgr, reducer := setupFederation(&config.Config{}, zap.NewNop())
	if mgr == nil {
		t.Fatal("setupFederation must always return a Manager")
	}
	if reducer != nil {
		t.Error("reducer should be nil when Reduction config is absent")
		_ = reducer.Close()
	}
}

func TestSetupFederation_InstallsReducerWhenEnabled(t *testing.T) {
	cfg := &config.Config{
		Reduction: &config.ReductionConfig{Enabled: true, OllamaURL: "http://ollama:11434", SLMEnabled: true},
	}
	mgr, reducer := setupFederation(cfg, zap.NewNop())
	if mgr == nil {
		t.Fatal("setupFederation must always return a Manager")
	}
	if reducer == nil {
		t.Fatal("reducer should be installed when Reduction.Enabled is true")
	}
	t.Cleanup(func() { _ = reducer.Close() })
}
