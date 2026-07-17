package federation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeBlockScanner flags any block containing needle, "redacting" by replacing
// needle with [X]. tier and calls are recorded for assertions.
type fakeBlockScanner struct {
	needle   string
	failOn   string // return an error for a block containing this substring
	tiers    []TrustTier
	calls    int
	lastTier TrustTier
}

func (f *fakeBlockScanner) Scan(_ context.Context, content string, tier TrustTier) (ScanOutcome, error) {
	f.calls++
	f.tiers = append(f.tiers, tier)
	f.lastTier = tier
	if f.failOn != "" && strings.Contains(content, f.failOn) {
		return ScanOutcome{}, errors.New("scan failed")
	}
	if !strings.Contains(content, f.needle) {
		return ScanOutcome{Content: content}, nil
	}
	return ScanOutcome{
		Content:  strings.ReplaceAll(content, f.needle, "[X]"),
		Findings: []ScanFinding{{PatternID: "test", Severity: "high", Preview: "n***e"}},
		Redacted: true,
	}, nil
}

func toolsCallReq() *MCPRequest { return &MCPRequest{Method: methodToolsCall} }

// collectSink records what the sink received.
type collectSink struct {
	servers  []string
	findings int
	redacted bool
	calls    int
}

func (c *collectSink) fn() FindingSink {
	return func(serverID string, findings []ScanFinding, redacted bool) {
		c.calls++
		c.servers = append(c.servers, serverID)
		c.findings += len(findings)
		c.redacted = c.redacted || redacted
	}
}

// Log-only (the default): findings are surfaced but content is NOT modified.
// This is the draft→enforce first stage and the most important behavior to lock.
func TestScanningProcessor_LogOnly_ReportsButDoesNotModify(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	sink := &collectSink{}
	p := NewScanningResultProcessor(sc, nil, sink.fn(), false /* log-only */)

	resp := &MCPResponse{ID: "1", Result: contentResult("here is a SECRET value")}
	out := p.ScanResult(context.Background(), "srv-a", toolsCallReq(), resp)

	if got := firstText(t, out.Result); got != "here is a SECRET value" {
		t.Fatalf("log-only modified content: got %q", got)
	}
	if sink.calls != 1 || sink.findings != 1 {
		t.Fatalf("expected 1 sink call / 1 finding, got %d/%d", sink.calls, sink.findings)
	}
	if sink.redacted {
		t.Fatal("sink reported redacted=true in log-only mode")
	}
	if out.Meta["scanned"] != true || out.Meta["scan_redacted"] == true {
		t.Fatalf("meta wrong for log-only: %+v", out.Meta)
	}
}

// Redact mode: content IS modified in place, and the sink sees redacted=true.
func TestScanningProcessor_Redact_ModifiesContent(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	sink := &collectSink{}
	p := NewScanningResultProcessor(sc, nil, sink.fn(), true /* redact */)

	resp := &MCPResponse{ID: "1", Result: contentResult("here is a SECRET value")}
	out := p.ScanResult(context.Background(), "srv-a", toolsCallReq(), resp)

	if got := firstText(t, out.Result); got != "here is a [X] value" {
		t.Fatalf("redact did not modify content: got %q", got)
	}
	if !sink.redacted || out.Meta["scan_redacted"] != true {
		t.Fatalf("redacted not reported: sink=%v meta=%+v", sink.redacted, out.Meta)
	}
}

func TestScanningProcessor_RedactsBareString(t *testing.T) {
	sc := &fakeBlockScanner{needle: "PII"}
	p := NewScanningResultProcessor(sc, nil, nil, true)
	resp := &MCPResponse{ID: "1", Result: "leak PII now"}
	out := p.ScanResult(context.Background(), "srv", toolsCallReq(), resp)
	if s, _ := out.Result.(string); s != "leak [X] now" {
		t.Fatalf("bare string not redacted: got %q", out.Result)
	}
}

// No findings → response untouched, no sink call, no meta.
func TestScanningProcessor_CleanContentUntouched(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	sink := &collectSink{}
	p := NewScanningResultProcessor(sc, nil, sink.fn(), true)
	resp := &MCPResponse{ID: "1", Result: contentResult("nothing to see")}
	out := p.ScanResult(context.Background(), "srv", toolsCallReq(), resp)

	if got := firstText(t, out.Result); got != "nothing to see" {
		t.Fatalf("clean content changed: %q", got)
	}
	if sink.calls != 0 {
		t.Fatalf("sink called for clean content")
	}
	if _, ok := out.Meta["scanned"]; ok {
		t.Fatalf("meta stamped for clean content: %+v", out.Meta)
	}
}

// Only tools/call is scanned; other methods pass through with no scan call.
func TestScanningProcessor_OnlyToolsCall(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	p := NewScanningResultProcessor(sc, nil, nil, true)
	resp := &MCPResponse{ID: "1", Result: contentResult("a SECRET here")}
	p.ScanResult(context.Background(), "srv", &MCPRequest{Method: methodToolsList}, resp)
	if sc.calls != 0 {
		t.Fatalf("scanner called for non-tools/call method")
	}
	if got := firstText(t, resp.Result); got != "a SECRET here" {
		t.Fatalf("non-tools/call content modified: %q", got)
	}
}

// Fail-open: a scan error keeps the original block, never drops/corrupts it.
func TestScanningProcessor_FailOpenOnScanError(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET", failOn: "SECRET"}
	sink := &collectSink{}
	p := NewScanningResultProcessor(sc, nil, sink.fn(), true)
	resp := &MCPResponse{ID: "1", Result: contentResult("a SECRET value")}
	out := p.ScanResult(context.Background(), "srv", toolsCallReq(), resp)

	if got := firstText(t, out.Result); got != "a SECRET value" {
		t.Fatalf("fail-open corrupted content: %q", got)
	}
	if sink.calls != 0 {
		t.Fatalf("sink called on scan error")
	}
}

// The trust tier flows to the scanner; default (nil tierFor) is TierExternal —
// the safe posture for federated tool output.
func TestScanningProcessor_TierDefaultsExternal(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	p := NewScanningResultProcessor(sc, nil, nil, true)
	resp := &MCPResponse{ID: "1", Result: contentResult("a SECRET value")}
	p.ScanResult(context.Background(), "srv", toolsCallReq(), resp)
	if sc.lastTier != TierExternal {
		t.Fatalf("default tier = %v, want TierExternal", sc.lastTier)
	}
}

func TestScanningProcessor_TierFromMapper(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	tierFor := func(id string) TrustTier {
		if id == "trusted" {
			return TierPartner
		}
		return TierExternal
	}
	p := NewScanningResultProcessor(sc, tierFor, nil, true)
	resp := &MCPResponse{ID: "1", Result: contentResult("a SECRET value")}
	p.ScanResult(context.Background(), "trusted", toolsCallReq(), resp)
	if sc.lastTier != TierPartner {
		t.Fatalf("tier = %v, want TierPartner for trusted server", sc.lastTier)
	}
}

// Multiple text blocks are each scanned; findings aggregate into one sink call.
func TestScanningProcessor_ScansEveryBlock(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	sink := &collectSink{}
	p := NewScanningResultProcessor(sc, nil, sink.fn(), true)
	resp := &MCPResponse{ID: "1", Result: contentResult("SECRET one", "clean", "SECRET two")}
	out := p.ScanResult(context.Background(), "srv", toolsCallReq(), resp)

	if sc.calls != 3 {
		t.Fatalf("scanned %d blocks, want 3", sc.calls)
	}
	if sink.findings != 2 { // two blocks had the needle
		t.Fatalf("aggregated %d findings, want 2", sink.findings)
	}
	if got := firstText(t, out.Result); got != "[X] one" {
		t.Fatalf("first block not redacted: %q", got)
	}
}

// A nil scanner is a pass-through (defensive; callers install nothing instead).
func TestScanningProcessor_NilScannerPassesThrough(t *testing.T) {
	p := NewScanningResultProcessor(nil, nil, nil, true)
	resp := &MCPResponse{ID: "1", Result: contentResult("a SECRET value")}
	out := p.ScanResult(context.Background(), "srv", toolsCallReq(), resp)
	if got := firstText(t, out.Result); got != "a SECRET value" {
		t.Fatalf("nil scanner modified content: %q", got)
	}
}

// An error response is never scanned (nothing to redact; don't touch errors).
func TestScanningProcessor_ErrorResponseUntouched(t *testing.T) {
	sc := &fakeBlockScanner{needle: "SECRET"}
	p := NewScanningResultProcessor(sc, nil, nil, true)
	resp := &MCPResponse{ID: "1", Error: &MCPError{Message: "boom"}, Result: contentResult("a SECRET value")}
	p.ScanResult(context.Background(), "srv", toolsCallReq(), resp)
	if sc.calls != 0 {
		t.Fatalf("scanned an error response")
	}
}
