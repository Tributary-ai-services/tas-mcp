package federation

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
)

const probeToolsJSON = `{"tools":[
 {"name":"generate_visual","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}},
 {"name":"list_styles"}]}`

// bridgeServer mimics the TAS bridge: GET /mcp/tools/list only.
func bridgeServer(t *testing.T, body string, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		if r.URL.Path != mcpToolsListPath || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// streamableServer mimics a Streamable-HTTP MCP server and records methods.
func streamableServer(t *testing.T, failInit bool) (*httptest.Server, *[]string, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		methods = append(methods, req.Method)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case req.Method == ProbeStageInitialize && failInit:
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32603,\"message\":\"boom\"}}\n\n")
		case req.Method == ProbeStageInitialize:
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n\n")
		case req.Method == "tools/list":
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":"+
				strings.ReplaceAll(probeToolsJSON, "\n", "")+"}\n\n")
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &methods, &mu
}

func closedEndpoint(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return "http://" + addr
}

func newProbeManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(zap.NewNop(), NewDiscovery(zap.NewNop()), NewBridge(zap.NewNop()))
}

func register(t *testing.T, m *Manager, id, endpoint string, proto Protocol) {
	t.Helper()
	if err := m.RegisterServer(&MCPServer{ID: id, Name: id, Endpoint: endpoint, Protocol: proto, Auth: AuthConfig{Type: AuthNone}}); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

type catalogueRecorder struct {
	mu   sync.Mutex
	cats []ToolCatalogue
}

func (c *catalogueRecorder) ObserveCatalogue(cat ToolCatalogue) { //nolint:gocritic // interface signature
	c.mu.Lock()
	c.cats = append(c.cats, cat)
	c.mu.Unlock()
}

func TestProber_HTTPServerUpWithCatalogue(t *testing.T) {
	m := newProbeManager(t)
	register(t, m, "probe-http-up", bridgeServer(t, probeToolsJSON, nil).URL, ProtocolHTTP)
	p := NewProber(m, ProbeConfig{Timeout: 2 * time.Second}, nil)
	rec := &catalogueRecorder{}
	p.SetCatalogueObserver(rec)
	m.SetProber(p)

	p.RunOnce(context.Background())

	r, ok := m.ProbeStatus("probe-http-up")
	if !ok || !r.Up || r.ToolCount != 2 || r.Stage != "" || r.Error != "" || r.LastUpAt == nil {
		t.Fatalf("probe = %+v, ok=%v; want up with 2 tools", r, ok)
	}
	if r.IntervalSeconds != 60 {
		t.Errorf("interval_seconds = %d, want 60", r.IntervalSeconds)
	}
	if got := testutil.ToFloat64(ServerUp.WithLabelValues("probe-http-up")); got != 1 {
		t.Errorf("tas_mcp_server_up = %v, want 1", got)
	}
	if len(rec.cats) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(rec.cats))
	}
	cat := rec.cats[0]
	// Sorted by name; schema_bytes is the compact JSON size of inputSchema.
	wantSchema := len(`{"properties":{"text":{"type":"string"}},"type":"object"}`)
	if cat.Tools[0].Name != "generate_visual" || cat.Tools[0].SchemaBytes != wantSchema ||
		cat.Tools[1].Name != "list_styles" || cat.Tools[1].SchemaBytes != 0 {
		t.Errorf("tools = %+v, want generate_visual(%d), list_styles(0)", cat.Tools, wantSchema)
	}
	if cat.Hash == "" || cat.Hash != r.ToolsetHash {
		t.Errorf("hash %q vs probe %q", cat.Hash, r.ToolsetHash)
	}
}

func TestProber_StreamableInitializeThenToolsList(t *testing.T) {
	srv, methods, mu := streamableServer(t, false)
	m := newProbeManager(t)
	register(t, m, "probe-sse", srv.URL, ProtocolSSE)
	p := NewProber(m, ProbeConfig{Timeout: 2 * time.Second}, nil)
	m.SetProber(p)
	p.RunOnce(context.Background())

	mu.Lock()
	got := strings.Join(*methods, ",")
	mu.Unlock()
	if got != "initialize,tools/list" {
		t.Errorf("methods = %q, want initialize,tools/list", got)
	}
	if r, _ := m.ProbeStatus("probe-sse"); !r.Up || r.ToolCount != 2 {
		t.Errorf("probe = %+v, want up", r)
	}
}

func TestProber_StreamableInitializeFailure(t *testing.T) {
	srv, methods, mu := streamableServer(t, true)
	m := newProbeManager(t)
	register(t, m, "probe-sse-bad", srv.URL, ProtocolSSE)
	p := NewProber(m, ProbeConfig{Timeout: 2 * time.Second}, nil)
	m.SetProber(p)
	p.RunOnce(context.Background())

	r, _ := m.ProbeStatus("probe-sse-bad")
	if r.Up || r.Stage != ProbeStageInitialize || !strings.Contains(r.Error, "boom") {
		t.Errorf("probe = %+v, want down at initialize", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*methods) != 1 {
		t.Errorf("tools/list sent after a failed initialize: %v", *methods)
	}
}

func TestProber_DownServers(t *testing.T) {
	m := newProbeManager(t)
	register(t, m, "probe-refused", closedEndpoint(t), ProtocolHTTP)
	register(t, m, "probe-no-tools", bridgeServer(t, `{"status":"ok"}`, nil).URL, ProtocolHTTP)
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	register(t, m, "probe-slow", slow.URL, ProtocolHTTP)

	p := NewProber(m, ProbeConfig{Timeout: 200 * time.Millisecond}, nil)
	rec := &catalogueRecorder{}
	p.SetCatalogueObserver(rec)
	m.SetProber(p)
	start := time.Now()
	p.RunOnce(context.Background())
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("round took %v; the probe timeout did not bound it", el)
	}

	for _, id := range []string{"probe-refused", "probe-no-tools", "probe-slow"} {
		r, ok := m.ProbeStatus(id)
		if !ok || r.Up || r.Stage != ProbeStageToolsList || r.Error == "" || r.ConsecutiveFailures != 1 {
			t.Errorf("%s: probe = %+v, want down at tools/list", id, r)
		}
		if got := testutil.ToFloat64(ServerUp.WithLabelValues(id)); got != 0 {
			t.Errorf("%s: tas_mcp_server_up = %v, want 0", id, got)
		}
	}
	if len(rec.cats) != 0 {
		t.Errorf("a failed probe published a catalogue: %+v", rec.cats)
	}

	p.RunOnce(context.Background())
	if r, _ := m.ProbeStatus("probe-refused"); r.ConsecutiveFailures != 2 {
		t.Errorf("consecutive failures = %d, want 2", r.ConsecutiveFailures)
	}
}

func TestProber_RecoveryKeepsLastUp(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, probeToolsJSON)
	}))
	t.Cleanup(srv.Close)
	m := newProbeManager(t)
	register(t, m, "probe-flap", srv.URL, ProtocolHTTP)
	p := NewProber(m, ProbeConfig{Timeout: time.Second}, nil)
	m.SetProber(p)

	p.RunOnce(context.Background())
	fail.Store(true)
	p.RunOnce(context.Background())
	r, _ := m.ProbeStatus("probe-flap")
	if r.Up || r.LastUpAt == nil || r.ToolCount != 2 {
		t.Fatalf("after failure: %+v; want down, last_up_at and tool_count kept", r)
	}
	fail.Store(false)
	p.RunOnce(context.Background())
	if r, _ := m.ProbeStatus("probe-flap"); !r.Up || r.ConsecutiveFailures != 0 {
		t.Errorf("after recovery: %+v", r)
	}
}

func TestProber_BoundedConcurrencyAndSkipsInflight(t *testing.T) {
	var cur, peak int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&cur, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&cur, -1)
		_, _ = io.WriteString(w, probeToolsJSON)
	}))
	t.Cleanup(srv.Close)
	m := newProbeManager(t)
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5", "c6"} {
		register(t, m, "probe-conc-"+id, srv.URL, ProtocolHTTP)
	}
	p := NewProber(m, ProbeConfig{Timeout: 5 * time.Second, Concurrency: 2}, nil)

	done := make(chan struct{})
	go func() { p.RunOnce(context.Background()); close(done) }()
	time.Sleep(200 * time.Millisecond)
	close(release)
	<-done
	if peak > 2 {
		t.Errorf("peak concurrent probes = %d, want <= 2", peak)
	}
	if got := len(p.ProbeStatuses()); got != 6 {
		t.Errorf("probed %d servers, want 6", got)
	}
}

func TestProber_InflightServerIsSkipped(t *testing.T) {
	m := newProbeManager(t)
	var hits int32
	register(t, m, "probe-skip", bridgeServer(t, probeToolsJSON, &hits).URL, ProtocolHTTP)
	p := NewProber(m, ProbeConfig{}, nil)
	if !p.claim("probe-skip") {
		t.Fatal("claim failed")
	}
	p.RunOnce(context.Background())
	if hits != 0 {
		t.Errorf("an in-flight server was probed again (%d hits)", hits)
	}
	p.release("probe-skip")
	p.RunOnce(context.Background())
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
}

func TestProber_PrunesUnregistered(t *testing.T) {
	m := newProbeManager(t)
	register(t, m, "probe-gone", bridgeServer(t, probeToolsJSON, nil).URL, ProtocolHTTP)
	p := NewProber(m, ProbeConfig{}, nil)
	m.SetProber(p)
	p.RunOnce(context.Background())
	if _, ok := m.ProbeStatus("probe-gone"); !ok {
		t.Fatal("not probed")
	}
	if err := m.UnregisterServer("probe-gone"); err != nil {
		t.Fatal(err)
	}
	p.RunOnce(context.Background())
	if _, ok := m.ProbeStatus("probe-gone"); ok {
		t.Error("result kept for an unregistered server")
	}
	if ServerUp.DeleteLabelValues("probe-gone") {
		t.Error("gauge series for an unregistered server not deleted")
	}
}

// A probe never counts as upstream traffic: the MCP-16 alerts read those
// counters as client calls.
func TestProber_DoesNotTouchUpstreamCounters(t *testing.T) {
	m := newProbeManager(t)
	register(t, m, "probe-quiet", closedEndpoint(t), ProtocolHTTP)
	p := NewProber(m, ProbeConfig{Timeout: time.Second}, nil)
	p.RunOnce(context.Background())
	if n := testutil.ToFloat64(UpstreamRequests.WithLabelValues("probe-quiet", "error")); n != 0 {
		t.Errorf("upstream_requests_total{error} = %v, want 0", n)
	}
	if mm := m.GetMetrics(); mm.RequestsTotal != 0 {
		t.Errorf("requests_total = %d, want 0", mm.RequestsTotal)
	}
}

func TestProber_RunStopsOnCancel(t *testing.T) {
	m := newProbeManager(t)
	var hits int32
	register(t, m, "probe-run", bridgeServer(t, probeToolsJSON, &hits).URL, ProtocolHTTP)
	p := NewProber(m, ProbeConfig{Interval: 20 * time.Millisecond}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if atomic.LoadInt32(&hits) < 2 {
		t.Errorf("hits = %d; want the interval to repeat probes", hits)
	}
}

func TestProbeHTTPHandlers(t *testing.T) {
	m := newProbeManager(t)
	register(t, m, "probe-api", bridgeServer(t, probeToolsJSON, nil).URL, ProtocolHTTP)
	register(t, m, "probe-api-unprobed", "http://127.0.0.1:1", ProtocolHTTP)
	p := NewProber(m, ProbeConfig{}, nil)
	m.SetProber(p)
	// Probe only the first server.
	p.claim("probe-api-unprobed")
	p.RunOnce(context.Background())

	r := mux.NewRouter()
	NewHTTPHandlers(m, zap.NewNop()).RegisterRoutes(r)
	get := func(path string) (int, map[string]interface{}) {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		var body map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		return rr.Code, body
	}

	code, body := get("/api/v1/federation/servers/probe-api")
	if code != http.StatusOK || body["id"] != "probe-api" {
		t.Fatalf("GET server: %d %v", code, body)
	}
	probe, ok := body["probe"].(map[string]interface{})
	if !ok || probe["up"] != true || probe["tool_count"] != float64(2) {
		t.Errorf("probe block = %v", body["probe"])
	}

	if _, body := get("/api/v1/federation/servers/probe-api-unprobed"); body["probe"] != nil {
		t.Errorf("unprobed server has a probe block: %v", body["probe"])
	}
	if code, _ := get("/api/v1/federation/servers/probe-api-unprobed/probe"); code != http.StatusNotFound {
		t.Errorf("GET probe for unprobed = %d, want 404", code)
	}
	if code, body := get("/api/v1/federation/servers/probe-api/probe"); code != http.StatusOK || body["up"] != true {
		t.Errorf("GET probe = %d %v", code, body)
	}
	_, body = get("/api/v1/federation/health")
	probes, _ := body["probes"].(map[string]interface{})
	if _, ok := probes["probe-api"]; !ok {
		t.Errorf("health has no probes map entry: %v", body)
	}
	_, body = get("/api/v1/federation/servers")
	servers, _ := body["servers"].([]interface{})
	if len(servers) != 2 {
		t.Fatalf("list = %v", body)
	}
}

func TestParseToolsList(t *testing.T) {
	if _, err := parseToolsList("text"); err == nil {
		t.Error("non-object accepted")
	}
	if _, err := parseToolsList(map[string]interface{}{}); err == nil {
		t.Error("missing tools array accepted")
	}
	tools, err := parseToolsList(map[string]interface{}{"tools": []interface{}{}})
	if err != nil || len(tools) != 0 {
		t.Errorf("empty toolset: %v %v", tools, err)
	}
	a, _ := parseToolsList(map[string]interface{}{"tools": []interface{}{
		map[string]interface{}{"name": "b"}, map[string]interface{}{"name": "a"}, map[string]interface{}{"x": 1}}})
	b, _ := parseToolsList(map[string]interface{}{"tools": []interface{}{
		map[string]interface{}{"name": "a"}, map[string]interface{}{"name": "b"}}})
	if toolsetHash(a) != toolsetHash(b) {
		t.Error("toolset hash depends on order")
	}
}
