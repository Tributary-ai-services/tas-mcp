package federation

import (
	"context"
	"sort"
	"strings"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
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
	PatternID   string // Gatekeeper matcher id, already categorized: "pii-email", "pii-ssn", "cred-aws-access-key", "injection-sql"
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

	// attestor records "this content was scanned" in the shared scan-once
	// store (Plan #22 T.3 / GK-5b), so the gateway does not scan the same
	// tool result again. Nil = no attestations.
	attestor ScanAttestor
}

// ScanAttestation is what the boundary scan learned about one tool result,
// in the form the gateway will meet it: the result's text blocks joined by
// "\n" (the gateway's extractMessagesText join), i.e. what an agent puts in
// the tool message.
type ScanAttestation struct {
	TenantID string
	Tier     TrustTier
	// Content is the result text as scanned (before any redaction).
	Content string
	// Patterns are the pattern ids found in Content, sorted, unique; empty
	// means clean.
	Patterns []string
	// Redacted is true when the caller receives Output instead of Content.
	Redacted bool
	// Output is the delivered text after redaction (set only when Redacted).
	Output string
}

// ScanAttestor records scan attestations. It MUST NOT block: Attest runs on
// the tool-call path. Any failure must leave the gateway to scan normally.
type ScanAttestor interface {
	Attest(ctx context.Context, a ScanAttestation)
}

// ScanOption configures a scanning processor.
type ScanOption func(*scanningResultProcessor)

// WithAttestor makes the processor attest each fully scanned tool result.
func WithAttestor(a ScanAttestor) ScanOption {
	return func(p *scanningResultProcessor) { p.attestor = a }
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
func NewScanningResultProcessor(scanner BlockScanner, tierFor func(string) TrustTier, sink FindingSink, redact bool,
	opts ...ScanOption) ResultScanner {
	p := &scanningResultProcessor{
		scanner: scanner,
		tierFor: tierFor,
		sink:    sink,
		redact:  redact,
	}
	for _, o := range opts {
		o(p)
	}
	return p
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

	sr := p.scanResultContent(ctx, resp, tier)
	findings, anyRedacted := sr.findings, sr.redacted
	// Hand the counts to the tool-call event (T.2): the event reuses this
	// scan's result, it never scans again.
	observationFrom(ctx).recordScan(findings)
	p.attest(ctx, tier, &sr)
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

// scanResult is what scanning one response produced.
type scanResult struct {
	findings []ScanFinding
	redacted bool
	// complete is true when every text block scanned without error, so the
	// whole result is known; only then may it be attested.
	complete bool
	// in and out are the scanned and the delivered text blocks, in order
	// (non-empty blocks only, as the gateway reads them).
	in, out []string
}

// scanResultContent scans the response's content in place — either a bare-string
// result or each text block — and returns the aggregated findings plus whether
// any block was redacted. Redaction writes back through resp.Result / the block
// setter, so the caller's resp is mutated directly.
func (p *scanningResultProcessor) scanResultContent(ctx context.Context, resp *MCPResponse, tier TrustTier) scanResult {
	r := scanResult{complete: true}

	// A bare-string result is held by value on resp, so scan it directly.
	if s, ok := resp.Result.(string); ok && s != "" {
		out, ok := p.scanBlock(ctx, s, tier)
		if !ok {
			r.complete = false
			return r
		}
		r.findings = append(r.findings, out.Findings...)
		delivered := s
		if out.Redacted && out.Content != s {
			resp.Result = out.Content
			delivered = out.Content
			r.redacted = true
		}
		r.in, r.out = []string{s}, []string{delivered}
		return r
	}

	for _, b := range textBlocks(resp.Result) {
		orig := b.text()
		if orig == "" {
			continue
		}
		out, ok := p.scanBlock(ctx, orig, tier)
		if !ok {
			r.complete = false
			continue
		}
		r.findings = append(r.findings, out.Findings...)
		delivered := orig
		if out.Redacted && out.Content != orig {
			b.set(out.Content)
			delivered = out.Content
			r.redacted = true
		}
		r.in = append(r.in, orig)
		r.out = append(r.out, delivered)
	}
	return r
}

// attest hands a fully scanned, attributed result to the attestor. A partly
// scanned result (any block errored) or one without a tenant is never
// attested: the gateway keys by tenant, and an attestation must cover all of
// the content it names.
func (p *scanningResultProcessor) attest(ctx context.Context, tier TrustTier, r *scanResult) {
	if p.attestor == nil || !r.complete || len(r.in) == 0 {
		return
	}
	id := identity.FromContext(ctx)
	if id == nil || !id.Attributed || id.TenantID == "" {
		return
	}
	a := ScanAttestation{
		TenantID: id.TenantID,
		Tier:     tier,
		Content:  strings.Join(r.in, "\n"),
		Patterns: patternSet(r.findings),
		Redacted: r.redacted,
	}
	if r.redacted {
		a.Output = strings.Join(r.out, "\n")
	}
	p.attestor.Attest(ctx, a)
}

func patternSet(fs []ScanFinding) []string {
	if len(fs) == 0 {
		return nil
	}
	set := map[string]bool{}
	for i := range fs {
		set[fs[i].PatternID] = true
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
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
