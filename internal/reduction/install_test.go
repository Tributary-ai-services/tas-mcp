package reduction

import (
	"testing"

	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

func TestInstall_DisabledLeavesNoop(t *testing.T) {
	m := federation.NewManagerWithDefaults(zap.NewNop())
	r := Install(m, Config{Enabled: false}, zap.NewNop())
	if r != nil {
		t.Error("disabled config should install nothing and return a nil reducer")
	}
}

func TestInstall_NilManagerIsSafe(t *testing.T) {
	if r := Install(nil, Config{Enabled: true}, zap.NewNop()); r != nil {
		t.Error("nil manager must be a safe no-op")
	}
}

func TestInstall_EnabledReturnsReducer(t *testing.T) {
	m := federation.NewManagerWithDefaults(zap.NewNop())
	r := Install(m, Config{Enabled: true, OllamaURL: "http://ollama:11434", SLMEnabled: true}, zap.NewNop())
	if r == nil {
		t.Fatal("enabled config should install a processor and return the reducer")
	}
	t.Cleanup(func() { _ = r.Close() })
}
