package reduction

import (
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

// Install wires cache-safe reduce-at-source into a federation Manager from cfg.
// It is the single call a Manager owner makes to turn reduction on; until it is
// made (or when cfg.Enabled is false), the Manager stays on its no-op processor
// and tool results pass through untouched.
//
// Fail-safe: if the extractor cannot be built, Install logs and leaves the
// Manager on the no-op processor rather than returning the error up the startup
// path — reduce-at-source is an optimization and must never keep the gateway
// from starting. It returns the *Reducer (nil when disabled or on build
// failure) so the caller can Close() it during shutdown.
func Install(m *federation.Manager, cfg Config, logger *zap.Logger) *Reducer { //nolint:gocritic // startup-only config
	if m == nil || !cfg.Enabled {
		return nil
	}

	r, err := New(cfg)
	if err != nil {
		if logger != nil {
			logger.Warn("reduce-at-source: unavailable; reduction disabled",
				zap.Error(err))
		}
		return nil
	}
	if r == nil {
		return nil
	}

	m.SetResultProcessor(federation.NewReducingResultProcessor(r, logger))
	if logger != nil {
		logger.Info("reduce-at-source: enabled",
			zap.Bool("slm", cfg.SLMEnabled),
			zap.String("slm_provider", cfg.SLMProvider))
	}
	return r
}
