package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
)

type noVerify struct{}

func (noVerify) Verify(context.Context, string, string, string) (*federation.DelegationGrant, error) {
	return nil, federation.ErrDelegationUnavailable
}

// AIQG-88: through the real Handler, the delegate credential is refused on
// every non-federation route too (events, forwarding, metrics, the legacy
// /mcp endpoint), and without delegation those routes behave as before.
func TestHandler_DelegateCredentialRefusedOutsideInvoke(t *testing.T) {
	log := zap.NewNop()
	d, err := federation.NewDelegation(strings.Repeat("d", 40), "aether-be", "", noVerify{})
	if err != nil {
		t.Fatal(err)
	}
	on := NewServer(log, grpcserver.NewMCPServer(log, nil), nil)
	on.SetDelegation(d)
	off := NewServer(log, grpcserver.NewMCPServer(log, nil), nil)

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/events"},
		{http.MethodGet, "/api/v1/forwarding/targets"},
		{http.MethodPost, "/api/v1/forwarding/targets"},
		{http.MethodGet, "/api/v1/stats"},
		{http.MethodPost, "/mcp"},
		{http.MethodGet, "/health"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		req.Header.Set(federation.DelegateTokenHeader, strings.Repeat("d", 40))
		rec := httptest.NewRecorder()
		on.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with delegation on = %d, want 403", c.method, c.path, rec.Code)
		}
		rec = httptest.NewRecorder()
		off.Handler().ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s %s with delegation off = 403; behavior must be unchanged", c.method, c.path)
		}
	}
}
