package federation

import (
	"context"
	"testing"
)

// fakeResultScanner records that it ran and for which server, so tests can
// assert the InvokeServer scan hook fires independently of the reduce gate.
type fakeResultScanner struct {
	called  bool
	servers []string
}

func (f *fakeResultScanner) ScanResult(_ context.Context, serverID string, _ *MCPRequest, resp *MCPResponse) *MCPResponse {
	f.called = true
	f.servers = append(f.servers, serverID)
	return resp
}

// The load-bearing G2 guarantee: scanning is the boundary SECURITY control, so
// it runs on every external tools/call REGARDLESS of the per-server reduce
// opt-in. A server with Reduce=false — where reduction never runs — must still
// be scanned. (Contrast TestInvokeServer_ReduceGatedPerServer: the reduce
// processor is gated; the scanner is not.)
func TestInvokeServer_ScanRunsIndependentOfReduce(t *testing.T) {
	ts := newTestMCPServer(t)
	defer ts.Close()

	manager := createTestManager()
	sc := &fakeResultScanner{}
	fp := &fakeProcessor{}
	manager.SetScanner(sc)
	manager.SetResultProcessor(fp)
	ctx := context.Background()

	call := func(id string, reduce bool) {
		s := createTestServer()
		s.ID = id
		s.Endpoint = ts.URL
		s.Reduce = reduce
		if err := manager.registry.Put(ctx, s); err != nil {
			t.Fatalf("Put: %v", err)
		}
		_, err := manager.InvokeServer(ctx, id, &MCPRequest{
			ID: "1", Method: methodToolsCall,
			Params: map[string]interface{}{"name": "echo", "arguments": map[string]interface{}{}},
		})
		if err != nil {
			t.Fatalf("invoke %s: %v", id, err)
		}
	}

	// Reduce OFF: scanner MUST still run; reduce processor MUST NOT.
	sc.called, fp.called = false, false
	call("no-reduce", false)
	if !sc.called {
		t.Error("scanner must run for a Reduce=false server — scanning is the boundary control, not gated on reduce")
	}
	if fp.called {
		t.Error("reduce processor must NOT run for a Reduce=false server")
	}

	// Reduce ON: both run.
	sc.called, fp.called = false, false
	call("do-reduce", true)
	if !sc.called {
		t.Error("scanner must run for a Reduce=true server")
	}
	if !fp.called {
		t.Error("reduce processor must run for a Reduce=true server")
	}
}

// Scanning only applies to tools/call — other methods are not boundary content.
func TestInvokeServer_ScanOnlyToolsCall(t *testing.T) {
	ts := newTestMCPServer(t)
	defer ts.Close()

	manager := createTestManager()
	sc := &fakeResultScanner{}
	manager.SetScanner(sc)
	ctx := context.Background()

	s := createTestServer()
	s.ID = "srv"
	s.Endpoint = ts.URL
	if err := manager.registry.Put(ctx, s); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// tools/list is not a tool result — must not be scanned.
	_, err := manager.InvokeServer(ctx, "srv", &MCPRequest{ID: "1", Method: methodToolsList})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if sc.called {
		t.Error("scanner ran for tools/list — only tools/call is boundary content")
	}
}
