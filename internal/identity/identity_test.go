package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const goodBearer = "tas_qg_live_abc123"

// fakeDashboard serves /internal/auth/validate with a fixed status and body and
// counts calls. It asserts the request shape tas-llm-router sends.
func fakeDashboard(t *testing.T, status int, body string, calls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if r.Method != http.MethodPost || r.URL.Path != "/internal/auth/validate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Internal-Auth"); got != "internal-secret" {
			t.Errorf("Internal-Auth = %q", got)
		}
		var req struct {
			Bearer string `json:"bearer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Bearer == "" {
			t.Errorf("bad body: %v %+v", err, req)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

const okBody = `{"token_id":"tok-1","tenant_id":"ten-1","aiqg_account_id":"acct-1","source_app":"claude-code","suspended":false,"label":"laptop","owner_sub":"kc-sub-1"}`

func newResolver(t *testing.T, url string) *Resolver {
	t.Helper()
	v, err := NewDashboardValidator(url, "internal-secret")
	if err != nil {
		t.Fatal(err)
	}
	return &Resolver{Validator: NewCachingValidator(v, time.Minute)}
}

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestResolve_Attributed(t *testing.T) {
	var calls int32
	srv := fakeDashboard(t, http.StatusOK, okBody, &calls)
	defer srv.Close()
	r := newResolver(t, srv.URL)

	id := r.Resolve(context.Background(), hdr(
		"TAS-Auth", goodBearer,
		"baggage", "user.id=u%2042,session.id=s-9;prop=1,bad",
		"TAS-Agent-Id", "agent-7",
		"TAS-Flow-Id", "flow-1",
		"TAS-Step-Id", "step-2",
		"TAS-Tool-Call-Id", "toolu_01",
	))
	want := Identity{
		Attributed: true, Result: ResultAttributed,
		TenantID: "ten-1", TokenID: "tok-1", AIQGAccountID: "acct-1", OwnerSub: "kc-sub-1",
		Label: "laptop", SourceApp: "claude-code", UserID: "u 42", SessionID: "s-9",
		AgentID: "agent-7", FlowID: "flow-1", StepID: "step-2", ToolCallID: "toolu_01",
	}
	if *id != want {
		t.Fatalf("identity\n got %+v\nwant %+v", *id, want)
	}
}

func TestResolve_SourceAppOverrideAndAgentFallback(t *testing.T) {
	var calls int32
	srv := fakeDashboard(t, http.StatusOK, okBody, &calls)
	defer srv.Close()
	r := newResolver(t, srv.URL)
	id := r.Resolve(context.Background(), hdr("TAS-Auth", goodBearer, "TAS-Source-App", "cursor", "gen_ai.agent.id", "otel-agent"))
	if id.SourceApp != "cursor" || id.AgentID != "otel-agent" {
		t.Fatalf("got source_app=%q agent_id=%q", id.SourceApp, id.AgentID)
	}
}

func TestResolve_ObserveModeUnattributed(t *testing.T) {
	var calls int32
	srv := fakeDashboard(t, http.StatusUnauthorized, `{"error":{"code":"token_unknown"}}`, &calls)
	defer srv.Close()
	r := newResolver(t, srv.URL)

	cases := []struct {
		name string
		h    http.Header
		want string
	}{
		{"no token", hdr("baggage", "user.id=u1", "TAS-Agent-Id", "a1"), ResultNoToken},
		{"malformed", hdr("TAS-Auth", "sk-not-ours"), ResultMalformed},
		{"unknown", hdr("TAS-Auth", goodBearer, "baggage", "user.id=u1"), ResultUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := r.Resolve(context.Background(), c.h)
			if id.Attributed || id.Result != c.want {
				t.Fatalf("got attributed=%v result=%q, want %q", id.Attributed, id.Result, c.want)
			}
			// Unattributed: self-asserted identity is NOT carried.
			if id.TenantID != "" || id.UserID != "" || id.AgentID != "" || id.SourceApp != "" {
				t.Fatalf("unattributed identity carries fields: %+v", *id)
			}
		})
	}
	if calls != 1 {
		t.Fatalf("validator calls = %d, want 1 (only the well-formed token)", calls)
	}
}

func TestResolve_UnattributedKeepsToolCallID(t *testing.T) {
	id := (&Resolver{}).Resolve(context.Background(), hdr("TAS-Tool-Call-Id", "call_9"))
	if id.ToolCallID != "call_9" || id.Result != ResultNoToken {
		t.Fatalf("got %+v", *id)
	}
}

func TestResolve_Suspended(t *testing.T) {
	var calls int32
	srv := fakeDashboard(t, http.StatusForbidden,
		`{"error":{"code":"account_suspended"},"token_id":"tok-1","tenant_id":"ten-1","aiqg_account_id":"acct-1"}`, &calls)
	defer srv.Close()
	id := newResolver(t, srv.URL).Resolve(context.Background(), hdr("TAS-Auth", goodBearer))
	if !id.Attributed || id.Result != ResultSuspended || !id.Suspended || id.TenantID != "ten-1" {
		t.Fatalf("got %+v", *id)
	}
}

func TestResolve_ValidatorDownFailsOpen(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused from here on
	id := newResolver(t, url).Resolve(context.Background(), hdr("TAS-Auth", goodBearer))
	if id.Attributed || id.Result != ResultValidatorUnavailable {
		t.Fatalf("got %+v", *id)
	}
}

func TestResolve_Validator5xxFailsOpen(t *testing.T) {
	var calls int32
	srv := fakeDashboard(t, http.StatusInternalServerError, `{}`, &calls)
	defer srv.Close()
	id := newResolver(t, srv.URL).Resolve(context.Background(), hdr("TAS-Auth", goodBearer))
	if id.Result != ResultValidatorUnavailable {
		t.Fatalf("got %q", id.Result)
	}
}

func TestResolve_Unconfigured(t *testing.T) {
	id := (&Resolver{}).Resolve(context.Background(), hdr("TAS-Auth", goodBearer))
	if id.Attributed || id.Result != ResultValidatorUnconfigured {
		t.Fatalf("got %+v", *id)
	}
}

type countingValidator struct {
	calls int
	tok   *Token
	err   error
}

func (c *countingValidator) Validate(context.Context, string) (*Token, error) {
	c.calls++
	return c.tok, c.err
}

func TestCache_HitWithinTTLAndExpiry(t *testing.T) {
	inner := &countingValidator{tok: &Token{TenantID: "t"}}
	now := time.Unix(1000, 0)
	c := NewCachingValidator(inner, 60*time.Second)
	c.Now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if tok, err := c.Validate(context.Background(), goodBearer); err != nil || tok.TenantID != "t" {
			t.Fatalf("validate: %v %v", tok, err)
		}
	}
	if inner.calls != 1 {
		t.Fatalf("calls within TTL = %d, want 1", inner.calls)
	}
	now = now.Add(61 * time.Second)
	_, _ = c.Validate(context.Background(), goodBearer)
	if inner.calls != 2 {
		t.Fatalf("calls after TTL = %d, want 2", inner.calls)
	}
	// A different bearer is a different key.
	_, _ = c.Validate(context.Background(), goodBearer+"x")
	if inner.calls != 3 {
		t.Fatalf("calls for new bearer = %d, want 3", inner.calls)
	}
}

func TestCache_CachesUnknownButNotOutage(t *testing.T) {
	unknown := &countingValidator{err: ErrTokenNotFound}
	c := NewCachingValidator(unknown, time.Minute)
	_, _ = c.Validate(context.Background(), goodBearer)
	_, err := c.Validate(context.Background(), goodBearer)
	if !errors.Is(err, ErrTokenNotFound) || unknown.calls != 1 {
		t.Fatalf("unknown not cached: calls=%d err=%v", unknown.calls, err)
	}

	down := &countingValidator{err: errors.New("dial tcp: connection refused")}
	c = NewCachingValidator(down, time.Minute)
	_, _ = c.Validate(context.Background(), goodBearer)
	_, _ = c.Validate(context.Background(), goodBearer)
	if down.calls != 2 {
		t.Fatalf("outage was cached: calls=%d", down.calls)
	}
}

func TestCache_Bounded(t *testing.T) {
	inner := &countingValidator{tok: &Token{}}
	c := NewCachingValidator(inner, time.Minute)
	c.Max = 4
	for i := 0; i < 20; i++ {
		_, _ = c.Validate(context.Background(), goodBearer+string(rune('a'+i)))
	}
	if len(c.entries) > 4 {
		t.Fatalf("cache grew to %d entries, max 4", len(c.entries))
	}
}

func TestValidator_InternalAuthRejectedIsNotUnknown(t *testing.T) {
	var calls int32
	srv := fakeDashboard(t, http.StatusUnauthorized, `{"error":{"code":"internal_auth_invalid"}}`, &calls)
	defer srv.Close()
	v, _ := NewDashboardValidator(srv.URL, "internal-secret")
	_, err := v.Validate(context.Background(), goodBearer)
	if err == nil || errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("want a misconfig error, got %v", err)
	}
}

func TestMiddleware_StoresIdentityAndNeverRejects(t *testing.T) {
	r := &Resolver{}
	var got *Identity
	h := r.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = FromContext(req.Context())
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", http.NoBody))
	if rec.Code != http.StatusTeapot || got == nil || got.Result != ResultNoToken {
		t.Fatalf("code=%d identity=%+v", rec.Code, got)
	}
	if FromContext(context.Background()) != nil {
		t.Fatal("empty context should carry no identity")
	}
}

func TestNewDashboardValidator_RequiresConfig(t *testing.T) {
	if _, err := NewDashboardValidator("", "x"); err == nil {
		t.Fatal("want error for empty URL")
	}
	if _, err := NewDashboardValidator("http://x", ""); err == nil {
		t.Fatal("want error for empty internal token")
	}
}
