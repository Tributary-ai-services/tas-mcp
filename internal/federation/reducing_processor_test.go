package federation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// truncReducer is a deterministic fake: it keeps the first half of the content,
// so len(reduced) < len(orig) for any non-trivial input, and identical input
// always yields identical output. It counts calls so tests can assert the
// reduce-once cache.
type truncReducer struct {
	calls    atomic.Int64
	err      error
	mu       sync.Mutex
	gotQuery string
}

func (r *truncReducer) Reduce(_ context.Context, content, query string) (string, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.gotQuery = query
	r.mu.Unlock()
	if r.err != nil {
		return "", r.err
	}
	return content[:len(content)/2], nil
}

func (r *truncReducer) query() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gotQuery
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
	out := p.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, resp)

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
	out := p.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, resp)

	if s, _ := out.Result.(string); len(s) != 40 {
		t.Errorf("bare string not reduced: got %q (%d bytes), want 40", out.Result, len(s))
	}
}

func TestReducingProcessor_OnlyToolsCall(t *testing.T) {
	r := &truncReducer{}
	p := NewReducingResultProcessor(r)
	resp := &MCPResponse{ID: "1", Result: contentResult(strings.Repeat("x", 100))}

	out := p.ProcessResult(context.Background(), &MCPRequest{Method: "tools/list"}, resp)
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
	out := p.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, resp)

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

	p.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, resp)
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
		out := p.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, resp)
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
	out := p.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, resp)

	if out.Result != 42 || out.Meta["reduced"] == true {
		t.Errorf("unknown result shape should pass through: %#v", out)
	}
	if r.calls.Load() != 0 {
		t.Error("reducer ran on an unknown result shape")
	}
}

// The tool-call arguments' query is threaded to the reducer as the relevance
// anchor (this is what makes relevance-mode reduction work).
func TestReducingProcessor_ThreadsToolCallQuery(t *testing.T) {
	r := &truncReducer{}
	p := NewReducingResultProcessor(r)
	req := &MCPRequest{
		Method: methodToolsCall,
		Params: map[string]interface{}{
			"name":      "search",
			"arguments": map[string]interface{}{"query": "capital of France"},
		},
	}
	resp := &MCPResponse{ID: "1", Result: contentResult(strings.Repeat("x", 100))}
	p.ProcessResult(context.Background(), req, resp)

	if r.query() != "capital of France" {
		t.Errorf("reducer got query %q, want the tool's query argument", r.query())
	}
}

func TestToolCallQuery(t *testing.T) {
	args := func(m map[string]interface{}) *MCPRequest {
		return &MCPRequest{Method: methodToolsCall, Params: map[string]interface{}{"arguments": m}}
	}
	cases := []struct {
		name string
		req  *MCPRequest
		want string
	}{
		{"natural field", args(map[string]interface{}{"query": "hi"}), "hi"},
		{"question field", args(map[string]interface{}{"question": "why"}), "why"},
		{"json fallback", args(map[string]interface{}{"table": "users"}), `{"table":"users"}`},
		{"no args", &MCPRequest{Method: methodToolsCall}, ""},
		{"nil req", nil, ""},
		{"empty args", args(map[string]interface{}{}), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toolCallQuery(c.req); got != c.want {
				t.Errorf("toolCallQuery = %q, want %q", got, c.want)
			}
		})
	}
}

// The reduce-once cache is bounded: past its capacity the least-recently-used
// entry is evicted so it can't grow without limit.
func TestReduceLRU_EvictsLeastRecentlyUsed(t *testing.T) {
	c := newReduceLRU(2)
	c.put("a", "A")
	c.put("b", "B")
	// Touch "a" so "b" becomes least-recently-used.
	if _, ok := c.get("a"); !ok {
		t.Fatal("a should be present")
	}
	c.put("c", "C") // over capacity → evicts "b"

	if _, ok := c.get("b"); ok {
		t.Error("b should have been evicted (LRU)")
	}
	if v, ok := c.get("a"); !ok || v != "A" {
		t.Error("a should still be present")
	}
	if v, ok := c.get("c"); !ok || v != "C" {
		t.Error("c should be present")
	}
	if c.ll.Len() != 2 || len(c.m) != 2 {
		t.Errorf("cache exceeded bound: ll=%d map=%d, want 2/2", c.ll.Len(), len(c.m))
	}
}

func TestReducingProcessor_NilReducerPassthrough(t *testing.T) {
	p := NewReducingResultProcessor(nil)
	orig := strings.Repeat("x", 100)
	resp := &MCPResponse{ID: "1", Result: contentResult(orig)}
	out := p.ProcessResult(context.Background(), &MCPRequest{Method: methodToolsCall}, resp)

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
