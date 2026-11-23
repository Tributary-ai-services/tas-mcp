package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

const (
	delegateToken = "dlg-0123456789abcdef0123456789abcdef-secret"
	orgA          = "org-aaaa"
	orgB          = "org-bbbb"
	userA         = "sub-alice"
	userNotMember = "sub-mallory"
	tenantUUID    = "6f1d2e3f-0000-4000-8000-0000000000aa"
)

// fakeDelegationVerifier plays aiqg-dashboard-be: orgA is linked to tenantA
// with userA a member; orgB is not linked. It counts calls and can fail.
type fakeDelegationVerifier struct {
	mu    sync.Mutex
	calls int
	down  bool
}

func (f *fakeDelegationVerifier) Verify(_ context.Context, org, tenant, user string) (*DelegationGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.down {
		return nil, ErrDelegationUnavailable
	}
	if org != orgA {
		return nil, &DelegationRefusal{Code: "org_not_linked"}
	}
	if tenant != "" && tenant != tenantA {
		return nil, &DelegationRefusal{Code: "tenant_mismatch"}
	}
	if user != userA {
		return nil, &DelegationRefusal{Code: "not_a_member"}
	}
	return &DelegationGrant{TenantID: tenantA, AetherOrgID: org, UserSub: user, Role: "member"}, nil
}

type delegEnv struct {
	*connEnv
	ver     *fakeDelegationVerifier
	handler http.Handler // the delegated listener
	main    http.Handler // the main listener
}

// newDelegEnv is newConnEnv plus the delegate credential, wired the way
// internal/http does: the main router wrapped by RefuseDelegateHeader, and
// the delegated listener's own handler.
func newDelegEnv(t *testing.T) *delegEnv {
	t.Helper()
	e := newConnEnv(t)
	ver := &fakeDelegationVerifier{}
	d, err := NewDelegation(delegateToken, "aether-be", adminToken, ver)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHTTPHandlers(e.m, e.log)
	h.SetIdentityResolver(&identity.Resolver{Validator: fakeValidator{tokenA: tenantA, tokenB: tenantB, tokenC: tenantC}})
	h.SetDelegation(d)
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.HandleFunc("/api/v1/events", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return &delegEnv{connEnv: e, ver: ver, handler: h.DelegatedHandler(), main: RefuseDelegateHeader(d, r)}
}

// doMain sends a request to the main listener.
func (e *delegEnv) doMain(t *testing.T, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.main.ServeHTTP(rec, req)
	return rec
}

// rawInvoke sends an invoke to the delegated listener with exact header values,
// duplicates included.
func (e *delegEnv) rawInvoke(t *testing.T, srv string, h http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/servers/"+srv+"/invoke", bytes.NewBufferString(callBody("query", nil)))
	req.Header = http.Header{}
	for k, v := range h {
		req.Header[http.CanonicalHeaderKey(k)] = v // as the wire would deliver them
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *delegEnv) do(t *testing.T, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	e.bodies = append(e.bodies, rec.Body.String())
	return rec
}

func (e *delegEnv) invoke(t *testing.T, srv, body string, hdr map[string]string) *httptest.ResponseRecorder {
	return e.do(t, http.MethodPost, "/api/v1/federation/servers/"+srv+"/invoke", body, hdr)
}

func delegated(org, user string) map[string]string {
	return map[string]string{DelegateTokenHeader: delegateToken, OnBehalfOfOrgHeader: org, OnBehalfOfUserHeader: user}
}

// A verified delegated call is attributed to the org's tenant, injects that
// tenant's connection, and its event says it was delegated and by whom.
func TestDelegation_VerifiedCallInjectsTenantConnectionAndRecordsDelegate(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	e.res.set(tenantB, srvPlat, headerConn("cb", secretB))

	rec := e.invoke(t, srvPlat, callBody("query", map[string]interface{}{"q": "x"}), delegated(orgA, userA))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if got := e.up.lastSeen(t).Auth; got != "Bearer "+secretA {
		t.Fatalf("upstream auth = %q, want tenant A's connection", got)
	}
	if len(e.obs.recs) == 0 {
		t.Fatal("no tool-call record")
	}
	id := e.obs.recs[len(e.obs.recs)-1].Identity
	if id == nil || id.Result != identity.ResultDelegated || id.TenantID != tenantA || id.UserID != userA || id.DelegatedBy != "aether-be" {
		t.Fatalf("identity = %+v", id)
	}
	// tools/list is also allowed; it is filtered like any tenant caller's.
	if rec := e.invoke(t, srvPlat, listBody, delegated(orgA, userA)); rec.Code != http.StatusOK {
		t.Fatalf("tools/list = %d", rec.Code)
	}
	// It may reach the org's own tenant server, as A's token may.
	if rec := e.invoke(t, srvA, callBody("query", nil), delegated(orgA, userA)); rec.Code != http.StatusOK {
		t.Fatalf("own tenant server = %d %s", rec.Code, rec.Body)
	}
}

// Refusals: none of them resolves a connection or reaches the upstream.
func TestDelegation_RefusalsInjectNothing(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	upBefore, resBefore := e.up.count(), e.res.calls

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"unlinked org", delegated(orgB, userA), http.StatusForbidden},
		{"user not a member of the org", delegated(orgA, userNotMember), http.StatusForbidden},
		{"org/tenant pair mismatched", func() map[string]string {
			h := delegated(orgA, userA)
			h[OnBehalfOfTenantHeader] = tenantUUID
			return h
		}(), http.StatusForbidden},
		{"wrong credential", map[string]string{DelegateTokenHeader: delegateToken + "x", OnBehalfOfOrgHeader: orgA, OnBehalfOfUserHeader: userA}, http.StatusForbidden},
		{"no org header", map[string]string{DelegateTokenHeader: delegateToken, OnBehalfOfUserHeader: userA}, http.StatusBadRequest},
		{"no user header", map[string]string{DelegateTokenHeader: delegateToken, OnBehalfOfOrgHeader: orgA}, http.StatusBadRequest},
		{"bad tenant header", func() map[string]string {
			h := delegated(orgA, userA)
			h[OnBehalfOfTenantHeader] = "not-a-uuid"
			return h
		}(), http.StatusBadRequest},
		{"with a TAS-Auth token too", func() map[string]string {
			h := delegated(orgA, userA)
			h["TAS-Auth"] = tokenB
			return h
		}(), http.StatusBadRequest},
		{"with the admin token too", func() map[string]string {
			h := delegated(orgA, userA)
			h[AdminTokenHeader] = adminToken
			return h
		}(), http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := e.invoke(t, srvPlat, callBody("query", nil), c.hdr); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	// A method other than tools/list and tools/call.
	if rec := e.invoke(t, srvPlat, `{"id":"1","method":"resources/read"}`, delegated(orgA, userA)); rec.Code != http.StatusForbidden {
		t.Errorf("resources/read = %d", rec.Code)
	}
	if e.up.count() != upBefore || e.res.calls != resBefore {
		t.Fatalf("a refused delegated call reached upstream (%d) or resolved a connection (%d)", e.up.count()-upBefore, e.res.calls-resBefore)
	}
	// A refusal never names the other tenant.
	for _, b := range e.bodies {
		if strings.Contains(b, tenantA) || strings.Contains(b, tenantB) {
			t.Fatalf("refusal body names a tenant: %q", b)
		}
	}
}

// MEDIUM-2: the main listener refuses the credential on every route, invoke
// included; delegated calls exist only on the delegated listener, which in
// turn serves only invoke, and only with the credential.
func TestDelegation_MainListenerRefusesCredential(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	hdr := delegated(orgA, userA)
	srvJSON := `{"id":"new","name":"x","endpoint":"http://x.tas-mcp-servers.svc:80","protocol":"http"}`
	up := e.up.count()
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/federation/servers/" + srvPlat + "/invoke", callBody("query", nil)},
		{http.MethodPost, "/api/v1/federation/servers", srvJSON},
		{http.MethodPut, "/api/v1/federation/servers/" + srvPlat, srvJSON},
		{http.MethodDelete, "/api/v1/federation/servers/" + srvPlat, ""},
		{http.MethodGet, "/api/v1/federation/servers", ""},
		{http.MethodGet, "/api/v1/federation/servers/" + srvPlat, ""},
		{http.MethodPost, "/api/v1/federation/broadcast", `{"id":"1","method":"tools/list"}`},
		{http.MethodGet, "/api/v1/federation/health", ""},
		{http.MethodPost, "/api/v1/events", `{}`},
	} {
		if rec := e.doMain(t, c.method, c.path, c.body, hdr); rec.Code != http.StatusForbidden {
			t.Errorf("main %s %s with the delegate credential = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	// An empty credential header still counts as present.
	if rec := e.doMain(t, http.MethodPost, "/api/v1/federation/servers/"+srvPlat+"/invoke", callBody("query", nil),
		map[string]string{DelegateTokenHeader: ""}); rec.Code != http.StatusForbidden {
		t.Errorf("main with an empty delegate header = %d", rec.Code)
	}
	both := delegated(orgA, userA)
	both[AdminTokenHeader] = adminToken
	if rec := e.doMain(t, http.MethodDelete, "/api/v1/federation/servers/"+srvPlat, "", both); rec.Code != http.StatusForbidden {
		t.Errorf("delete with admin + delegate = %d", rec.Code)
	}
	if _, err := e.m.GetServer(srvPlat); err != nil {
		t.Fatal("the platform server was deleted")
	}
	if e.up.count() != up {
		t.Fatal("a main-listener delegated call reached the upstream")
	}
	// The delegated listener: only invoke, only with the credential.
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/v1/federation/servers", http.StatusNotFound},
		{http.MethodPost, "/api/v1/federation/servers", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/federation/servers/" + srvPlat, http.StatusNotFound},
		{http.MethodPost, "/api/v1/federation/broadcast", http.StatusNotFound},
		{http.MethodGet, "/api/v1/federation/servers/" + srvPlat + "/invoke", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/v1/events", http.StatusNotFound},
		{http.MethodGet, "/health", http.StatusNotFound},
	} {
		if rec := e.do(t, c.method, c.path, `{}`, hdr); rec.Code != c.want {
			t.Errorf("delegated %s %s = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
	// Without the credential the delegated listener serves nothing, and a
	// TAS-Auth caller is not served there either.
	if rec := e.invoke(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusForbidden {
		t.Errorf("delegated listener with TAS-Auth only = %d", rec.Code)
	}
	if rec := e.invoke(t, srvPlat, callBody("query", nil), nil); rec.Code != http.StatusForbidden {
		t.Errorf("delegated listener with nothing = %d", rec.Code)
	}
}

// INFO: presence counts, not the first value. Repeated delegation headers,
// and an empty TAS-Auth or admin header next to the credential, are refused.
func TestDelegation_DuplicateAndEmptyHeaders(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	base := func() http.Header {
		return http.Header{DelegateTokenHeader: {delegateToken}, OnBehalfOfOrgHeader: {orgA}, OnBehalfOfUserHeader: {userA}}
	}
	if rec := e.rawInvoke(t, srvPlat, base()); rec.Code != http.StatusOK {
		t.Fatalf("baseline = %d %s", rec.Code, rec.Body)
	}
	up := e.up.count()
	cases := map[string]func(http.Header){
		"token empty-first":        func(h http.Header) { h[DelegateTokenHeader] = []string{"", delegateToken} },
		"token twice":              func(h http.Header) { h[DelegateTokenHeader] = []string{delegateToken, delegateToken} },
		"org twice":                func(h http.Header) { h[OnBehalfOfOrgHeader] = []string{orgA, orgB} },
		"user twice":               func(h http.Header) { h[OnBehalfOfUserHeader] = []string{userA, userA} },
		"tenant twice":             func(h http.Header) { h[OnBehalfOfTenantHeader] = []string{tenantUUID, tenantUUID} },
		"expected twice":           func(h http.Header) { h[ExpectedConnectionHeader] = []string{"ca", "ca"} },
		"empty TAS-Auth":           func(h http.Header) { h["Tas-Auth"] = []string{""} },
		"TAS-Auth empty-first":     func(h http.Header) { h["Tas-Auth"] = []string{"", tokenB} },
		"empty admin token header": func(h http.Header) { h[AdminTokenHeader] = []string{""} },
		"user with a comma":        func(h http.Header) { h[OnBehalfOfUserHeader] = []string{userA + ",sub-mallory"} },
		"whitespace org":           func(h http.Header) { h[OnBehalfOfOrgHeader] = []string{"   "} },
	}
	for name, mutate := range cases {
		h := base()
		mutate(h)
		if rec := e.rawInvoke(t, srvPlat, h); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, rec.Code)
		}
	}
	if e.up.count() != up {
		t.Fatal("an ambiguous delegated call reached the upstream")
	}
}

// LOW-2: a delegated call that names its bound connection is refused (409)
// when the tenant's stored connection is a different one, or none.
func TestDelegation_ExpectedConnectionMismatchIs409(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	with := func(expected string) map[string]string {
		h := delegated(orgA, userA)
		h[ExpectedConnectionHeader] = expected
		return h
	}
	if rec := e.invoke(t, srvPlat, callBody("query", nil), with("ca")); rec.Code != http.StatusOK {
		t.Fatalf("matching connection = %d %s", rec.Code, rec.Body)
	}
	up := e.up.count()
	if rec := e.invoke(t, srvPlat, callBody("query", nil), with("replaced-in-aiqg")); rec.Code != http.StatusConflict {
		t.Fatalf("other connection = %d %s", rec.Code, rec.Body)
	}
	e.res.set(tenantA, srvPlat, nil)
	if rec := e.invoke(t, srvPlat, callBody("query", nil), with("ca")); rec.Code != http.StatusConflict {
		t.Fatalf("no connection = %d %s", rec.Code, rec.Body)
	}
	if e.up.count() != up {
		t.Fatal("a mismatched call reached the upstream")
	}
}

// The delegate credential is not a TAS-Auth token: sent there (on the main
// listener), it is malformed, unattributed, gets no connection and cannot
// reach a tenant server. The same shape rule is why the LLM gateway refuses it.
func TestDelegation_NotAcceptedAsTASAuth(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	if rec := e.doMain(t, http.MethodPost, "/api/v1/federation/servers/"+srvPlat+"/invoke", callBody("query", nil), map[string]string{"TAS-Auth": delegateToken}); rec.Code != http.StatusOK {
		t.Fatalf("platform server = %d", rec.Code)
	}
	if got := e.up.lastSeen(t).Auth; got != "" {
		t.Fatalf("delegate token as TAS-Auth injected %q", got)
	}
	if rec := e.doMain(t, http.MethodPost, "/api/v1/federation/servers/"+srvA+"/invoke", callBody("query", nil), map[string]string{"TAS-Auth": delegateToken}); rec.Code != http.StatusForbidden {
		t.Fatalf("tenant server = %d", rec.Code)
	}
	if _, err := NewDelegation("tas_qg_live_"+strings.Repeat("x", 40), "d", "", &fakeDelegationVerifier{}); err == nil {
		t.Fatal("a delegate token shaped like a gateway token must be refused at startup")
	}
	if _, err := NewDelegation("short", "d", "", &fakeDelegationVerifier{}); err == nil {
		t.Fatal("a short delegate token must be refused")
	}
	if _, err := NewDelegation(delegateToken, "d", delegateToken, &fakeDelegationVerifier{}); err == nil {
		t.Fatal("a delegate token equal to the admin token must be refused")
	}
}

// An AIQG outage fails closed (503), injects nothing, and is not cached.
func TestDelegation_VerifyOutageFailsClosed(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	e.ver.down = true
	up := e.up.count()
	if rec := e.invoke(t, srvPlat, callBody("query", nil), delegated(orgA, userA)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("outage = %d", rec.Code)
	}
	if e.up.count() != up {
		t.Fatal("an unverified delegated call reached the upstream")
	}
	e.ver.down = false
	if rec := e.invoke(t, srvPlat, callBody("query", nil), delegated(orgA, userA)); rec.Code != http.StatusOK {
		t.Fatalf("after recovery = %d", rec.Code)
	}
}

// With delegation off, the delegate headers mean nothing: the call is what it
// was before (unattributed on a platform server, refused on a tenant server).
func TestDelegation_OffIsUnchanged(t *testing.T) {
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	if rec := e.call(t, srvPlat, callBody("query", nil), delegated(orgA, userA)); rec.Code != http.StatusOK {
		t.Fatalf("platform = %d", rec.Code)
	}
	if got := e.up.lastSeen(t).Auth; got != "" {
		t.Fatalf("delegation off injected %q", got)
	}
	if rec := e.call(t, srvA, callBody("query", nil), delegated(orgA, userA)); rec.Code != http.StatusForbidden {
		t.Fatalf("tenant server = %d", rec.Code)
	}
	if RefuseDelegateHeader(nil, http.NotFoundHandler()) == nil {
		t.Fatal("nil delegation must pass through")
	}
	if NewHTTPHandlers(e.m, e.log).DelegatedHandler() != nil {
		t.Fatal("no delegated listener without delegation")
	}
}

// The credential never reaches a log line or a response.
func TestDelegation_CredentialNeverLogged(t *testing.T) {
	e := newDelegEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	_ = e.invoke(t, srvPlat, callBody("query", nil), delegated(orgA, userA))
	_ = e.invoke(t, srvPlat, callBody("query", nil), delegated(orgB, userA))
	if rec := e.invoke(t, srvPlat, callBody("query", nil), delegated(orgA, userNotMember)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-member = %d", rec.Code)
	}
	bad := delegated(orgA, userA)
	bad[DelegateTokenHeader] = delegateToken + "-wrong"
	_ = e.invoke(t, srvPlat, callBody("query", nil), bad)
	e.ver.down = true
	_ = e.invoke(t, srvPlat, callBody("query", nil), delegated(orgA, userA))
	_ = e.doMain(t, http.MethodGet, "/api/v1/federation/servers", "", delegated(orgA, userA))

	for _, entry := range e.logs.All() {
		b, _ := json.Marshal(entry.ContextMap())
		line := entry.Message + string(b)
		if strings.Contains(line, "dlg-0123456789") {
			t.Fatalf("log line carries the delegate credential: %s", line)
		}
		// MEDIUM-2: a refused user's sub is logged only as a hash.
		if strings.Contains(line, userNotMember) {
			t.Fatalf("log line carries a refused sub: %s", line)
		}
	}
	for _, b := range e.bodies {
		if strings.Contains(b, "dlg-0123456789") {
			t.Fatalf("response carries the delegate credential: %s", b)
		}
	}
}

// The verify cache keeps answers (allowed and refused) for its TTL and never
// keeps an outage.
func TestCachingDelegationVerifier(t *testing.T) {
	inner := &fakeDelegationVerifier{}
	now := time.Unix(0, 0)
	c := NewCachingDelegationVerifier(inner, time.Minute)
	c.Now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.Verify(ctx, orgA, "", userA); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Verify(ctx, orgB, "", userA); !errors.Is(err, ErrDelegationRefused) {
			t.Fatal(err)
		}
	}
	if inner.calls != 2 {
		t.Fatalf("inner calls = %d, want 2 (one per key)", inner.calls)
	}
	now = now.Add(61 * time.Second)
	inner.down = true
	if _, err := c.Verify(ctx, orgA, "", userA); !errors.Is(err, ErrDelegationUnavailable) {
		t.Fatalf("expired + outage = %v", err)
	}
	inner.down = false
	if _, err := c.Verify(ctx, orgA, "", userA); err != nil {
		t.Fatal("an outage was cached")
	}
}

// The dashboard verifier's wire contract.
func TestDashboardDelegationVerifier(t *testing.T) {
	var gotAuth string
	var gotBody delegationVerifyBody
	status, reply := http.StatusOK, `{"tenant_id":"`+tenantUUID+`","aether_org_id":"`+orgA+`","user_sub":"`+userA+`","role":"member"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Internal-Auth")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		if r.URL.Path != delegationVerifyPath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	v, err := NewDashboardDelegationVerifier(srv.URL, "verify-token")
	if err != nil {
		t.Fatal(err)
	}
	g, err := v.Verify(context.Background(), orgA, "", userA)
	if err != nil || g.TenantID != tenantUUID || gotAuth != "verify-token" || gotBody.AetherOrgID != orgA || gotBody.UserSub != userA {
		t.Fatalf("grant=%+v err=%v auth=%q body=%+v", g, err, gotAuth, gotBody)
	}
	// AIQG answered for another tenant than the one named: refused.
	if _, err := v.Verify(context.Background(), orgA, "6f1d2e3f-0000-4000-8000-0000000000bb", userA); !errors.Is(err, ErrDelegationRefused) {
		t.Fatalf("mismatch = %v", err)
	}
	// An answer that does not echo the question is not trusted.
	reply = `{"tenant_id":"` + tenantUUID + `","aether_org_id":"other","user_sub":"` + userA + `"}`
	if _, err := v.Verify(context.Background(), orgA, "", userA); !errors.Is(err, ErrDelegationUnavailable) {
		t.Fatalf("non-echo = %v", err)
	}
	status, reply = http.StatusForbidden, `{"error":{"code":"not_a_member"}}`
	var ref *DelegationRefusal
	if _, err := v.Verify(context.Background(), orgA, "", userA); !errors.As(err, &ref) || ref.Code != "not_a_member" {
		t.Fatalf("403 = %v", err)
	}
	// A redirect is never followed: the target would receive Internal-Auth.
	var hitTarget bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitTarget = true
		_, _ = w.Write([]byte(`{"tenant_id":"` + tenantUUID + `","aether_org_id":"` + orgA + `","user_sub":"` + userA + `"}`))
	}))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redir.Close()
	rv, _ := NewDashboardDelegationVerifier(redir.URL, "verify-token")
	if _, err := rv.Verify(context.Background(), orgA, "", userA); !errors.Is(err, ErrDelegationUnavailable) || hitTarget {
		t.Fatalf("redirect = %v, target hit = %v", err, hitTarget)
	}
	for _, st := range []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusBadGateway} {
		status = st
		if _, err := v.Verify(context.Background(), orgA, "", userA); !errors.Is(err, ErrDelegationUnavailable) {
			t.Fatalf("%d = %v", st, err)
		}
	}
}
