package federation

import "context"

// ResultProcessor transforms an MCP tool-result response at its source — the
// cache-safe "reduce at the source, once" seam (AIQG_CACHE_SAFE_REDUCTION.md
// §2/§9.A).
//
// Why here, and not at the LLM gateway: a tool result is produced exactly once,
// at the federation boundary, BEFORE the agent puts it into its context. If we
// reduce + compliance-scan it here, the agent caches the already-reduced form
// and re-reads that cached, smaller content on every later turn. Nothing
// downstream ever edits cached content, so prompt caching stays intact. The
// gateway's old in-place per-turn reduction did the opposite — it mutated
// already-cached content every turn and busted the cache (retired in Phase 0).
//
// Implementation contract:
//   - Deterministic for a given content, so the reduced form is byte-stable
//     across turns (the agent re-sends it verbatim; a different result each
//     turn would defeat caching).
//   - Scan/reduce ONCE — content-hash attested, so repeated identical content
//     isn't re-processed.
//   - Fail-open: a reduction/scan failure must NEVER drop or corrupt a tool
//     result. On any error, return the original response unchanged.
//   - Only tools/call responses carry reducible external content; every other
//     method passes through untouched.
type ResultProcessor interface {
	// ProcessResult returns the tool-call response with its result reduced +
	// compliance-scanned, or the original response unchanged for non-tool-call
	// methods / on any failure. Never returns nil when given a non-nil resp.
	ProcessResult(ctx context.Context, method string, resp *MCPResponse) *MCPResponse
}

// noopResultProcessor is the default: it passes every response through
// unchanged. Cache-safe reduce-at-source stays OFF until a Gatekeeper-backed
// processor is installed via Manager.SetResultProcessor (Slice 2). Keeping the
// seam always present (never nil) means the hot path has no nil-check branch
// and turning reduction on is a single wiring change, not a code change.
type noopResultProcessor struct{}

// ProcessResult returns resp unchanged.
func (noopResultProcessor) ProcessResult(_ context.Context, _ string, resp *MCPResponse) *MCPResponse {
	return resp
}
