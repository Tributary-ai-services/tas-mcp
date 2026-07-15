package federation

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// truncReducer is a deterministic fake: it keeps the first half of the content,
// so len(reduced) < len(orig) for any non-trivial input, and identical input
// always yields identical output. It counts calls so tests can assert the
// reduce-once cache.
type truncReducer struct {
	calls atomic.Int64
	err   error
}

func (r *truncReducer) Reduce(_ context.Context, content, _ string) (string, error) {
	r.calls.Add(1)
	if r.err != nil {
		return "", r.err
	}
	return content[:len(content)/2], nil
}

func contentResult(texts ...string) map[string]interface{} {
	items := make([]interface{}, 0, len(texts))
	for _, t := range texts {
		items = append(items, map[string]interface{}{"type": "text", "text": t})
	}
	return map[string]interface{}{"content": items}
}

func firstText(t *testing.T, result interface{}) string {
	t.Helper()
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a content object: %#v", result)
	}
	items := m["content"].([]interface{})
	return items[0].(map[string]interface{})["text"].(string)
}

func TestReducingProcessor_ReducesContentArray(t *testing.T) {
	r := &truncReducer{}
	p := NewReducingResultProcessor(r)

	orig := strings.Repeat("x", 100)
	resp := &MCPResponse{ID: "1", Result: contentResult(orig)}
	out := p.ProcessResult(context.Background(), "tools/call", resp)

	if got := firstText(t, out.Result); len(got) != 50 {
		t.Errorf("text not reduced: got %d bytes, want 50", len(got))
	}
	if out.Meta["reduced"] != true {
		t.Errorf("reduced meta not set: %#v", out.Meta)
	}
	if out.Meta["reduced_bytes_saved"] != 50 {
		t.Errorf("reduced_bytes_saved = %v, want 50", out.Meta["reduced_bytes_saved"])
	}
}

func TestReducingProcessor_ReducesBareString(t *testing.T) {
	p := NewReducingResultProcessor(&truncReducer{})
	resp := &MCPResponse{ID: "1", Result: strings.Repeat("y", 80)}
	out := p.ProcessResult(context.Background(), "tools/call", resp)

	if s, _ := out.Result.(string); len(s) != 40 {
		t.Errorf("bare string not reduced: got %q (%d bytes), want 40", out.Result, len(s))
	}
}

func TestReducingProcessor_OnlyToolsCall(t *testing.T) {
	r := &truncReducer{}
	p := NewReducingResultProcessor(r)
	resp := &MCPResponse{ID: "1", Result: contentResult(strings.Repeat("x", 100))}

	out := p.ProcessResult(context.Background(), "tools/list", resp)
	if r.calls.Load() != 0 {
		t.Error("reducer was called for a non-tools/call method")
	}
	if out.Meta["reduced"] == true {
		t.Error("non-tools/call response must not be marked reduced")
	}
}

func TestReducingProcessor_FailOpenOnReducerError(t *testing.T) {
	r := &truncReducer{err: errors.New("slm down")}
	p := NewReducingResultProcessor(r)

	orig := strings.Repeat("x", 100)
	resp := &MCPResponse{ID: "1", Result: contentResult(orig)}
	out := p.ProcessResult(context.Background(), "tools/call", resp)

	if got := firstText(t, out.Result); got != orig {
		t.Errorf("content changed on reducer error: got %d bytes, want original %d", len(got), len(orig))
	}
	if out.Meta["reduced"] == true {
		t.Error("must not mark reduced when reduction failed")
	}
}

func TestReducingProcessor_SkipsErrorResponse(t *testing.T) {
	r := &truncReducer{}
	p := NewReducingResultProcessor(r)
	resp := &MCPResponse{ID: "1", Error: &MCPError{Code: 1, Message: "boom"}, Result: contentResult("xxxx")}

	p.ProcessResult(context.Background(), "tools/call", resp)
	if r.calls.Load() != 0 {
		t.Error("reducer ran on an error response")
	}
}

func TestReducingProcessor_ReduceOnceCache(t *testing.T) {
	r := &truncReducer{}
	p := NewReducingResultProcessor(r)
	orig := strings.Repeat("z", 100)

	for i := 0; i < 3; i++ {
		resp := &MCPResponse{ID: "1", Result: contentResult(orig)}
		out := p.ProcessResult(context.Background(), "tools/call", resp)
		if got := firstText(t, out.Result); len(got) != 50 {
			t.Fatalf("iteration %d: text not reduced deterministically: %d bytes", i, len(got))
		}
	}
	if n := r.calls.Load(); n != 1 {
		t.Errorf("reducer called %d times for identical content, want 1 (reduce-once)", n)
	}
}

func TestReducingProcessor_UnknownShapePassthrough(t *testing.T) {
	r := &truncReducer{}
	p := NewReducingResultProcessor(r)
	// A number result matches no known shape → untouched, reducer never runs.
	resp := &MCPResponse{ID: "1", Result: 42}
	out := p.ProcessResult(context.Background(), "tools/call", resp)

	if out.Result != 42 || out.Meta["reduced"] == true {
		t.Errorf("unknown result shape should pass through: %#v", out)
	}
	if r.calls.Load() != 0 {
		t.Error("reducer ran on an unknown result shape")
	}
}

func TestReducingProcessor_NilReducerPassthrough(t *testing.T) {
	p := NewReducingResultProcessor(nil)
	orig := strings.Repeat("x", 100)
	resp := &MCPResponse{ID: "1", Result: contentResult(orig)}
	out := p.ProcessResult(context.Background(), "tools/call", resp)

	if got := firstText(t, out.Result); got != orig {
		t.Error("nil-reducer processor must pass content through unchanged")
	}
}

// The processor installs cleanly as the Manager's ResultProcessor and runs on
// the InvokeServer path (integration with the Slice 1 seam).
func TestReducingProcessor_WiresIntoManager(t *testing.T) {
	manager := createTestManager()
	server := createTestServer()
	if err := manager.RegisterServer(server); err != nil {
		t.Fatalf("Failed to register server: %v", err)
	}
	manager.SetResultProcessor(NewReducingResultProcessor(&truncReducer{}))

	resp, err := manager.InvokeServer(context.Background(), server.ID, &MCPRequest{ID: "r1", Method: "tools/list"})
	if err != nil {
		t.Fatalf("InvokeServer error: %v", err)
	}
	// tools/list is not reduced, but the processor must not error or corrupt it.
	if resp == nil {
		t.Fatal("nil response from InvokeServer")
	}
}
