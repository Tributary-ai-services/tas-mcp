package federation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

func TestClassifyOutcome(t *testing.T) {
	status := func(code int) error {
		return fmt.Errorf("srv returned %d: %w: body", code, &upstreamStatusError{code: code})
	}
	cases := []struct {
		name    string
		err     error
		resp    *MCPResponse
		blocked bool
		want    string
	}{
		{"ok", nil, &MCPResponse{Result: map[string]interface{}{"content": []interface{}{}}}, false, OutcomeOK},
		{"ok bare string", nil, &MCPResponse{Result: "hi"}, false, OutcomeOK},
		{"isError result", nil, &MCPResponse{Result: map[string]interface{}{"isError": true}}, false, OutcomeToolError},
		{"error response", nil, &MCPResponse{Error: &MCPError{Code: -32000, Message: "x"}}, false, OutcomeToolError},
		{"blocked wins", status(http.StatusBadGateway), nil, true, OutcomeBlocked},
		{"deadline", fmt.Errorf("call: %w", context.DeadlineExceeded), nil, false, OutcomeTimeout},
		{"canceled", fmt.Errorf("call: %w", context.Canceled), nil, false, OutcomeTimeout},
		{"refused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), nil, false, OutcomeUnreachable},
		{"dns", &net.DNSError{Err: "no such host", Name: "x"}, nil, false, OutcomeUnreachable},
		{"other transport", errors.New("EOF"), nil, false, OutcomeUnreachable},
		{"502", status(http.StatusBadGateway), nil, false, OutcomeUnreachable},
		{"503", status(http.StatusServiceUnavailable), nil, false, OutcomeUnreachable},
		{"504", status(http.StatusGatewayTimeout), nil, false, OutcomeUnreachable},
		{"500", status(http.StatusInternalServerError), nil, false, OutcomeToolError},
		{"404", status(http.StatusNotFound), nil, false, OutcomeToolError},
		{"jsonrpc", fmt.Errorf("srv %w 1: bad", errUpstreamJSONRPC), nil, false, OutcomeToolError},
		{"nil resp no err", nil, nil, false, OutcomeUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyOutcome(c.err, c.resp, c.blocked); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// The status error keeps classifyError and the log text unchanged.
func TestUpstreamStatusError_Compat(t *testing.T) {
	err := fmt.Errorf("s returned 503: %w: b", &upstreamStatusError{code: 503})
	if !errors.Is(err, errUpstreamStatus) || classifyError(err) != ErrKindHTTPStatus {
		t.Fatal("upstreamStatusError must match errUpstreamStatus")
	}
	if err.Error() != "s returned 503: "+errUpstreamStatus.Error()+": b" {
		t.Fatalf("message changed: %q", err.Error())
	}
}

type captureObserver struct{ recs []ToolCallRecord }

func (c *captureObserver) ObserveToolCall(r ToolCallRecord) { c.recs = append(c.recs, r) } //nolint:gocritic

type panicObserver struct{}

func (panicObserver) ObserveToolCall(ToolCallRecord) { panic("boom") } //nolint:gocritic

func registerAt(t *testing.T, m *Manager, id, url string) {
	t.Helper()
	s := createTestServer()
	s.ID = id
	s.Endpoint = url
	s.HealthCheck.Enabled = false
	if err := m.registry.Put(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func callTool(m *Manager, ctx context.Context, server, id string) (*MCPResponse, error) { //nolint:revive
	return m.InvokeServer(ctx, server, &MCPRequest{
		ID: id, Method: methodToolsCall,
		Params: map[string]interface{}{"name": "echo", "arguments": map[string]interface{}{}},
	})
}

func TestInvokeServer_ObservesToolCall(t *testing.T) {
	ts := newTestMCPServer(t)
	defer ts.Close()
	m := createTestManager()
	registerAt(t, m, "napkin", ts.URL)
	obs := &captureObserver{}
	m.SetToolCallObserver(obs)
	// The real scanning processor with a fake block scanner: the record's
	// findings must come from THIS scan.
	bs := &fakeBlockScanner{needle: "echo"}
	m.SetScanner(NewScanningResultProcessor(bs, nil, nil, false))

	id := &identity.Identity{Attributed: true, TenantID: "ten", ToolCallID: "toolu_1"}
	ctx := identity.WithIdentity(context.Background(), id)
	resp, err := callTool(m, ctx, "napkin", "rpc-1")
	if err != nil || resp.Error != nil {
		t.Fatalf("call: %v %+v", err, resp)
	}
	if len(obs.recs) != 1 {
		t.Fatalf("records = %d", len(obs.recs))
	}
	r := obs.recs[0]
	if r.ServerID != "napkin" || r.ToolName != "echo" || r.Outcome != OutcomeOK || r.ToolCallID != "toolu_1" {
		t.Fatalf("record %+v", r)
	}
	if r.ScanSkipped || r.Findings["test"] != 1 || bs.calls != 1 {
		t.Fatalf("findings=%v scan_skipped=%v scans=%d", r.Findings, r.ScanSkipped, bs.calls)
	}
	if r.ResultBytes == 0 || r.Identity != id || r.OccurredAt.IsZero() {
		t.Fatalf("record %+v", r)
	}
	if !strings.Contains(histogramDump(t), `server_id="napkin",tool="echo",outcome="ok"`) &&
		!strings.Contains(histogramDump(t), `outcome="ok",server_id="napkin",tool="echo"`) {
		t.Fatal("tas_mcp_tool_call_seconds{napkin,echo,ok} not recorded")
	}
}

func TestInvokeServer_ToolCallIDPrecedence(t *testing.T) {
	ts := newTestMCPServer(t)
	defer ts.Close()
	m := createTestManager()
	registerAt(t, m, "s", ts.URL)
	obs := &captureObserver{}
	m.SetToolCallObserver(obs)

	_, _ = callTool(m, context.Background(), "s", "rpc-7")
	_, _ = callTool(m, WithGeneratedRequestID(context.Background()), "s", "req_123")
	if obs.recs[0].ToolCallID != "rpc-7" {
		t.Fatalf("client id not used: %q", obs.recs[0].ToolCallID)
	}
	if got := obs.recs[1].ToolCallID; got == "req_123" || len(got) != 36 {
		t.Fatalf("generated id should be replaced by a UUID, got %q", got)
	}
}

func TestInvokeServer_OutcomesFromRealUpstreams(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer slow.Close()
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unavailable.Close()
	toolErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"nope"}],"isError":true}`))
	}))
	defer toolErr.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	m := createTestManager()
	registerAt(t, m, "slow", slow.URL)
	registerAt(t, m, "unavail", unavailable.URL)
	registerAt(t, m, "toolerr", toolErr.URL)
	registerAt(t, m, "gone", goneURL)
	obs := &captureObserver{}
	m.SetToolCallObserver(obs)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = callTool(m, ctx, "slow", "1")
	_, _ = callTool(m, context.Background(), "unavail", "2")
	_, _ = callTool(m, context.Background(), "toolerr", "3")
	_, _ = callTool(m, context.Background(), "gone", "4")

	want := []string{OutcomeTimeout, OutcomeUnreachable, OutcomeToolError, OutcomeUnreachable}
	if len(obs.recs) != len(want) {
		t.Fatalf("records = %d", len(obs.recs))
	}
	for i, w := range want {
		if obs.recs[i].Outcome != w {
			t.Errorf("%s: outcome %q, want %q", obs.recs[i].ServerID, obs.recs[i].Outcome, w)
		}
		if w != OutcomeToolError && !obs.recs[i].ScanSkipped {
			t.Errorf("%s: no scanner installed, scan_skipped must be true", obs.recs[i].ServerID)
		}
	}
}

// An observer must never affect the call, even by panicking.
func TestInvokeServer_ObserverPanicDoesNotAffectCall(t *testing.T) {
	ts := newTestMCPServer(t)
	defer ts.Close()
	m := createTestManager()
	registerAt(t, m, "s", ts.URL)
	m.SetToolCallObserver(panicObserver{})
	resp, err := callTool(m, context.Background(), "s", "1")
	if err != nil || resp == nil || resp.Error != nil {
		t.Fatalf("call affected by observer: %v %+v", err, resp)
	}
}

// Non-tools/call methods are not tool calls.
func TestInvokeServer_ToolsListNotObserved(t *testing.T) {
	ts := newTestMCPServer(t)
	defer ts.Close()
	m := createTestManager()
	registerAt(t, m, "s", ts.URL)
	obs := &captureObserver{}
	m.SetToolCallObserver(obs)
	_, _ = m.InvokeServer(context.Background(), "s", &MCPRequest{ID: "1", Method: methodToolsList})
	if len(obs.recs) != 0 {
		t.Fatalf("tools/list observed: %+v", obs.recs)
	}
}

func TestToolLabelLimiter(t *testing.T) {
	l := &toolLabelLimiter{seen: map[string]map[string]struct{}{}}
	for i := 0; i < maxToolLabelsPerServer; i++ {
		if got := l.label("s", fmt.Sprintf("t%d", i)); got != fmt.Sprintf("t%d", i) {
			t.Fatalf("label %d = %q", i, got)
		}
	}
	if got := l.label("s", "one-too-many"); got != otherToolLabel {
		t.Fatalf("over cap = %q", got)
	}
	if got := l.label("s", "t0"); got != "t0" {
		t.Fatalf("known name folded: %q", got)
	}
	if got := l.label("other-server", "x"); got != "x" {
		t.Fatalf("cap is per server: %q", got)
	}
	if got := l.label("s", ""); got != otherToolLabel {
		t.Fatalf("empty = %q", got)
	}
}

func TestReducedBytesFromMeta(t *testing.T) {
	if reducedBytes(&MCPResponse{Meta: map[string]interface{}{"reduced_bytes_saved": 120}}) != 120 {
		t.Fatal("int")
	}
	if reducedBytes(&MCPResponse{}) != 0 || reducedBytes(nil) != 0 {
		t.Fatal("absent")
	}
}

// histogramDump renders ToolCallSeconds in text exposition format.
func histogramDump(t *testing.T) string {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(ToolCallSeconds)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, mf := range mfs {
		_, _ = expfmt.MetricFamilyToText(&b, mf)
	}
	return b.String()
}
