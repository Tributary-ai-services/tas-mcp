package reduction

import "github.com/tributary-ai-services/tas-mcp/internal/config"

// FromConfig maps the app-level config.ReductionConfig onto the reducer's
// Config. It's the seam that keeps internal/config free of the Gatekeeper
// extractor dependency: config describes the knobs, this package (which already
// imports Gatekeeper) translates them.
//
// A nil argument — the default when REDUCTION_ENABLED is unset — yields a
// disabled Config, so New/Install become no-ops. This makes the full wiring one
// line:
//
//	red := reduction.Install(mgr, reduction.FromConfig(cfg.Reduction), logger)
func FromConfig(rc *config.ReductionConfig) Config {
	if rc == nil {
		return Config{Enabled: false}
	}
	return Config{
		Enabled:        rc.Enabled,
		EmbedModel:     rc.EmbedModel,
		OllamaURL:      rc.OllamaURL,
		MinContentSize: rc.MinContentSize,
		SLMEnabled:     rc.SLMEnabled,
		SLMProvider:    rc.SLMProvider,
		SLMBaseURL:     rc.SLMBaseURL,
		SLMModel:       rc.SLMModel,
		SLMAPIKey:      rc.SLMAPIKey,
		SLMMaxTokens:   rc.SLMMaxTokens,
		MaxOutputBytes: rc.MaxOutputBytes,
	}
}
