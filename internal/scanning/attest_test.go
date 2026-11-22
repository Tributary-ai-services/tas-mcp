package scanning

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tributary-ai-services/Gatekeeper/pkg/scan"
	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

const (
	gwTenant  = "a689c0b2-0000-4000-8000-000000000001"
	gwBuild   = "d6f4b1a"
	gwRuleset = "c955bcba0540bfc6"
	emailText = "Contact jane@example.com for access."
)

// Keys computed by tas-llm-router's own scanOnceKey (internal/gatekeeper/
// scanonce.go, origin/main d892cea) for profile "full", tier external,
// ruleset c955bcba0540bfc6 (Gatekeeper 8439cfe default registry) and build
// d6f4b1a. If these drift, the gateway stops recognizing proxy attestations.
var gatewayKeys = map[string]string{
	emailText:               gk("c4c254f9587b23bfcc0569963e9c8deb099bc141a0dccf1542225b201d5269be"),
	"Cafe\u0301 menu  \n\t": gk("d3bd7b817298938372293328f74ab0b6c203469170fca8f26bbc846084e6c9bd"),
	"line one\nline two":    gk("b6858b03a6cae635deeaeab09a74e598979b72c917cbfff0bb3fe2cd05111dbc"),
}

func gk(sha string) string {
	return "gk:scan1:" + gwTenant + ":" + sha + ":full:2:" + gwRuleset + ":" + gwBuild
}

func TestAttestKey_MatchesGateway(t *testing.T) {
	for content, want := range gatewayKeys {
		if got := AttestKey(gwTenant, content, scan.ProfileFull, scan.TierExternal, gwRuleset, gwBuild); got != want {
			t.Errorf("%q:\n got %s\nwant %s", content, got, want)
		}
	}
	// NFC + right-trim only: the decomposed é with trailing space keys like
	// the composed, trimmed form; case and inner spacing still matter.
	if AttestKey(gwTenant, "Caf\u00e9 menu", scan.ProfileFull, scan.TierExternal, gwRuleset, gwBuild) !=
		gatewayKeys["Cafe\u0301 menu  \n\t"] {
		t.Error("normalisation differs from the gateway's")
	}
	if AttestKey(gwTenant, "cafe menu", scan.ProfileFull, scan.TierExternal, gwRuleset, gwBuild) ==
		AttestKey(gwTenant, "Cafe menu", scan.ProfileFull, scan.TierExternal, gwRuleset, gwBuild) {
		t.Error("normalisation must not fold case")
	}
}

// The record JSON is the gateway's ScanRecord: {} clean, {"p":[...]}.
func TestAttestRecord_JSONMatchesGateway(t *testing.T) {
	a, _ := json.Marshal(AttestRecord{})
	b, _ := json.Marshal(AttestRecord{Patterns: []string{"pii-email"}})
	if string(a) != `{}` || string(b) != `{"p":["pii-email"]}` {
		t.Errorf("records = %s %s", a, b)
	}
}

func newMini(t *testing.T) (*miniredis.Miniredis, AttestStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	store, err := NewRedisAttestStore("redis://" + mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	return mr, store
}

func newScanner(t *testing.T, redact bool) *Scanner {
	t.Helper()
	s, err := New(Config{Enabled: true, Redact: redact, RedactStrategy: "replace"})
	if err != nil || s == nil {
		t.Fatalf("scanner: %v", err)
	}
	return s
}

// gatewayLookup is what the gateway does on its next turn: hash what it is
// about to scan and GET the key.
func gatewayLookup(t *testing.T, mr *miniredis.Miniredis, content, ruleset string) (AttestRecord, bool) {
	t.Helper()
	// The live gateway scans inbound at "internal" (tier 0).
	v, err := mr.Get(AttestKey(gwTenant, content, scan.ProfileFull, scan.TierInternal, ruleset, gwBuild))
	if err != nil {
		return AttestRecord{}, false
	}
	var rec AttestRecord
	if json.Unmarshal([]byte(v), &rec) != nil {
		t.Fatalf("unreadable record %q", v)
	}
	return rec, true
}

func scanThroughProxy(t *testing.T, s *Scanner, a *Attestor, result interface{}) *federation.MCPResponse {
	t.Helper()
	p := federation.NewScanningResultProcessor(s, nil, nil, s.redact, federation.WithAttestor(a))
	ctx := identity.WithIdentity(context.Background(), &identity.Identity{Attributed: true, TenantID: gwTenant})
	resp := p.ScanResult(ctx, "napkin-mcp", &federation.MCPRequest{Method: "tools/call"},
		&federation.MCPResponse{Result: result})
	if err := a.Close(); err != nil { // drain the queue
		t.Fatal(err)
	}
	return resp
}

func textResult(texts ...string) map[string]interface{} {
	items := make([]interface{}, 0, len(texts))
	for _, s := range texts {
		items = append(items, map[string]interface{}{"type": "text", "text": s})
	}
	return map[string]interface{}{"content": items}
}

// End to end against the real Gatekeeper scanner: the gateway finds the
// proxy's attestation for both a clean and a dirty result, and the TTL is
// the gateway's 24h.
func TestAttestor_GatewayFindsProxyScan(t *testing.T) {
	mr, store := newMini(t)
	s := newScanner(t, false)
	a, err := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	scanThroughProxy(t, s, a, textResult("All quiet on the", "western front."))

	a, _ = NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}}, nil)
	scanThroughProxy(t, s, a, emailText)

	rs := s.RulesetVersion()
	t.Logf("proxy ruleset %s (gateway vectors were computed at %s)", rs, gwRuleset)
	clean, ok := gatewayLookup(t, mr, "All quiet on the\nwestern front.", rs)
	if !ok || len(clean.Patterns) != 0 {
		t.Errorf("clean result: rec=%+v ok=%v; want a clean record (gateway skips)", clean, ok)
	}
	dirty, ok := gatewayLookup(t, mr, emailText, rs)
	if !ok || len(dirty.Patterns) != 1 || dirty.Patterns[0] != "pii-email" {
		t.Errorf("email result: rec=%+v ok=%v; want {p:[pii-email]} (gateway re-scans, does not count)", dirty, ok)
	}
	key := AttestKey(gwTenant, emailText, scan.ProfileFull, scan.TierInternal, rs, gwBuild)
	if ttl := mr.TTL(key); ttl != DefaultAttestTTL {
		t.Errorf("ttl = %v, want %v", ttl, DefaultAttestTTL)
	}
}

// Redacted: the delivered text is attested too, so the gateway recognizes
// it later; it vouches for what a scan of the output finds.
func TestAttestor_RedactedOutputAttested(t *testing.T) {
	mr, store := newMini(t)
	s := newScanner(t, true)
	a, _ := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}}, nil)
	resp := scanThroughProxy(t, s, a, emailText)

	out, _ := resp.Result.(string)
	if out == emailText || strings.Contains(out, "jane@example.com") {
		t.Fatalf("not redacted: %q", out)
	}
	rs := s.RulesetVersion()
	in, ok := gatewayLookup(t, mr, emailText, rs)
	if !ok || len(in.Patterns) != 1 {
		t.Errorf("input record = %+v ok=%v", in, ok)
	}
	outRec, ok := gatewayLookup(t, mr, out, rs)
	if !ok {
		t.Fatalf("redacted output %q not attested", out)
	}
	if len(outRec.Patterns) != 0 {
		t.Errorf("redacted output still matches %v (fine, but say so)", outRec.Patterns)
	}
}

// A rule change re-scans: the ruleset digest is in the key, so after the
// rules change the gateway looks up a key nobody wrote and scans.
func TestAttestor_RulesetChangeRescans(t *testing.T) {
	mr, store := newMini(t)
	s := newScanner(t, false)
	a, _ := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}}, nil)
	scanThroughProxy(t, s, a, "hello world")

	newRules := &Scanner{sc: scan.NewScannerWithRegistry(scan.NewPatternRegistry()), profile: s.profile}
	if newRules.RulesetVersion() == s.RulesetVersion() {
		t.Fatal("precondition: a different registry must give a different ruleset digest")
	}
	if _, ok := gatewayLookup(t, mr, "hello world", s.RulesetVersion()); !ok {
		t.Fatal("precondition: attested under the current rules")
	}
	if _, ok := gatewayLookup(t, mr, "hello world", newRules.RulesetVersion()); ok {
		t.Error("an attestation made under old rules was found under new rules; content would not be re-scanned")
	}
}

// One key per configured gateway build (a rolling deploy runs two).
func TestAttestor_OneKeyPerGatewayBuild(t *testing.T) {
	_, store := newMini(t)
	s := newScanner(t, false)
	a, _ := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild, " abc1234 ", ""}}, nil)
	defer func() { _ = a.Close() }()
	recs, err := a.Records(context.Background(), federation.ScanAttestation{TenantID: gwTenant, Content: "x"})
	if err != nil || len(recs) != 2 {
		t.Fatalf("records = %v err=%v, want 2 keys", recs, err)
	}
	for k := range recs {
		if !strings.HasSuffix(k, ":"+gwBuild) && !strings.HasSuffix(k, ":abc1234") {
			t.Errorf("unexpected key %s", k)
		}
	}
}

func TestNewAttestor_RefusesWhatCannotMatch(t *testing.T) {
	_, store := newMini(t)
	if _, err := NewAttestor(store, newScanner(t, false), AttestConfig{Enabled: true}, nil); err == nil {
		t.Error("no gateway build: want error")
	}
	s, _ := New(Config{Enabled: true, MinConfidence: 0.9})
	if _, err := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}}, nil); err == nil {
		t.Error("non-default confidence: want error")
	}
}

type failingStore struct{ calls int }

func (f *failingStore) SetMany(context.Context, map[string]AttestRecord, time.Duration) error {
	f.calls++
	return errors.New("redis: connection refused")
}

type blockingStore struct {
	release chan struct{}
}

func (b *blockingStore) SetMany(ctx context.Context, _ map[string]AttestRecord, _ time.Duration) error {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil
}

// Fail open: a store error leaves the tool result untouched and is counted;
// the gateway, finding nothing, scans as before.
func TestAttestor_StoreErrorFailsOpen(t *testing.T) {
	before := testutil.ToFloat64(attestations.WithLabelValues("failed"))
	fs := &failingStore{}
	s := newScanner(t, false)
	a, _ := NewAttestor(fs, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}}, nil)
	resp := scanThroughProxy(t, s, a, emailText)
	if resp.Result != emailText || fs.calls != 1 {
		t.Errorf("result=%v calls=%d", resp.Result, fs.calls)
	}
	if got := testutil.ToFloat64(attestations.WithLabelValues("failed")) - before; got < 1 {
		t.Errorf("failed counter rose by %v", got)
	}
}

// A wedged store never slows a tool call: the queue fills and drops.
func TestAttestor_FullQueueDropsWithoutBlocking(t *testing.T) {
	bs := &blockingStore{release: make(chan struct{})}
	s := newScanner(t, false)
	a, _ := NewAttestor(bs, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}, BufferSize: 1}, nil)
	start := time.Now()
	for i := 0; i < 200; i++ {
		a.Attest(context.Background(), federation.ScanAttestation{TenantID: gwTenant, Content: "x"})
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("Attest blocked for %v", d)
	}
	close(bs.release)
	_ = a.Close()
	a.Attest(context.Background(), federation.ScanAttestation{TenantID: gwTenant, Content: "x"}) // closed: dropped
}

// The shape of a live key (redis-shared, 2026-11-21): tier 0 and build
// 5.120.0 after both gateways rolled; the proxy writes exactly that shape.
func TestAttestor_WritesLiveKeyShape(t *testing.T) {
	_, store := newMini(t)
	s := newScanner(t, false)
	a, err := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{"5.120.0"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	recs, _ := a.Records(context.Background(), federation.ScanAttestation{TenantID: gwTenant, Content: emailText,
		Tier: federation.TierExternal})
	want := "gk:scan1:" + gwTenant + ":c4c254f9587b23bfcc0569963e9c8deb099bc141a0dccf1542225b201d5269be:full:0:" +
		s.RulesetVersion() + ":5.120.0"
	if _, ok := recs[want]; !ok || len(recs) != 1 {
		t.Errorf("records %v, want exactly %s", recs, want)
	}
}

// A scan covers its own tier and every lower one (Gatekeeper isTierAdequate);
// a scan below the gateway's tier is never attested.
func TestAttestor_TierAdequacy(t *testing.T) {
	_, store := newMini(t)
	s := newScanner(t, false)
	ext, err := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}, GatewayTier: "external"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ext.Close() }()
	got, _ := ext.Records(context.Background(), federation.ScanAttestation{TenantID: gwTenant, Content: "x", Tier: federation.TierExternal})
	for k := range got {
		if !strings.Contains(k, ":full:2:") {
			t.Errorf("key %s, want tier 2", k)
		}
	}
	if len(got) != 1 {
		t.Errorf("external scan for an external gateway: %d records, want 1", len(got))
	}
	if got, _ := ext.Records(context.Background(), federation.ScanAttestation{TenantID: gwTenant, Content: "x",
		Tier: federation.TierPartner}); len(got) != 0 {
		t.Errorf("a partner scan vouched for an external lookup: %v", got)
	}
	if _, err := NewAttestor(store, s, AttestConfig{Enabled: true, GatewayBuilds: []string{gwBuild}, GatewayTier: "bogus"}, nil); err == nil {
		t.Error("bogus tier accepted")
	}
}
