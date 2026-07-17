// Package scanning adapts Gatekeeper's content scanner (pkg/scan) into the
// narrow BlockScanner port the federation boundary depends on. Keeping the
// Gatekeeper import here (not in the federation package) keeps the federation
// hot path and its tests free of the cross-module dependency — the same split
// the reduction package uses for the extractor.
//
// This is the MCP-proxy trust boundary (docs/AIQG-GATEKEEPER-INTEGRATION.md §2,
// tas-llm-router#101 G2): federated tool results are TierExternal content on
// their way into an LLM prompt, and this is the only place that scans them.
package scanning

import (
	"context"
	"fmt"

	"github.com/Tributary-ai-services/Gatekeeper/pkg/scan"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

// Config configures the Gatekeeper-backed scanner.
type Config struct {
	// Enabled turns boundary scanning on. When false, New returns (nil, nil) and
	// the caller leaves the Manager with no scanner (scanning off).
	Enabled bool

	// Redact controls whether findings are redacted in place, or only reported.
	// Default false = LOG-ONLY: scan and emit findings, modify nothing. This is
	// the draft→enforce first stage — going from 0 to 21 matchers on federated
	// tool output will surface new findings, and a false positive that silently
	// removes real tool output is worse than a missed one until the finding rate
	// is understood.
	Redact bool

	// RedactStrategy is the redaction applied when Redact is true. Restricted to
	// deterministic, infra-free strategies: "mask" (j***@***.com), "replace"
	// ([EMAIL]), "hash" ([HASH:...]). Tokenize is deliberately NOT offered — it
	// needs Databunker (not deployed) and would mint per-call tokens that break
	// prompt caching. Empty → "mask". See docs/AIQG-GATEKEEPER-INTEGRATION.md §3.
	RedactStrategy string

	// Profile selects the scan profile ("full", "pii_only", "injection_only",
	// "compliance"). Empty → full.
	Profile string

	// MinConfidence is the detection threshold below which matches are dropped.
	// 0 → Gatekeeper default (0.7).
	MinConfidence float64
}

// Scanner is the Gatekeeper-backed BlockScanner. One scanner instance serves all
// trust tiers; the tier is applied per Scan call via the ScanConfig.
type Scanner struct {
	sc       scan.Scanner
	engine   scan.RedactionEngine
	profile  scan.ScanProfile
	minConf  float64
	redact   bool
	strategy scan.RedactionStrategy
}

// New builds a Scanner from cfg. Returns (nil, nil) when scanning is disabled so
// the caller can simply skip installing it. A non-nil error means the scanner
// could not be constructed; the caller should treat that as "scanning
// unavailable" and leave the boundary unscanned rather than fail startup —
// except that, unlike reduction, an unscanned boundary is a security gap, so the
// caller SHOULD log this loudly (see Install).
func New(cfg Config) (*Scanner, error) { //nolint:gocritic // startup-only config
	if !cfg.Enabled {
		return nil, nil
	}

	sc := scan.NewScanner()
	if sc == nil {
		return nil, fmt.Errorf("gatekeeper NewScanner returned nil")
	}

	return &Scanner{
		sc:       sc,
		engine:   scan.NewRedactionEngine(),
		profile:  parseProfile(cfg.Profile),
		minConf:  cfg.MinConfidence,
		redact:   cfg.Redact,
		strategy: parseStrategy(cfg.RedactStrategy),
	}, nil
}

// Scan scans one tool-result text block and, when redaction is enabled, returns
// the redacted form. It satisfies federation.BlockScanner.
//
// Deterministic: Gatekeeper's redaction strategies are content-derived
// (mask/replace/hash), so identical (content, tier) yields identical output —
// the redacted block is byte-stable across turns, keeping prompt caching intact.
func (s *Scanner) Scan(ctx context.Context, content string, tier federation.TrustTier) (federation.ScanOutcome, error) {
	if s == nil || s.sc == nil {
		return federation.ScanOutcome{Content: content}, nil
	}

	cfg := scan.DefaultScanConfig()
	cfg.Profile = s.profile
	cfg.TrustTier = toGatekeeperTier(tier)
	if s.minConf > 0 {
		cfg.MinConfidence = s.minConf
	}
	// Never tokenize (needs Databunker); force our deterministic strategy so a
	// stray default can't route redaction through the vault path.
	cfg.RedactionMode = s.strategy

	result, err := s.sc.ScanString(ctx, content, cfg)
	if err != nil {
		return federation.ScanOutcome{}, fmt.Errorf("scan: %w", err)
	}
	if result == nil || len(result.Findings) == 0 {
		return federation.ScanOutcome{Content: content}, nil
	}

	out := federation.ScanOutcome{
		Content:  content,
		Findings: mapFindings(result.Findings),
	}

	if s.redact {
		redacted, rerr := s.engine.Redact(content, result.Findings, s.strategy)
		if rerr != nil {
			// Redaction failed but detection succeeded: surface the findings,
			// keep the original content (fail-open on the mutation, never on the
			// signal). The processor's log-only guard would do the same.
			return out, nil
		}
		out.Content = redacted
		out.Redacted = true
	}
	return out, nil
}

// mapFindings projects Gatekeeper findings onto the federation-local shape,
// carrying only log-safe fields — never the raw matched value (Gatekeeper's
// ValuePreview is already masked; Value is dropped here entirely).
func mapFindings(fs []scan.Finding) []federation.ScanFinding {
	out := make([]federation.ScanFinding, 0, len(fs))
	for i := range fs {
		f := &fs[i]
		var fw []string
		for _, m := range f.Frameworks {
			fw = append(fw, string(m.Framework))
		}
		out = append(out, federation.ScanFinding{
			PatternID:   f.PatternID,
			PatternType: string(f.PatternType),
			Severity:    string(f.Severity),
			Preview:     f.ValuePreview,
			Frameworks:  fw,
		})
	}
	return out
}

func toGatekeeperTier(t federation.TrustTier) scan.TrustTier {
	switch t {
	case federation.TierInternal:
		return scan.TierInternal
	case federation.TierPartner:
		return scan.TierPartner
	default:
		return scan.TierExternal // safest default
	}
}

func parseProfile(p string) scan.ScanProfile {
	switch p {
	case "pii_only":
		return scan.ProfilePIIOnly
	case "injection_only":
		return scan.ProfileInjectionOnly
	case "compliance":
		return scan.ProfileCompliance
	default:
		return scan.ProfileFull
	}
}

// parseStrategy maps a config string to a deterministic, infra-free redaction
// strategy. Tokenize and remove are excluded: tokenize needs Databunker, and
// remove would silently delete tool output. Anything unrecognized → mask.
func parseStrategy(s string) scan.RedactionStrategy {
	switch s {
	case "replace":
		return scan.RedactionReplace
	case "hash":
		return scan.RedactionHash
	default:
		return scan.RedactionMask
	}
}
