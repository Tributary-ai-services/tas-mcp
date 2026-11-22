package federation

import (
	"context"
	"sync"
	"testing"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

type recordingAttestor struct {
	mu  sync.Mutex
	got []ScanAttestation
}

func (r *recordingAttestor) Attest(_ context.Context, a ScanAttestation) { //nolint:gocritic // interface signature
	r.mu.Lock()
	r.got = append(r.got, a)
	r.mu.Unlock()
}

const attestTenant = "a689c0b2-0000-4000-8000-000000000001"

func tenantCtx() context.Context {
	return identity.WithIdentity(context.Background(), &identity.Identity{Attributed: true, TenantID: attestTenant})
}

func twoBlockResult(a, b string) *MCPResponse {
	return &MCPResponse{Result: map[string]interface{}{"content": []interface{}{
		map[string]interface{}{"type": "text", "text": a},
		map[string]interface{}{"type": "image", "data": "..."},
		map[string]interface{}{"type": "text", "text": ""},
		map[string]interface{}{"type": "text", "text": b},
	}}}
}

// The attestation names the text the gateway will meet: non-empty text
// blocks joined by "\n", as the gateway's extractMessagesText joins them.
func TestScanAttest_JoinsBlocksAndCarriesPatterns(t *testing.T) {
	rec := &recordingAttestor{}
	p := NewScanningResultProcessor(&fakeBlockScanner{needle: "SECRET"}, nil, nil, false, WithAttestor(rec))
	p.ScanResult(tenantCtx(), "s1", toolsCallReq(), twoBlockResult("first", secretValue))
	if len(rec.got) != 1 {
		t.Fatalf("attestations = %d, want 1", len(rec.got))
	}
	a := rec.got[0]
	if a.TenantID != attestTenant || a.Content != "first\n"+secretValue || a.Redacted || a.Output != "" ||
		len(a.Patterns) != 1 || a.Patterns[0] != "test" || a.Tier != TierExternal {
		t.Errorf("attestation = %+v", a)
	}
}

func TestScanAttest_CleanAndRedacted(t *testing.T) {
	rec := &recordingAttestor{}
	p := NewScanningResultProcessor(&fakeBlockScanner{needle: "SECRET"}, nil, nil, true, WithAttestor(rec))
	p.ScanResult(tenantCtx(), "s1", toolsCallReq(), &MCPResponse{Result: "nothing here"})
	p.ScanResult(tenantCtx(), "s1", toolsCallReq(), twoBlockResult("first", secretValue))
	if len(rec.got) != 2 {
		t.Fatalf("attestations = %d, want 2", len(rec.got))
	}
	if c := rec.got[0]; c.Content != "nothing here" || len(c.Patterns) != 0 || c.Redacted {
		t.Errorf("clean = %+v", c)
	}
	if r := rec.got[1]; !r.Redacted || r.Content != "first\n"+secretValue || r.Output != "first\na [X] value" {
		t.Errorf("redacted = %+v", r)
	}
}

// No tenant, no attestation: the gateway keys by tenant, and an
// unattributed caller must never seed another tenant's skip.
func TestScanAttest_RequiresAttributedTenant(t *testing.T) {
	rec := &recordingAttestor{}
	p := NewScanningResultProcessor(&fakeBlockScanner{needle: "SECRET"}, nil, nil, false, WithAttestor(rec))
	p.ScanResult(context.Background(), "s1", toolsCallReq(), &MCPResponse{Result: "x"})
	unattributed := identity.WithIdentity(context.Background(), &identity.Identity{Result: identity.ResultNoToken})
	p.ScanResult(unattributed, "s1", toolsCallReq(), &MCPResponse{Result: "x"})
	if len(rec.got) != 0 {
		t.Errorf("attested without a tenant: %+v", rec.got)
	}
}

// A result whose scan failed on any block is not vouched for.
func TestScanAttest_PartialScanNotAttested(t *testing.T) {
	rec := &recordingAttestor{}
	p := NewScanningResultProcessor(&fakeBlockScanner{needle: "SECRET", failOn: "boom"}, nil, nil, false, WithAttestor(rec))
	p.ScanResult(tenantCtx(), "s1", toolsCallReq(), twoBlockResult("boom", "fine"))
	p.ScanResult(tenantCtx(), "s1", toolsCallReq(), &MCPResponse{Result: "boom"})
	if len(rec.got) != 0 {
		t.Errorf("attested a partly scanned result: %+v", rec.got)
	}
}
