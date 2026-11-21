package federation

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

// identityCapturingManager records the identity the invoke handler put in ctx.
type identityCapturingManager struct {
	TASManager
	got *identity.Identity
}

func (m *identityCapturingManager) InvokeServer(ctx context.Context, _ string, req *MCPRequest) (*MCPResponse, error) {
	m.got = identity.FromContext(ctx)
	return &MCPResponse{ID: req.ID, Result: "ok"}, nil
}

func invoke(t *testing.T, h *HTTPHandlers, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/servers/s1/invoke",
		bytes.NewBufferString(`{"id":"1","method":"tools/call","params":{"name":"t"}}`))
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// Observe mode: a call with no token is served, and carries an unattributed
// identity.
func TestInvokeHandler_NoTokenServedUnattributed(t *testing.T) {
	m := &identityCapturingManager{}
	h := NewHTTPHandlers(m, zap.NewNop())
	h.SetIdentityResolver(&identity.Resolver{})
	rec := invoke(t, h, http.Header{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (observe mode never refuses)", rec.Code)
	}
	if m.got == nil || m.got.Attributed || m.got.Result != identity.ResultNoToken {
		t.Fatalf("identity = %+v", m.got)
	}
}

// Identity disabled: no resolution at all, ctx carries nothing.
func TestInvokeHandler_IdentityDisabled(t *testing.T) {
	m := &identityCapturingManager{}
	h := NewHTTPHandlers(m, zap.NewNop())
	rec := invoke(t, h, http.Header{"Tas-Auth": {"tas_qg_live_x"}})
	if rec.Code != http.StatusOK || m.got != nil {
		t.Fatalf("status=%d identity=%+v", rec.Code, m.got)
	}
}
