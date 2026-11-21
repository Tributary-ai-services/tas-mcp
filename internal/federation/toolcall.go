package federation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

// Tool-call outcomes (Plan #22 design §9.5). A closed set: it is a Prometheus
// label and a column the Timescale table and Spark job read. `no_result` is in
// the design's set too, but only the gateway can observe it, so the proxy
// never emits it.
const (
	OutcomeOK          = "ok"
	OutcomeToolError   = "tool_error"
	OutcomeTimeout     = "timeout"
	OutcomeUnreachable = "unreachable"
	OutcomeBlocked     = "blocked"
)

// ToolCallRecord is everything the proxy observed about one tools/call. It is
// handed to the ToolCallObserver after the response is final (scanned and
// reduced), so the observer never needs the response itself.
type ToolCallRecord struct {
	ServerID   string
	ToolName   string
	ToolCallID string
	Outcome    string
	// Duration is the upstream round trip only: service.Invoke, excluding
	// registry lookup, scanning and reduction. It is "how long the tool took".
	Duration time.Duration
	// ResultBytes is the size of the JSON-encoded result the caller receives,
	// i.e. after redaction and reduction. Zero for an error response.
	ResultBytes int
	// ReducedBytes is how many bytes reduce-at-source removed (0 if it did not
	// run). The pre-reduction size is ResultBytes + ReducedBytes.
	ReducedBytes int
	// Findings is the G2 boundary scan's finding count per Gatekeeper pattern
	// id, e.g. "pii-email": 2. The ids are already categorized — the category
	// is the prefix before the first "-" (pii, cred, injection, aiqg), the
	// same ids the gateway writes to event_metrics.tags — and are passed
	// through verbatim. Taken from the one scan the boundary already does;
	// never the matched values. Empty, never nil.
	Findings map[string]int
	// ScanSkipped is true when the G2 scanner did not scan this result:
	// scanning disabled, or an error response with nothing to scan.
	ScanSkipped bool
	OccurredAt  time.Time
	// Identity is the caller (T.1). Nil when identity resolution is off.
	Identity *identity.Identity
}

// ToolCallObserver receives one record per tools/call. Implementations MUST
// NOT block: ObserveToolCall runs on the request path.
type ToolCallObserver interface {
	ObserveToolCall(rec ToolCallRecord)
}

// SetToolCallObserver installs the tool-call observer (the event publisher).
// Nil turns it off. The histogram is recorded either way.
func (m *Manager) SetToolCallObserver(o ToolCallObserver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCallObserver = o
}

// callObservation collects, during one InvokeServer, what deeper layers saw:
// the typed upstream error that GenericService folds into resp.Error, and the
// scanner's findings. It rides in the context so neither layer's signature
// changes and nothing extra is written into the client-visible response.
type callObservation struct {
	mu          sync.Mutex
	upstreamErr error
	scanned     bool
	findings    map[string]int
	blocked     bool
}

type callObservationKey struct{}

func withCallObservation(ctx context.Context) (context.Context, *callObservation) {
	obs := &callObservation{findings: map[string]int{}}
	return context.WithValue(ctx, callObservationKey{}, obs), obs
}

func observationFrom(ctx context.Context) *callObservation {
	obs, _ := ctx.Value(callObservationKey{}).(*callObservation)
	return obs
}

func (o *callObservation) setUpstreamErr(err error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.upstreamErr = err
	o.mu.Unlock()
}

func (o *callObservation) recordScan(findings []ScanFinding) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.scanned = true
	for _, f := range findings {
		if f.PatternID != "" {
			o.findings[f.PatternID]++
		}
	}
}

// generatedIDKey marks a request whose ID the HTTP handler generated, so the
// tool_call_id falls back to a UUID rather than the synthetic req_<nanos>.
type generatedIDKey struct{}

// WithGeneratedRequestID marks ctx: the request ID was minted by the proxy,
// not sent by the client.
func WithGeneratedRequestID(ctx context.Context) context.Context {
	return context.WithValue(ctx, generatedIDKey{}, true)
}

func requestIDGenerated(ctx context.Context) bool {
	v, _ := ctx.Value(generatedIDKey{}).(bool)
	return v
}

// upstreamStatusError carries the HTTP status of a non-2xx upstream answer.
// It matches errUpstreamStatus under errors.Is, so classifyError is unchanged,
// and its text is errUpstreamStatus's, so log lines are unchanged.
type upstreamStatusError struct{ code int }

func (e *upstreamStatusError) Error() string        { return errUpstreamStatus.Error() }
func (e *upstreamStatusError) Is(target error) bool { return target == errUpstreamStatus }

// classifyOutcome maps what the proxy saw to an outcome.
//
//   - blocked: a policy refused the call. Nothing sets this yet (G2 is
//     log-only and never blocks); the branch exists so the enum is complete.
//   - timeout: deadline exceeded, or the caller canceled. A canceled call
//     was not answered within the time the caller would wait, which is what
//     timeout means to Uptime and Speed.
//   - unreachable: the server could not be reached (refused, DNS, other
//     transport) or the edge answered 502/503/504 for it — a server scaled to
//     zero answers through its ingress or Service exactly this way.
//   - tool_error: the server answered and said no — another non-2xx, a
//     JSON-RPC error, an error response, or an MCP result with isError=true.
//   - ok: everything else.
func classifyOutcome(upstreamErr error, resp *MCPResponse, blocked bool) string {
	if blocked {
		return OutcomeBlocked
	}
	if upstreamErr != nil {
		switch classifyError(upstreamErr) {
		case ErrKindTimeout, ErrKindCanceled:
			return OutcomeTimeout
		case ErrKindJSONRPC:
			return OutcomeToolError
		case ErrKindHTTPStatus:
			var se *upstreamStatusError
			if errors.As(upstreamErr, &se) {
				switch se.code {
				case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
					return OutcomeUnreachable
				}
			}
			return OutcomeToolError
		default:
			return OutcomeUnreachable
		}
	}
	if resp == nil {
		return OutcomeUnreachable
	}
	if resp.Error != nil {
		return OutcomeToolError
	}
	if m, ok := resp.Result.(map[string]interface{}); ok {
		if isErr, _ := m["isError"].(bool); isErr {
			return OutcomeToolError
		}
	}
	return OutcomeOK
}

// resultBytes is the JSON size of the result the caller receives.
func resultBytes(resp *MCPResponse) int {
	if resp == nil || resp.Error != nil || resp.Result == nil {
		return 0
	}
	if s, ok := resp.Result.(string); ok {
		return len(s)
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return 0
	}
	return len(b)
}

// reducedBytes reads the saving reduce-at-source stamped on resp.Meta.
func reducedBytes(resp *MCPResponse) int {
	if resp == nil || resp.Meta == nil {
		return 0
	}
	switch v := resp.Meta["reduced_bytes_saved"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// ToolCallSeconds is the tool-call latency histogram (design §9.5 "Speed").
// Duration is the upstream round trip; see ToolCallRecord.Duration.
var ToolCallSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "tas_mcp",
	Name:      "tool_call_seconds",
	Help:      "Upstream duration of federated MCP tools/call requests, by server, tool and outcome.",
	Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
}, []string{"server_id", "tool", "outcome"})

// Tool-name label bounding. A tool name is client-supplied, so it cannot be a
// label unbounded: past maxToolLabelsPerServer distinct names per server (the
// design's catalog fold, §9.5 "Rollup") and for over-long names the label
// is "(other)". Events still carry the real name.
const (
	maxToolLabelsPerServer = 200
	maxToolLabelLen        = 128
	otherToolLabel         = "(other)"
)

type toolLabelLimiter struct {
	mu   sync.Mutex
	seen map[string]map[string]struct{}
}

var toolLabels = &toolLabelLimiter{seen: map[string]map[string]struct{}{}}

func (l *toolLabelLimiter) label(serverID, tool string) string {
	if tool == "" || len(tool) > maxToolLabelLen {
		return otherToolLabel
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.seen[serverID]
	if set == nil {
		set = map[string]struct{}{}
		l.seen[serverID] = set
	}
	if _, ok := set[tool]; ok {
		return tool
	}
	if len(set) >= maxToolLabelsPerServer {
		return otherToolLabel
	}
	set[tool] = struct{}{}
	return tool
}
