package config

import (
	"os"
	"testing"
)

func TestLoad_ReductionDisabledByDefault(t *testing.T) {
	os.Unsetenv("REDUCTION_ENABLED")
	if c := Load(); c.Reduction != nil {
		t.Errorf("Reduction should be nil when REDUCTION_ENABLED is unset, got %+v", c.Reduction)
	}
}

func TestLoad_ReductionEnabled(t *testing.T) {
	env := map[string]string{
		"REDUCTION_ENABLED":          "true",
		"REDUCTION_OLLAMA_URL":       "http://ollama.tas:11434",
		"REDUCTION_SLM_ENABLED":      "true",
		"REDUCTION_SLM_PROVIDER":     "anthropic",
		"REDUCTION_SLM_MODEL":        "claude-haiku-4-5-20251001",
		"REDUCTION_MAX_OUTPUT_BYTES": "4096",
	}
	for k, v := range env {
		os.Setenv(k, v)
	}
	defer func() {
		for k := range env {
			os.Unsetenv(k)
		}
	}()

	rc := Load().Reduction
	if rc == nil {
		t.Fatal("Reduction should be loaded when REDUCTION_ENABLED=true")
	}
	if !rc.Enabled {
		t.Error("Enabled should be true")
	}
	if rc.OllamaURL != "http://ollama.tas:11434" {
		t.Errorf("OllamaURL = %q", rc.OllamaURL)
	}
	if !rc.SLMEnabled || rc.SLMProvider != "anthropic" || rc.SLMModel != "claude-haiku-4-5-20251001" {
		t.Errorf("SLM fields not loaded: %+v", rc)
	}
	if rc.MaxOutputBytes != 4096 {
		t.Errorf("MaxOutputBytes = %d, want 4096", rc.MaxOutputBytes)
	}
}

func TestLoad_ReductionDefaultsOllamaURL(t *testing.T) {
	os.Setenv("REDUCTION_ENABLED", "true")
	os.Unsetenv("REDUCTION_OLLAMA_URL")
	defer os.Unsetenv("REDUCTION_ENABLED")

	rc := Load().Reduction
	if rc == nil || rc.OllamaURL != DefaultReductionOllamaURL {
		t.Errorf("OllamaURL should default to %q, got %+v", DefaultReductionOllamaURL, rc)
	}
}
