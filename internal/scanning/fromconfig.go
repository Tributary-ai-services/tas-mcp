package scanning

import "github.com/tributary-ai-services/tas-mcp/internal/config"

// FromConfig maps the app-level config.ScanningConfig onto the scanner's Config.
// It's the seam that keeps internal/config free of the Gatekeeper scan
// dependency: config describes the knobs, this package (which imports Gatekeeper)
// translates them.
//
// A nil argument — the default when SCANNING_ENABLED is unset — yields a disabled
// Config, so New/Install become no-ops:
//
//	sc := scanning.Install(mgr, scanning.FromConfig(cfg.Scanning), logger)
func FromConfig(sc *config.ScanningConfig) Config {
	if sc == nil {
		return Config{Enabled: false}
	}
	return Config{
		Enabled:        sc.Enabled,
		Redact:         sc.Redact,
		RedactStrategy: sc.RedactStrategy,
		Profile:        sc.Profile,
		MinConfidence:  sc.MinConfidence,
	}
}
