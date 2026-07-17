package scanning

import (
	"context"
	"strings"
	"testing"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

// End-to-end against the real Gatekeeper scanner: a tool result carrying an
// email is detected, and in log-only mode the content is returned unchanged.
func TestScanner_DetectsEmail_LogOnly(t *testing.T) {
	s, err := New(Config{Enabled: true, Redact: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const content = "contact the user at alice@example.com for details"
	out, err := s.Scan(context.Background(), content, federation.TierExternal)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(out.Findings) == 0 {
		t.Fatal("no findings for an obvious email — the scanner isn't detecting")
	}
	if out.Content != content {
		t.Fatalf("log-only modified content: %q", out.Content)
	}
	if out.Redacted {
		t.Fatal("log-only reported Redacted=true")
	}
	// The finding must be log-safe: a masked preview, never the raw address.
	for _, f := range out.Findings {
		if strings.Contains(f.Preview, "alice@example.com") {
			t.Fatalf("finding preview leaked the raw value: %q", f.Preview)
		}
	}
}

// Redact mode against the real scanner + redaction engine: the email is gone
// from the returned content, and redaction is deterministic (same in → same out).
func TestScanner_RedactsEmail_Deterministic(t *testing.T) {
	s, err := New(Config{Enabled: true, Redact: true, RedactStrategy: "mask"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const content = "reach alice@example.com now"
	out1, err := s.Scan(context.Background(), content, federation.TierExternal)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !out1.Redacted || out1.Content == content {
		t.Fatalf("redact did not change content: %q", out1.Content)
	}
	if strings.Contains(out1.Content, "alice@example.com") {
		t.Fatalf("raw email survived redaction: %q", out1.Content)
	}
	// Determinism is the cache-safety contract: identical input must redact to
	// identical bytes, or a redacted tool result would break prompt caching.
	out2, _ := s.Scan(context.Background(), content, federation.TierExternal)
	if out1.Content != out2.Content {
		t.Fatalf("non-deterministic redaction: %q vs %q", out1.Content, out2.Content)
	}
}

// Clean content yields no findings and unchanged content.
func TestScanner_CleanContent(t *testing.T) {
	s, _ := New(Config{Enabled: true, Redact: true})
	const content = "the weather is fine today"
	out, err := s.Scan(context.Background(), content, federation.TierExternal)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(out.Findings) != 0 {
		t.Fatalf("false positives on clean content: %+v", out.Findings)
	}
	if out.Content != content || out.Redacted {
		t.Fatalf("clean content altered: %q redacted=%v", out.Content, out.Redacted)
	}
}

// Disabled config → New returns (nil, nil) so the caller installs nothing.
func TestNew_DisabledIsNil(t *testing.T) {
	s, err := New(Config{Enabled: false})
	if err != nil || s != nil {
		t.Fatalf("disabled New = (%v, %v), want (nil, nil)", s, err)
	}
}

// The adapter must never route redaction through the tokenize (Databunker) path,
// whatever the config says — tokenize isn't deployed and would break caching.
func TestParseStrategy_NeverTokenize(t *testing.T) {
	for _, in := range []string{"tokenize", "remove", "", "bogus", "mask"} {
		if got := parseStrategy(in); string(got) == "tokenize" || string(got) == "remove" {
			t.Fatalf("parseStrategy(%q) = %q — must never be tokenize/remove", in, got)
		}
	}
}

// Satisfy the port at compile time.
var _ federation.BlockScanner = (*Scanner)(nil)
