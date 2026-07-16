package federation

import (
	"context"
	"testing"
)

// fakeProcessor records the method it saw and applies an optional transform,
// so tests can assert the InvokeServer hook wiring.
type fakeProcessor struct {
	called    bool
	gotMethod string
	transform func(*MCPResponse) *MCPResponse
}

func (f *fakeProcessor) ProcessResult(_ context.Context, req *MCPRequest, resp *MCPResponse) *MCPResponse {
	f.called = true
	if req != nil {
		f.gotMethod = req.Method
	}
	if f.transform != nil {
		return f.transform(resp)
	}
	return resp
}

func markReduced(r *MCPResponse) *MCPResponse {
	if r.Meta == nil {
		r.Meta = map[string]interface{}{}
	}
	r.Meta["reduced"] = true
	return r
}

// The default processor must not alter the response (reduction off until a
// Gatekeeper-backed processor is installed).
func TestNoopResultProcessor_PassThrough(t *testing.T) {
	in := &MCPResponse{ID: "x", Result: "hello"}
	out := noopResultProcessor{}.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, in)
	if out != in {
		t.Fatal("noop processor must return the same response pointer, unchanged")
	}
}

// InvokeServer runs the installed processor with the request method and returns
// its (possibly transformed) result — the reduce-at-source seam.
func TestInvokeServer_RunsResultProcessor(t *testing.T) {
	ts := newTestMCPServer(t)
	defer ts.Close()

	manager := createTestManager()
	server := createTestServer()
	server.Endpoint = ts.URL
	server.Reduce = true // reduction is per-server opt-in
	if err := manager.RegisterServer(server); err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}

	fp := &fakeProcessor{transform: markReduced}
	manager.SetResultProcessor(fp)

	req := &MCPRequest{ID: "r1", Method: methodToolsCall}
	resp, err := manager.InvokeServer(context.Background(), server.ID, req)
	if err != nil {
		t.Fatalf("InvokeServer error: %v", err)
	}
	if !fp.called {
		t.Error("result processor was not invoked")
	}
	if fp.gotMethod != methodToolsCall {
		t.Errorf("processor saw method %q, want tools/call", fp.gotMethod)
	}
	if resp == nil || resp.Meta["reduced"] != true {
		t.Errorf("processor transform not applied to the returned response: %+v", resp)
	}
}

// With no processor installed, InvokeServer returns the response unchanged.
func TestInvokeServer_NoopByDefault(t *testing.T) {
	manager := createTestManager()
	server := createTestServer()
	if err := manager.RegisterServer(server); err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}

	resp, err := manager.InvokeServer(context.Background(), server.ID, &MCPRequest{ID: "r1", Method: methodToolsCall})
	if err != nil {
		t.Fatalf("InvokeServer error: %v", err)
	}
	if resp != nil && resp.Meta["reduced"] == true {
		t.Error("default (no-op) processor must not transform the response")
	}
}

// SetResultProcessor(nil) resets to the no-op (reduction off).
func TestSetResultProcessor_NilResetsToNoop(t *testing.T) {
	manager := createTestManager()
	server := createTestServer()
	if err := manager.RegisterServer(server); err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}

	manager.SetResultProcessor(&fakeProcessor{transform: markReduced})
	manager.SetResultProcessor(nil) // reset

	resp, err := manager.InvokeServer(context.Background(), server.ID, &MCPRequest{ID: "r1", Method: methodToolsCall})
	if err != nil {
		t.Fatalf("InvokeServer error: %v", err)
	}
	if resp != nil && resp.Meta["reduced"] == true {
		t.Error("SetResultProcessor(nil) should reset to the no-op processor")
	}
}
