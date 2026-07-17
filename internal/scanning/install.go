package scanning

import (
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

// Install wires Gatekeeper boundary scanning into a federation Manager from cfg.
// It is the single call a Manager owner makes to turn scanning on; until it is
// made (or when cfg.Enabled is false), the Manager has no scanner and federated
// tool results pass the boundary unscanned.
//
// Unlike reduction, scanning is a SECURITY control, and an unavailable scanner
// is a gap, not merely a missed optimization. Install still fails safe — it does
// not take the gateway down if the scanner can't be built — but it logs at Warn
// so the gap is visible, not silent. It returns the *Scanner (nil when disabled
// or on build failure); the scanner holds no resources needing Close.
func Install(m *federation.Manager, cfg Config, logger *zap.Logger) *Scanner { //nolint:gocritic // startup-only config
	if m == nil || !cfg.Enabled {
		return nil
	}

	s, err := New(cfg)
	if err != nil {
		if logger != nil {
			logger.Warn("boundary scanning: unavailable; federated tool results will NOT be scanned",
				zap.Error(err))
		}
		return nil
	}
	if s == nil {
		return nil
	}

	m.SetScanner(federation.NewScanningResultProcessor(
		s,
		nil, // tierFor: every federated server is TierExternal for now (per-server
		//        trust tier is a follow-up; External is the safe default).
		findingSink(logger, cfg.Redact),
		cfg.Redact,
	))

	mode := "log-only"
	if cfg.Redact {
		mode = "redact:" + orDefault(cfg.RedactStrategy, "mask")
	}
	if logger != nil {
		logger.Info("boundary scanning: enabled",
			zap.String("mode", mode),
			zap.String("profile", orDefault(cfg.Profile, "full")))
	}
	return s
}

// findingSink logs one structured line per tool-result block that produced
// findings. The line is safe to emit — Preview is masked and no raw value is
// carried (see mapFindings). This is the observe side of the draft→enforce
// staging: in log-only mode it is the ONLY effect, so the finding rate can be
// measured before any redaction is turned on.
func findingSink(logger *zap.Logger, redact bool) federation.FindingSink {
	if logger == nil {
		return nil
	}
	return func(serverID string, findings []federation.ScanFinding, redacted bool) {
		// Aggregate by pattern so a noisy block is one line, not one per match.
		byPattern := map[string]int{}
		maxSev := ""
		for _, f := range findings {
			byPattern[f.PatternID]++
			if severityRank(f.Severity) > severityRank(maxSev) {
				maxSev = f.Severity
			}
		}
		logger.Warn("boundary scan findings",
			zap.String("server_id", serverID),
			zap.Int("finding_count", len(findings)),
			zap.String("max_severity", maxSev),
			zap.Any("patterns", byPattern),
			zap.Bool("redacted", redacted),
			zap.Bool("redact_enabled", redact),
		)
	}
}

// Severity ranks, highest first. Used to pick the max-severity finding when
// deciding whether a scan result crosses the block/log threshold.
const (
	rankNone     = 0
	rankLow      = 1
	rankMedium   = 2
	rankHigh     = 3
	rankCritical = 4
)

func severityRank(s string) int {
	switch s {
	case "critical":
		return rankCritical
	case "high":
		return rankHigh
	case "medium":
		return rankMedium
	case "low":
		return rankLow
	default:
		return rankNone
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
