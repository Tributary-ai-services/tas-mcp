package federation

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// The processor used to be silent: a reducer error was swallowed by the
// fail-open path, and "reduced nothing" was indistinguishable from "never ran".
// Diagnosing a live "reduction does nothing" report meant reading the source
// and guessing, so these pin that every outcome is now attributable from logs
// alone.

type stubReducer struct {
	out string
	err error
}

func (s *stubReducer) Reduce(_ context.Context, content, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.out != "" {
		return s.out, nil
	}
	return content, nil
}

func logsFor(t *testing.T, r Reducer, req *MCPRequest, resp *MCPResponse) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	p := NewReducingResultProcessor(r, zap.New(core))
	p.ProcessResult(context.Background(), req, resp)
	return logs
}

func callReq(tool, query string) *MCPRequest {
	return &MCPRequest{Method: methodToolsCall, Params: map[string]interface{}{
		"name": tool, "arguments": map[string]interface{}{"query": query},
	}}
}

func reasonOf(t *testing.T, logs *observer.ObservedLogs) string {
	t.Helper()
	all := logs.All()
	if len(all) == 0 {
		t.Fatal("processor logged nothing — the silence this change exists to fix")
	}
	for _, f := range all[len(all)-1].Context {
		if f.Key == "reason" {
			return f.String
		}
	}
	return ""
}

func TestLogsReductionWithSavings(t *testing.T) {
	logs := logsFor(t, &stubReducer{out: "short"},
		callReq("search_papers", "q"), &MCPResponse{Result: "a much longer original body"})

	e := logs.All()[len(logs.All())-1]
	if e.Level != zapcore.InfoLevel {
		t.Errorf("level = %v, want info for a real reduction", e.Level)
	}
	if e.Message != "reduce-at-source: reduced" {
		t.Errorf("message = %q", e.Message)
	}
	got := map[string]bool{}
	for _, f := range e.Context {
		got[f.Key] = true
	}
	// Without bytes_in and saved_pct an operator cannot tell a 1% trim from a
	// 70% strip, which is the number that matters.
	for _, k := range []string{"tool", "bytes_in", "bytes_saved", "saved_pct", "query_len", "cache_hits"} {
		if !got[k] {
			t.Errorf("reduction log missing field %q", k)
		}
	}
}

// The case that cost hours: the reducer ran fine and simply dropped nothing.
func TestLogsNothingDroppedDistinctly(t *testing.T) {
	logs := logsFor(t, &stubReducer{}, callReq("query", "some query"),
		&MCPResponse{Result: "content that comes back unchanged"})
	if r := reasonOf(t, logs); r != "nothing_dropped" {
		t.Errorf("reason = %q, want nothing_dropped", r)
	}
}

// An empty anchor means the relevance path is unavailable — with the SLM off
// that is a guaranteed no-op, and it must not look like "nothing to drop".
func TestLogsMissingQueryAnchorDistinctly(t *testing.T) {
	req := &MCPRequest{Method: methodToolsCall, Params: map[string]interface{}{"name": "t"}}
	logs := logsFor(t, &stubReducer{}, req, &MCPResponse{Result: "body"})
	if r := reasonOf(t, logs); r != "no_query_anchor" {
		t.Errorf("reason = %q, want no_query_anchor", r)
	}
}

func TestLogsReducerFailureAtWarn(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	p := NewReducingResultProcessor(&stubReducer{err: errors.New("embedder unreachable")}, zap.New(core))
	resp := p.ProcessResult(context.Background(), callReq("t", "q"), &MCPResponse{Result: "body"})

	// Fail-open must still hold.
	if resp.Result != "body" {
		t.Errorf("fail-open broken: result = %v", resp.Result)
	}
	if logs.FilterLevelExact(zapcore.WarnLevel).Len() == 0 {
		t.Error("a reducer failure must WARN — it is a real fault that used to be invisible")
	}
	if r := reasonOf(t, logs); r != "reducer_failed" {
		t.Errorf("reason = %q, want reducer_failed", r)
	}
}

// Repeat calls are served from the memo cache; without this an operator sees
// "no reduction" and cannot tell it was already reduced earlier.
func TestLogsCacheHits(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	p := NewReducingResultProcessor(&stubReducer{out: "s"}, zap.New(core))
	for i := 0; i < 2; i++ {
		p.ProcessResult(context.Background(), callReq("t", "q"), &MCPResponse{Result: "a longer body"})
	}
	last := logs.All()[len(logs.All())-1]
	for _, f := range last.Context {
		if f.Key == "cache_hits" && f.Integer == 1 {
			return
		}
	}
	t.Error("second identical call should report cache_hits=1")
}

// Non-tools/call methods must stay silent — logging every list call would bury
// the signal.
func TestDoesNotLogNonToolCalls(t *testing.T) {
	logs := logsFor(t, &stubReducer{}, &MCPRequest{Method: methodToolsList}, &MCPResponse{Result: "x"})
	if logs.Len() != 0 {
		t.Errorf("expected silence for %s, got %d lines", methodToolsList, logs.Len())
	}
}
