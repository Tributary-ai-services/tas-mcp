package reduction

import (
	"testing"

	"github.com/tributary-ai-services/tas-mcp/internal/config"
)

func TestFromConfig_NilIsDisabled(t *testing.T) {
	if got := FromConfig(nil); got.Enabled {
		t.Error("nil app config should map to a disabled reducer Config")
	}
}

func TestFromConfig_MapsAllFields(t *testing.T) {
	rc := &config.ReductionConfig{
		Enabled:        true,
		EmbedModel:     "all-MiniLM-L6-v2",
		OllamaURL:      "http://ollama:11434",
		MinContentSize: 1024,
		SLMEnabled:     true,
		SLMProvider:    "openai",
		SLMBaseURL:     "https://api.openai.com/v1",
		SLMModel:       "gpt-4o-mini",
		SLMAPIKey:      "sk-test",
		SLMMaxTokens:   2048,
		MaxOutputBytes: 8192,
	}
	got := FromConfig(rc)

	want := Config{
		Enabled:        true,
		EmbedModel:     "all-MiniLM-L6-v2",
		OllamaURL:      "http://ollama:11434",
		MinContentSize: 1024,
		SLMEnabled:     true,
		SLMProvider:    "openai",
		SLMBaseURL:     "https://api.openai.com/v1",
		SLMModel:       "gpt-4o-mini",
		SLMAPIKey:      "sk-test",
		SLMMaxTokens:   2048,
		MaxOutputBytes: 8192,
	}
	if got != want {
		t.Errorf("FromConfig mismatch:\n got = %+v\nwant = %+v", got, want)
	}
}

// FromConfig(cfg.Reduction) → Install must produce a working reducer end to end.
func TestFromConfig_FeedsNew(t *testing.T) {
	rc := &config.ReductionConfig{Enabled: true, OllamaURL: "http://ollama:11434", SLMEnabled: true}
	r, err := New(FromConfig(rc))
	if err != nil {
		t.Fatalf("New from mapped config failed: %v", err)
	}
	if r == nil {
		t.Fatal("expected a reducer from an enabled config")
	}
	_ = r.Close()
}
