package federation

import (
	"context"
)

// TrustTier is the federation-local trust classification of a federated MCP
// server's output. It mirrors Gatekeeper's scan.TrustTier but is defined here
// so the federation hot path carries no Gatekeeper dependency (same reason the
// Reducer port is structural — see reducing_processor.go). The scanning adapter
// maps this onto Gatekeeper's own tier.
//
// Federated tool output is untrusted by default: a tool result is content the
// gateway did not author, arriving from a server it does not control, on its way
// into an LLM prompt. That is the prompt-injection and third-party-PII surface,
// so the zero value is the most restrictive tier.
type TrustTier int

const (
	// TierExternal — an arbitrary/unknown federated server. Default (zero value)
	// and the correct posture for anything not explicitly trusted.
	TierExternal TrustTier = iota
	// TierPartner — a known, vetted federated server.
	TierPartner
	// TierInternal — first-party content (our own services). Rarely applies to
	// a federated tool result, but present for completeness.
	TierInternal
)

// ScanFinding is one detected pattern in a tool-result block, in a shape safe to
// log: it never carries the raw matched value, only a masked preview and a hash
// (Gatekeeper's contract — "never log actual PII"). It is the federation-local
// projection of scan.Finding.
type ScanFinding struct {
	PatternID   string // "email", "ssn", "aws_access_key", "sql_injection"
	PatternType string // "pii" | "credential" | "injection"
	Severity    string
	Preview     string // masked, e.g. "j***@***.com" — never the raw value
	Frameworks  []string
}

// ScanOutcome is the result of scanning one text block.
type ScanOutcome struct {
	// Content is the block after redaction. In log-only mode (the default), or
	// when there are no findings, it equals the input — scanning observes but
	// does not modify. Redaction is deterministic and content-derived, so the
	// redacted form is byte-stable and cache-safe (it enters the conversation
	// already redacted; see AIQG_CACHE_SAFE_REDUCTION.md — redact at source,
	// once).
	Content  string
	Findings []ScanFinding
	Redacted bool
}

// BlockScanner scans (and optionally redacts) a single tool-result text block.
// It is the port the scanning ResultProcessor depends on; the Gatekeeper-backed
// adapter (internal/scanning) is wired in behind it, keeping pkg/scan out of the
// federation hot path and letting the processor be tested with a fake.
//
// Contract:
//   - Deterministic: identical (content, tier) MUST yield identical output, so a
//     redacted block is byte-stable across turns and prompt caching survives.
//   - Fail-open: an error means "could not scan"; the caller keeps the original
//     content and drops the findings. A scan failure must NEVER drop or corrupt
//     a tool result.
//   - Observe-safe: in log-only mode Content == input; only Findings are set.
type BlockScanner interface {
	Scan(ctx context.Context, content string, tier TrustTier) (ScanOutcome, error)
}

// FindingSink receives the findings from scanning one tool-result block, tagged
// with the server that produced it. It is how the boundary surfaces what it saw
// — a logger, a metric, an event emitter — without the processor knowing which.
// Called once per block that produced at least one finding. Must not block the
// hot path meaningfully.
type FindingSink func(serverID string, findings []ScanFinding, redacted bool)

// scanningResultProcessor scans — and, when enabled, redacts — the text blocks
// of a tools/call result at the federation boundary, BEFORE reduction. It is the
// security control the reduce optimization is not: it runs on every external
// tool result regardless of whether that server opted into reduction (see
// manager wiring), because a tool result is TierExternal content on its way into
// an LLM prompt.
//
// Order is scan → redact → (reduce, downstream). Scanning the ORIGINAL, not the
// reduced text, is deliberate: reduction is lossy, so scanning reduced content
// under-reports — it would miss PII that reduction dropped and lose a finding
// compliance wants recorded even when the content never reached the model.
type scanningResultProcessor struct {
	scanner BlockScanner
	tierFor func(serverID string) TrustTier
	sink    FindingSink
	redact  bool // false = log-only (observe, don't modify): the default first stage
}

// ResultScanner scans a tools/call response at the federation boundary. It is a
// separate seam from ResultProcessor (reduction): scanning is the security
// control and runs on every external tool result, while reduction is a per-server
// opt-in optimization. The manager calls Scan before reduce.
type ResultScanner interface {
	ScanResult(ctx context.Context, serverID string, req *MCPRequest, resp *MCPResponse) *MCPResponse
}

// NewScanningResultProcessor returns a ResultScanner that scans (and, when
// redact is true, redacts) tools/call result text through scanner. tierFor maps
// a server id to its trust tier (nil → every server is TierExternal, the safe
// default). sink receives findings per block (nil → findings are dropped after
// stamping resp.Meta). redact=false is log-only: observe and report, modify
// nothing — the draft→enforce first stage.
//
// A nil scanner yields a processor that passes every response through unchanged.
func NewScanningResultProcessor(scanner BlockScanner, tierFor func(string) TrustTier, sink FindingSink, redact bool) ResultScanner {
	return &scanningResultProcessor{
		scanner: scanner,
		tierFor: tierFor,
		sink:    sink,
		redact:  redact,
	}
}

// ScanResult scans (and optionally redacts) the text blocks of a tools/call
// response in place, emitting findings through the sink. It returns resp
// unchanged for non-tool-call methods, a nil scanner, an unrecognized result
// shape, or on any per-block scan error (fail-open — a scan failure must never
// drop or corrupt a tool result). serverID identifies the federated server that
// produced resp, for the trust tier and finding attribution.
func (p *scanningResultProcessor) ScanResult(ctx context.Context, serverID string, req *MCPRequest, resp *MCPResponse) *MCPResponse {
	if p == nil || p.scanner == nil || resp == nil || resp.Error != nil {
		return resp
	}
	if req == nil || req.Method != methodToolsCall {
		return resp
	}

	tier := TierExternal
	if p.tierFor != nil {
		tier = p.tierFor(serverID)
	}

	findings, anyRedacted := p.scanResultContent(ctx, resp, tier)
	if len(findings) == 0 {
		return resp
	}

	if p.sink != nil {
		p.sink(serverID, findings, anyRedacted)
	}
	if resp.Meta == nil {
		resp.Meta = map[string]interface{}{}
	}
	resp.Meta["scanned"] = true
	resp.Meta["scan_findings"] = len(findings)
	if anyRedacted {
		resp.Meta["scan_redacted"] = true
	}
	return resp
}

// scanResultContent scans the response's content in place — either a bare-string
// result or each text block — and returns the aggregated findings plus whether
// any block was redacted. Redaction writes back through resp.Result / the block
// setter, so the caller's resp is mutated directly.
func (p *scanningResultProcessor) scanResultContent(ctx context.Context, resp *MCPResponse, tier TrustTier) ([]ScanFinding, bool) {
	var findings []ScanFinding
	anyRedacted := false

	// A bare-string result is held by value on resp, so scan it directly.
	if s, ok := resp.Result.(string); ok && s != "" {
		out, ok := p.scanBlock(ctx, s, tier)
		if ok {
			findings = append(findings, out.Findings...)
			if out.Redacted && out.Content != s {
				resp.Result = out.Content
				anyRedacted = true
			}
		}
		return findings, anyRedacted
	}

	for _, b := range textBlocks(resp.Result) {
		orig := b.text()
		if orig == "" {
			continue
		}
		out, ok := p.scanBlock(ctx, orig, tier)
		if !ok {
			continue
		}
		findings = append(findings, out.Findings...)
		if out.Redacted && out.Content != orig {
			b.set(out.Content)
			anyRedacted = true
		}
	}
	return findings, anyRedacted
}

// scanBlock scans one block, honoring log-only mode. It returns ok=false on a
// scan error (fail-open: caller keeps the original block). In log-only mode it
// forces Redacted=false and returns the original content, so findings are still
// surfaced but nothing is modified — the draft→enforce first stage.
func (p *scanningResultProcessor) scanBlock(ctx context.Context, content string, tier TrustTier) (ScanOutcome, bool) {
	out, err := p.scanner.Scan(ctx, content, tier)
	if err != nil {
		return ScanOutcome{}, false
	}
	if !p.redact {
		out.Content = content
		out.Redacted = false
	}
	return out, true
}
