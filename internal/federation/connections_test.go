package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

// Plan #22 C.4: per-tenant connection injection.

const (
	tenantC      = "tenant-c" // a tenant with no connection anywhere
	tokenC       = "tas_qg_live_cccc"
	secretA      = "sk-live-AAAA-tenant-a-key-0001"
	secretB      = "sk-live-BBBB-tenant-b-key-0002"
	secretASrvA  = "sk-live-AAAA-own-crm-key-0003"
	dsnPassword  = "Pw-d0nt-leak-me!"
	dsnA         = "postgres://alice:" + dsnPassword + "@db.tenant-a.svc:6543/sales?sslmode=require"
	oauthSecret  = "oauth-client-secret-9876543210"
	oauthTokenV1 = "at-v1-access-token-abcdef"
	oldSecret    = "old-secret-1111"
	newSecret    = "new-secret-2222"
	listBody     = `{"id":"1","method":"tools/list"}`
)

// fakeResolver maps (tenant, server) to a connection and counts calls. It is
// safe for concurrent use and its answers can be changed mid-test.
type fakeResolver struct {
	mu    sync.Mutex
	conns map[string]*Connection
	err   error
	calls int
}

func (f *fakeResolver) set(tenant, server string, c *Connection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conns == nil {
		f.conns = map[string]*Connection{}
	}
	f.conns[connKey(tenant, server)] = c
}

func (f *fakeResolver) Resolve(_ context.Context, tenant, server string) (*Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	c := f.conns[connKey(tenant, server)]
	if c == nil {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

// seen is what the echo upstream received on one call.
type seen struct {
	Auth   string
	APIKey string
	Args   map[string]interface{}
}

// echoUpstream is an MCP server that records what it received and echoes it
// back, in the result on success and in a 500 body for the tool "fail", the
// way a careless server would. The proxy must still never show it.
type echoUpstream struct {
	srv  *httptest.Server
	mu   sync.Mutex
	last []seen
}

func newEchoUpstream(t *testing.T) *echoUpstream {
	t.Helper()
	e := &echoUpstream{}
	m := http.NewServeMux()
	record := func(r *http.Request, args map[string]interface{}) seen {
		s := seen{Auth: r.Header.Get("Authorization"), APIKey: r.Header.Get("X-Api-Key"), Args: args}
		e.mu.Lock()
		e.last = append(e.last, s)
		e.mu.Unlock()
		return s
	}
	m.HandleFunc("/mcp/tools/list", func(w http.ResponseWriter, r *http.Request) {
		s := record(r, nil)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"tools":[{"name":"query","description":"auth was %s"}]}`, s.Auth)
	})
	m.HandleFunc("/mcp/tools/call", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s := record(r, body.Arguments)
		echo, _ := json.Marshal(s)
		if body.Name == "fail" {
			http.Error(w, "upstream exploded with "+string(echo), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		text, _ := json.Marshal(string(echo))
		_, _ = fmt.Fprintf(w, `{"content":[{"type":"text","text":%s}],"isError":false}`, text)
	})
	e.srv = httptest.NewServer(m)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *echoUpstream) lastSeen(t *testing.T) seen {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.last) == 0 {
		t.Fatal("upstream was not called")
	}
	return e.last[len(e.last)-1]
}

func (e *echoUpstream) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.last)
}

// recordingScanner sees what the boundary scanner is handed.
type recordingScanner struct {
	mu   sync.Mutex
	seen []string
}

func (s *recordingScanner) ScanResult(_ context.Context, _ string, req *MCPRequest, resp *MCPResponse) *MCPResponse {
	b, _ := json.Marshal(struct {
		Req  *MCPRequest
		Resp *MCPResponse
	}{req, resp})
	s.mu.Lock()
	s.seen = append(s.seen, string(b))
	s.mu.Unlock()
	return resp
}

type connEnv struct {
	m      *Manager
	router *mux.Router
	up     *echoUpstream
	res    *fakeResolver
	obs    *captureObserver
	scan   *recordingScanner
	logs   *observer.ObservedLogs
	bodies []string
}

// newConnEnv builds a manager with a platform server and tenant A's server on
// one echo upstream, the tenant registry on, and injection on through res.
func newConnEnv(t *testing.T) *connEnv {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	up := newEchoUpstream(t)
	m := NewManager(log, &MockServiceDiscovery{}, &MockProtocolBridge{})
	m.SetTenantPolicy(onPolicy())
	for _, s := range []*MCPServer{
		{ID: srvPlat, Name: "Platform"},
		{ID: srvA, Name: "A CRM", TenantID: tenantA, ManagedBy: ManagedByAIQG},
	} {
		s.Endpoint, s.Protocol, s.Auth = up.srv.URL, ProtocolHTTP, AuthConfig{Type: AuthNone}
		if err := m.registry.Put(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	res := &fakeResolver{}
	m.SetConnectionInjector(&ConnectionInjector{Resolver: res, Policy: m.TenantPolicy()})
	obs := &captureObserver{}
	m.SetToolCallObserver(obs)
	sc := &recordingScanner{}
	m.SetScanner(sc)

	h := NewHTTPHandlers(m, log)
	h.SetIdentityResolver(&identity.Resolver{Logger: log,
		Validator: fakeValidator{tokenA: tenantA, tokenB: tenantB, tokenC: tenantC}})
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	return &connEnv{m: m, router: r, up: up, res: res, obs: obs, scan: sc, logs: logs}
}

func (e *connEnv) call(t *testing.T, srv, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/servers/"+srv+"/invoke", bytes.NewBufferString(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	e.bodies = append(e.bodies, rec.Body.String())
	return rec
}

func callBody(tool string, args map[string]interface{}) string {
	b, _ := json.Marshal(map[string]interface{}{"id": "1", "method": "tools/call",
		"params": map[string]interface{}{"name": tool, "arguments": args}})
	return string(b)
}

func headerConn(id, secret string) *Connection {
	return &Connection{ID: id, Kind: ConnKindHeader, Version: "1", Secret: secret,
		Params: map[string]string{"prefix": "Bearer "}}
}

// Two tenants on one platform server each get their own connection; a tenant
// with none, and an unattributed caller, get nothing — never another
// tenant's. Connections are per (tenant, server): A's platform connection is
// not used on A's own server.
func TestConnections_TwoTenantsEachOwnConnection(t *testing.T) {
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("ca", secretA))
	e.res.set(tenantB, srvPlat, headerConn("cb", secretB))
	e.res.set(tenantA, srvA, &Connection{ID: "ca2", Kind: ConnKindHeader, Version: "1", Secret: secretASrvA,
		Params: map[string]string{"header": "X-Api-Key"}})

	cases := []struct {
		srv, token, wantAuth, wantKey string
	}{
		{srvPlat, tokenA, "Bearer " + secretA, ""},
		{srvPlat, tokenB, "Bearer " + secretB, ""},
		{srvPlat, tokenC, "", ""},
		{srvPlat, "", "", ""},
		{srvPlat, "tas_qg_live_unknown", "", ""},
		{srvA, tokenA, "", secretASrvA},
		{srvPlat, tokenA, "Bearer " + secretA, ""}, // again, after B
	}
	for _, c := range cases {
		rec := e.call(t, c.srv, callBody("query", map[string]interface{}{"q": "x"}), as(c.token))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s as %q: %d %s", c.srv, c.token, rec.Code, rec.Body)
		}
		got := e.up.lastSeen(t)
		if got.Auth != c.wantAuth || got.APIKey != c.wantKey {
			t.Fatalf("%s as %q: upstream got auth=%q key=%q, want %q/%q", c.srv, c.token, got.Auth, got.APIKey, c.wantAuth, c.wantKey)
		}
	}
	// B's call to A's tenant server is refused before anything is resolved.
	before := e.res.calls
	if rec := e.call(t, srvA, callBody("query", nil), as(tokenB)); rec.Code != http.StatusForbidden {
		t.Fatalf("B on A's server = %d", rec.Code)
	}
	if e.res.calls != before {
		t.Fatal("a refused cross-tenant call resolved a connection")
	}
}

// A database DSN goes into the tools/call arguments in aether-be's
// connectionInfo shape, replacing a caller-supplied value; the caller's
// request is not modified, and tools/list carries no DSN.
func TestConnections_DatabaseDSNArgument(t *testing.T) {
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, &Connection{ID: "db", Kind: ConnKindDatabaseDSN, Version: "1", Secret: dsnA})

	rec := e.call(t, srvPlat, callBody("query", map[string]interface{}{"sql": "select 1",
		"_connection": map[string]interface{}{"host": "attacker.example"}}), as(tokenA))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := e.up.lastSeen(t)
	conn, _ := got.Args["_connection"].(map[string]interface{})
	want := map[string]interface{}{"host": "db.tenant-a.svc", "port": float64(6543), "username": "alice",
		"password": dsnPassword, "database": "sales", "protocol": "postgres", "ssl_mode": "require"}
	for k, v := range want {
		if conn[k] != v {
			t.Fatalf("_connection[%s] = %v, want %v (all: %v)", k, conn[k], v, conn)
		}
	}
	if got.Args["sql"] != "select 1" {
		t.Fatalf("caller arguments lost: %v", got.Args)
	}

	// The string format and a custom argument name.
	e.res.set(tenantB, srvPlat, &Connection{ID: "db2", Kind: ConnKindDatabaseDSN, Version: "1", Secret: dsnA,
		Params: map[string]string{"argument": "dsn", "format": "string"}})
	_ = e.call(t, srvPlat, callBody("query", nil), as(tokenB))
	if got := e.up.lastSeen(t); got.Args["dsn"] != dsnA {
		t.Fatalf("dsn argument = %v", got.Args)
	}

	// tools/list: nothing is injected (a DSN is a tool argument).
	_ = e.call(t, srvPlat, `{"id":"2","method":"tools/list"}`, as(tokenA))
	if got := e.up.lastSeen(t); got.Auth != "" || got.Args != nil {
		t.Fatalf("tools/list carried %+v", got)
	}

	// The reducer's view of the request (toolCallQuery) has no DSN, because
	// injection works on a copy.
	req := &MCPRequest{Method: methodToolsCall, Params: map[string]interface{}{"name": "q", "arguments": map[string]interface{}{"sql": "x"}}}
	inj, err := (&ConnectionInjector{Resolver: e.res}).prepare(context.Background(), tenantA, srvPlat, req)
	if err != nil || inj == nil || inj.req == nil {
		t.Fatalf("prepare: %v %+v", err, inj)
	}
	if strings.Contains(toolCallQuery(req), dsnPassword) || req.Params["arguments"].(map[string]interface{})["_connection"] != nil {
		t.Fatal("injection modified the caller's request")
	}
}

// The secret-grep test: a connection's secret reaches the upstream server and
// nothing else — not the response (even when the upstream echoes it, in a
// result or in an error body), not a log line, not the tool-call event, not
// what the boundary scanner is handed. Every kind is checked.
func TestConnections_SecretNeverInProxyOutputs(t *testing.T) {
	// A TLS token endpoint: with no tenant policy the injector accepts any
	// https token URL, and the test server's client trusts its certificate.
	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"bearer","expires_in":3600}`, oauthTokenV1)
	}))
	defer tokenSrv.Close()

	e := newConnEnv(t)
	e.m.SetConnectionInjector(&ConnectionInjector{Resolver: e.res, HTTP: tokenSrv.Client()})

	kinds := []struct {
		name    string
		conn    *Connection
		secrets []string
	}{
		{"header", headerConn("h", secretA), []string{secretA}},
		{"dsn", &Connection{ID: "d", Kind: ConnKindDatabaseDSN, Version: "1", Secret: dsnA}, []string{dsnA, dsnPassword}},
		{"oauth", &Connection{ID: "o", Kind: ConnKindOAuthClientCredentials, Version: "1", Secret: oauthSecret,
			Params: map[string]string{"client_id": "cid", "token_url": tokenSrv.URL}},
			[]string{oauthSecret, oauthTokenV1}},
	}

	for _, k := range kinds {
		e.res.set(tenantA, srvPlat, k.conn)
		for _, tool := range []string{"query", "fail"} {
			rec := e.call(t, srvPlat, callBody(tool, map[string]interface{}{"q": "x"}), as(tokenA))
			if tool == "query" && rec.Code != http.StatusOK {
				t.Fatalf("%s %s: %d %s", k.name, tool, rec.Code, rec.Body)
			}
			// The upstream really did receive it, so the absence below means
			// redaction, not a broken injection.
			if b, _ := json.Marshal(e.up.lastSeen(t)); !strings.Contains(string(b), k.secrets[len(k.secrets)-1]) {
				t.Fatalf("%s: upstream did not receive the secret: %s", k.name, b)
			}
		}
		_ = e.call(t, srvPlat, `{"id":"2","method":"tools/list"}`, as(tokenA))

		var outputs []string
		outputs = append(outputs, e.bodies...)
		for _, l := range e.logs.All() {
			b, _ := json.Marshal(l.ContextMap())
			outputs = append(outputs, l.Message, string(b))
		}
		for _, r := range e.obs.recs {
			b, _ := json.Marshal(r)
			outputs = append(outputs, string(b))
		}
		outputs = append(outputs, e.scan.seen...)
		for _, s := range k.secrets {
			for i, o := range outputs {
				if strings.Contains(o, s) {
					t.Fatalf("%s: secret %q found in output %d: %s", k.name, s, i, o)
				}
			}
		}
		if !strings.Contains(strings.Join(e.bodies, ""), redactedMarker) {
			t.Fatalf("%s: expected the echoed secret to be redacted in a response", k.name)
		}
	}
	if s := e.up.lastSeen(t); s.Auth != "Bearer "+oauthTokenV1 {
		t.Fatalf("oauth: upstream auth = %q", s.Auth)
	}
}

// OAuth client credentials: the token is fetched once and reused, and a
// rotation (a new version) fetches a new one with the new secret.
func TestConnections_OAuthClientCredentials(t *testing.T) {
	var grants atomic.Int32
	var lastSecret atomic.Value
	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, sec, ok := r.BasicAuth()
		if !ok {
			sec = r.Form.Get("client_secret")
		}
		lastSecret.Store(sec)
		n := grants.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"bearer","expires_in":3600}`, n)
	}))
	defer tokenSrv.Close()

	e := newConnEnv(t)
	e.m.connections.HTTP = tokenSrv.Client()
	e.m.connections.Policy = nil // https only; the tenant policy is tested below
	conn := &Connection{ID: "o", Kind: ConnKindOAuthClientCredentials, Version: "1", Secret: oauthSecret,
		Params: map[string]string{"client_id": "cid", "token_url": tokenSrv.URL, "scopes": "crm.read"}}
	e.res.set(tenantA, srvPlat, conn)
	for i := 0; i < 3; i++ {
		_ = e.call(t, srvPlat, callBody("query", nil), as(tokenA))
	}
	if grants.Load() != 1 || e.up.lastSeen(t).Auth != "Bearer tok-1" {
		t.Fatalf("grants=%d auth=%q", grants.Load(), e.up.lastSeen(t).Auth)
	}
	if lastSecret.Load() != oauthSecret {
		t.Fatalf("token endpoint got secret %v", lastSecret.Load())
	}
	rot := *conn
	rot.Version, rot.Secret = "2", "rotated-client-secret-0000"
	e.res.set(tenantA, srvPlat, &rot)
	_ = e.call(t, srvPlat, callBody("query", nil), as(tokenA))
	if grants.Load() != 2 || e.up.lastSeen(t).Auth != "Bearer tok-2" || lastSecret.Load() != rot.Secret {
		t.Fatalf("after rotation grants=%d auth=%q", grants.Load(), e.up.lastSeen(t).Auth)
	}

	// A token URL outside the tenant's egress allowlist is refused (fail
	// closed), with no upstream call.
	e.m.connections.Policy = e.m.TenantPolicy()
	e.m.connections.tokens = oauthTokenCache{}
	before := e.up.count()
	if rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("token URL off the allowlist = %d %s", rec.Code, rec.Body)
	}
	if e.up.count() != before {
		t.Fatal("upstream called without the tenant's connection")
	}
}

// Rotation takes effect within the cache TTL: the cached connection is used
// until it expires, then the new one; errors are not cached.
func TestConnections_CacheTTLAndRotation(t *testing.T) {
	inner := &fakeResolver{}
	inner.set(tenantA, srvPlat, headerConn("c", oldSecret))
	now := time.Unix(1_800_000_000, 0)
	c := NewCachingConnectionResolver(inner, 60*time.Second)
	c.Now = func() time.Time { return now }
	ctx := context.Background()

	get := func() string {
		t.Helper()
		got, err := c.Resolve(ctx, tenantA, srvPlat)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			return ""
		}
		return got.Secret
	}
	if get() != oldSecret {
		t.Fatal("first resolve")
	}
	inner.set(tenantA, srvPlat, headerConn("c", newSecret))
	now = now.Add(59 * time.Second)
	if get() != oldSecret || inner.calls != 1 {
		t.Fatalf("within TTL: calls=%d", inner.calls)
	}
	now = now.Add(2 * time.Second)
	if get() != newSecret || inner.calls != 2 {
		t.Fatalf("after TTL: calls=%d", inner.calls)
	}
	// "None" is cached too, per (tenant, server).
	if s, _ := c.Resolve(ctx, tenantB, srvPlat); s != nil {
		t.Fatal("B has no connection")
	}
	_, _ = c.Resolve(ctx, tenantB, srvPlat)
	if inner.calls != 3 {
		t.Fatalf("none not cached: calls=%d", inner.calls)
	}
	// Errors are not cached.
	inner.err = errors.New("down")
	now = now.Add(2 * time.Minute)
	if _, err := c.Resolve(ctx, tenantA, srvPlat); err == nil {
		t.Fatal("error swallowed")
	}
	inner.err = nil
	if get() != newSecret {
		t.Fatal("recovered resolve")
	}
}

// A resolver outage fails the call closed (503), without contacting the
// upstream; an unattributed call does not resolve at all.
func TestConnections_ResolverOutageFailsClosed(t *testing.T) {
	e := newConnEnv(t)
	e.res.err = errors.New("dashboard down: " + secretA) // an error text must not leak either
	rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA))
	if rec.Code != http.StatusServiceUnavailable || e.up.count() != 0 {
		t.Fatalf("outage = %d, upstream calls %d", rec.Code, e.up.count())
	}
	if rec := e.call(t, srvPlat, callBody("query", nil), nil); rec.Code != http.StatusOK {
		t.Fatalf("unattributed during outage = %d", rec.Code)
	}
	// The resolver's error text is logged, never returned.
	for _, b := range e.bodies {
		if strings.Contains(b, secretA) || strings.Contains(b, "dashboard down") {
			t.Fatalf("resolver error text in a response: %s", b)
		}
	}
}

// Injection off: nothing is resolved and nothing is added.
func TestConnections_OffResolvesNothing(t *testing.T) {
	e := newConnEnv(t)
	e.m.SetConnectionInjector(nil)
	e.res.set(tenantA, srvPlat, headerConn("c", secretA))
	_ = e.call(t, srvPlat, callBody("query", nil), as(tokenA))
	if e.res.calls != 0 || e.up.lastSeen(t).Auth != "" {
		t.Fatalf("calls=%d auth=%q", e.res.calls, e.up.lastSeen(t).Auth)
	}
}

// Test connection's on-behalf header: honored only with the admin token, so
// the tenant still never comes from anything a caller can assert.
func TestConnections_OnBehalfOfTenant(t *testing.T) {
	const tu = "6f1d2e3f-0000-4000-8000-00000000000a"
	e := newConnEnv(t)
	s := &MCPServer{ID: "u-crm", Name: "U CRM", TenantID: tu, ManagedBy: ManagedByAIQG,
		Endpoint: e.up.srv.URL, Protocol: ProtocolHTTP, Auth: AuthConfig{Type: AuthNone}}
	if err := e.m.registry.Put(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	e.res.set(tu, "u-crm", headerConn("u", secretB))
	list := listBody

	if rec := e.call(t, "u-crm", list, map[string]string{AdminTokenHeader: adminToken, OnBehalfOfTenantHeader: tu}); rec.Code != http.StatusOK {
		t.Fatalf("admin on behalf = %d %s", rec.Code, rec.Body)
	}
	if e.up.lastSeen(t).Auth != "Bearer "+secretB {
		t.Fatal("on-behalf call did not inject the tenant's connection")
	}
	// Without the admin token the header is ignored: unattributed, 403.
	if rec := e.call(t, "u-crm", list, map[string]string{OnBehalfOfTenantHeader: tu}); rec.Code != http.StatusForbidden {
		t.Fatalf("header without admin token = %d", rec.Code)
	}
	// A tenant's own token plus the header still acts as that token's tenant.
	if rec := e.call(t, "u-crm", list, map[string]string{"TAS-Auth": tokenB, OnBehalfOfTenantHeader: tu}); rec.Code != http.StatusForbidden {
		t.Fatalf("tenant token + header = %d", rec.Code)
	}
	if rec := e.call(t, "u-crm", list, map[string]string{AdminTokenHeader: "wrong", OnBehalfOfTenantHeader: tu}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong admin token + header = %d", rec.Code)
	}
	if rec := e.call(t, "u-crm", list, map[string]string{AdminTokenHeader: adminToken, OnBehalfOfTenantHeader: "nope"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad tenant id = %d", rec.Code)
	}
}

// The dashboard resolver: Internal-Auth, 200 decode, 404 = none, else error.
func TestDashboardConnectionResolver(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Internal-Auth")
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(r.Body)
		gotBody = b.String()
		if r.URL.Path != connectionsResolvePath {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.Contains(gotBody, `"server_id":"none"`):
			http.Error(w, `{"error":{"code":"not_found"}}`, http.StatusNotFound)
		case strings.Contains(gotBody, `"server_id":"boom"`):
			http.Error(w, "x", http.StatusInternalServerError)
		default:
			_, _ = w.Write([]byte(`{"connection_id":"c1","kind":"header","version":"v","params":{"prefix":"Bearer "},"secret":"s3cr3t-value"}`))
		}
	}))
	defer srv.Close()
	r, err := NewDashboardConnectionResolver(srv.URL+"/", "svc-token")
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.Resolve(context.Background(), tenantA, "dbhub")
	if err != nil || c == nil || c.Secret != "s3cr3t-value" || c.Params["prefix"] != "Bearer " {
		t.Fatalf("200: %v %v", c, err)
	}
	if gotAuth != "svc-token" || !strings.Contains(gotBody, `"tenant_id":"tenant-a"`) {
		t.Fatalf("request auth=%q body=%s", gotAuth, gotBody)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", c, c, c), "s3cr3t") {
		t.Fatal("a Connection prints its secret")
	}
	if c, err := r.Resolve(context.Background(), tenantA, "none"); c != nil || err != nil {
		t.Fatalf("404: %v %v", c, err)
	}
	if _, err := r.Resolve(context.Background(), tenantA, "boom"); err == nil {
		t.Fatal("500 not an error")
	}
	if _, err := NewDashboardConnectionResolver("", "x"); err == nil {
		t.Fatal("empty URL accepted")
	}
	if _, err := NewDashboardConnectionResolver("http://x", ""); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestRedactor(t *testing.T) {
	r := &redactor{}
	r.add("abc", "pw-secret", "postgres://u:pw-secret@h/db")
	if got := r.str("dsn postgres://u:pw-secret@h/db and pw-secret, abc"); got != "dsn [REDACTED] and [REDACTED], abc" {
		t.Fatalf("str = %q", got)
	}
	resp := r.response(&MCPResponse{Result: map[string]interface{}{"a": []interface{}{"x pw-secret", 1.0}},
		Error: &MCPError{Message: "pw-secret", Data: "pw-secret"}})
	b, _ := json.Marshal(resp)
	if strings.Contains(string(b), "pw-secret") {
		t.Fatalf("response not redacted: %s", b)
	}
	base := errors.New("boom pw-secret")
	err := r.err(base)
	if strings.Contains(err.Error(), "pw-secret") || !errors.Is(err, base) {
		t.Fatalf("err = %v", err)
	}
	var nilR *redactor
	if nilR.str("x") != "x" || nilR.err(base) != base || nilR.response(nil) != nil {
		t.Fatal("nil redactor")
	}
}

// The streamable-HTTP (sse) client applies the injected header too, replacing
// the server's own auth header.
func TestConnections_SSEClientHeader(t *testing.T) {
	var got atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	defer up.Close()
	e := newConnEnv(t)
	s := &MCPServer{ID: "sse-srv", Name: "SSE", Endpoint: up.URL, Protocol: ProtocolSSE,
		Auth: AuthConfig{Type: AuthAPIKey, Config: map[string]string{"key": "Bearer platform-default", "header": "Authorization"}}}
	if err := e.m.registry.Put(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	e.res.set(tenantA, "sse-srv", headerConn("s", secretA))
	if rec := e.call(t, "sse-srv", listBody, as(tokenA)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got.Load() != "Bearer "+secretA {
		t.Fatalf("sse upstream auth = %v", got.Load())
	}
	_ = e.call(t, "sse-srv", listBody, as(tokenC))
	if got.Load() != "Bearer platform-default" {
		t.Fatalf("tenant C (no connection) sent %v, want the server's own auth", got.Load())
	}
}

// An on-behalf tools/list (Test connection) sees every tool, so the UI can
// choose the allowlist; a tenant's own call sees the filtered list, and the
// on-behalf call still cannot call a disallowed tool.
func TestConnections_OnBehalfToolsListUnfiltered(t *testing.T) {
	const tu = "6f1d2e3f-0000-4000-8000-00000000000b"
	e := newConnEnv(t)
	s := &MCPServer{ID: "f-crm", Name: "F", TenantID: tu, ManagedBy: ManagedByAIQG, AllowedTools: []string{"other"},
		Endpoint: e.up.srv.URL, Protocol: ProtocolHTTP, Auth: AuthConfig{Type: AuthNone}}
	if err := e.m.registry.Put(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	admin := map[string]string{AdminTokenHeader: adminToken, OnBehalfOfTenantHeader: tu}
	rec := e.call(t, "f-crm", listBody, admin)
	if names := toolNames(t, rec); !eq(names, "query") {
		t.Fatalf("on-behalf tools/list = %v, want the unfiltered list", names)
	}
	if rec := e.call(t, "f-crm", callBody("query", nil), admin); rec.Code != http.StatusForbidden {
		t.Fatalf("on-behalf call of a disallowed tool = %d", rec.Code)
	}
}
