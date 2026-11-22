package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

// Plan #22 C.1: the tenant-scoped registry.

const (
	tenantA    = "tenant-a"
	tenantB    = "tenant-b"
	tokenA     = "tas_qg_live_aaaa"
	tokenB     = "tas_qg_live_bbbb"
	adminToken = "admin-secret"
	srvPlat    = "platform-srv"
	srvA       = "a-crm"
	toolRead   = "read_record"
	toolWrite  = "write_record"
)

// fakeValidator maps a bearer to a tenant.
type fakeValidator map[string]string

func (f fakeValidator) Validate(_ context.Context, bearer string) (*identity.Token, error) {
	if t, ok := f[bearer]; ok {
		return &identity.Token{TokenID: "tok-" + t, TenantID: t}, nil
	}
	return nil, identity.ErrTokenNotFound
}

// twoToolServer is an upstream MCP server with one read and one write tool.
func twoToolServer(t *testing.T) *httptest.Server {
	t.Helper()
	m := http.NewServeMux()
	m.HandleFunc("/mcp/tools/list", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tools":[{"name":"` + toolRead + `"},{"name":"` + toolWrite + `"}]}`))
	})
	m.HandleFunc("/mcp/tools/call", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name, _ := body["name"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"called ` + name + `"}],"isError":false}`))
	})
	ts := httptest.NewServer(m)
	t.Cleanup(ts.Close)
	return ts
}

type tenancyEnv struct {
	m      *Manager
	router *mux.Router
	obs    *captureObserver
	up     *httptest.Server
}

// newTenancyEnv builds a manager with a platform server and tenant A's
// server, both pointing at a real upstream, with the tenant registry on
// (policy non-nil) or off.
func newTenancyEnv(t *testing.T, policy *TenantPolicy) *tenancyEnv {
	t.Helper()
	up := twoToolServer(t)
	m := createTestManager()
	m.SetTenantPolicy(policy)
	put := func(s *MCPServer) {
		s.Endpoint = up.URL
		s.Protocol = ProtocolHTTP
		s.Auth = AuthConfig{Type: AuthNone}
		s.HealthCheck.Enabled = false
		if err := m.registry.Put(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	put(&MCPServer{ID: srvPlat, Name: "Platform"})
	put(&MCPServer{ID: srvA, Name: "A CRM", TenantID: tenantA, ManagedBy: ManagedByAIQG,
		AllowedTools: []string{toolRead}})
	obs := &captureObserver{}
	m.SetToolCallObserver(obs)

	h := NewHTTPHandlers(m, zap.NewNop())
	h.SetIdentityResolver(&identity.Resolver{Validator: fakeValidator{tokenA: tenantA, tokenB: tenantB}})
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	return &tenancyEnv{m: m, router: r, obs: obs, up: up}
}

func onPolicy() *TenantPolicy {
	return &TenantPolicy{AdminToken: adminToken, Egress: map[string][]string{tenantA: {"api.crm.example", "*.vendor.example"}}}
}

func (e *tenancyEnv) do(t *testing.T, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func as(token string) map[string]string {
	if token == "" {
		return nil
	}
	return map[string]string{"TAS-Auth": token}
}

func (e *tenancyEnv) listIDs(t *testing.T, hdr map[string]string) []string {
	t.Helper()
	rec := e.do(t, http.MethodGet, "/api/v1/federation/servers", "", hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Servers []struct {
			ID string `json:"id"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(out.Servers))
	for _, s := range out.Servers {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return ids
}

func invokeBody(method, tool string) string {
	if method == methodToolsList {
		return `{"id":"1","method":"tools/list"}`
	}
	return `{"id":"1","method":"tools/call","params":{"name":"` + tool + `","arguments":{}}}`
}

func (e *tenancyEnv) invoke(t *testing.T, srv, method, tool, token string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodPost, "/api/v1/federation/servers/"+srv+"/invoke", invokeBody(method, tool), as(token))
}

func toolNames(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	names := make([]string, 0, len(resp.Result.Tools))
	for _, tl := range resp.Result.Tools {
		names = append(names, tl.Name)
	}
	sort.Strings(names)
	return names
}

func eq(a []string, b ...string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

// Two tenants: A's server is invisible to B (list, get, tools/list, invoke
// 403) and to an unattributed caller; the platform server is visible to all.
func TestTenancy_TwoTenantsIsolation(t *testing.T) {
	e := newTenancyEnv(t, onPolicy())

	if ids := e.listIDs(t, as(tokenA)); !eq(ids, srvA, srvPlat) {
		t.Fatalf("A lists %v", ids)
	}
	if ids := e.listIDs(t, as(tokenB)); !eq(ids, srvPlat) {
		t.Fatalf("B lists %v", ids)
	}
	if ids := e.listIDs(t, nil); !eq(ids, srvPlat) {
		t.Fatalf("unattributed lists %v", ids)
	}
	// An unknown token is unattributed, not a tenant.
	if ids := e.listIDs(t, as("tas_qg_live_nope")); !eq(ids, srvPlat) {
		t.Fatalf("unknown token lists %v", ids)
	}
	if rec := e.do(t, http.MethodGet, "/api/v1/federation/servers/"+srvA, "", as(tokenB)); rec.Code != http.StatusNotFound {
		t.Fatalf("B get A's server = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodGet, "/api/v1/federation/servers/"+srvA, "", as(tokenA)); rec.Code != http.StatusOK {
		t.Fatalf("A get own server = %d", rec.Code)
	}

	for _, method := range []string{methodToolsList, methodToolsCall} {
		for _, tok := range []string{tokenB, ""} {
			if rec := e.invoke(t, srvA, method, toolRead, tok); rec.Code != http.StatusForbidden {
				t.Fatalf("%s on A's server with token %q = %d %s", method, tok, rec.Code, rec.Body)
			}
		}
		if rec := e.invoke(t, srvA, method, toolRead, tokenA); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"error"`) {
			t.Fatalf("A %s on own server = %d %s", method, rec.Code, rec.Body)
		}
		for _, tok := range []string{tokenA, tokenB, ""} {
			if rec := e.invoke(t, srvPlat, method, toolWrite, tok); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"error"`) {
				t.Fatalf("%s on platform with %q = %d %s", method, tok, rec.Code, rec.Body)
			}
		}
	}

	// Health and probe lookups of another tenant's server read as absent,
	// and the health map does not name it.
	if rec := e.do(t, http.MethodGet, "/api/v1/federation/servers/"+srvA+"/health", "", as(tokenB)); rec.Code != http.StatusNotFound {
		t.Fatalf("B health of A's server = %d", rec.Code)
	}
	rec := e.do(t, http.MethodGet, "/api/v1/federation/health", "", as(tokenB))
	if strings.Contains(rec.Body.String(), srvA) {
		t.Fatalf("B's health map names A's server: %s", rec.Body)
	}
	// The cross-tenant refusals emitted no tool-call events.
	for _, r := range e.obs.recs {
		if r.ServerID == srvA && (r.Identity == nil || r.Identity.TenantID != tenantA) {
			t.Fatalf("event for A's server from another caller: %+v", r)
		}
	}
}

// A disallowed tool is missing from tools/list and refused on invoke with a
// blocked tool-call event.
func TestTenancy_AllowedTools(t *testing.T) {
	e := newTenancyEnv(t, onPolicy())

	rec := e.invoke(t, srvA, methodToolsList, "", tokenA)
	if rec.Code != http.StatusOK || !eq(toolNames(t, rec), toolRead) {
		t.Fatalf("A's tools/list = %d %v", rec.Code, rec.Body)
	}
	// The platform server has no allowlist: both tools.
	if rec := e.invoke(t, srvPlat, methodToolsList, "", tokenA); !eq(toolNames(t, rec), toolRead, toolWrite) {
		t.Fatalf("platform tools/list = %s", rec.Body)
	}

	e.obs.recs = nil
	rec = e.invoke(t, srvA, methodToolsCall, toolWrite, tokenA)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "allowed_tools") {
		t.Fatalf("disallowed call = %d %s", rec.Code, rec.Body)
	}
	if len(e.obs.recs) != 1 {
		t.Fatalf("events = %d, want 1", len(e.obs.recs))
	}
	r := e.obs.recs[0]
	if r.Outcome != OutcomeBlocked || r.ToolName != toolWrite || r.ServerID != srvA ||
		r.Identity == nil || r.Identity.TenantID != tenantA {
		t.Fatalf("event %+v", r)
	}

	e.obs.recs = nil
	if rec := e.invoke(t, srvA, methodToolsCall, toolRead, tokenA); rec.Code != http.StatusOK {
		t.Fatalf("allowed call = %d %s", rec.Code, rec.Body)
	}
	if len(e.obs.recs) != 1 || e.obs.recs[0].Outcome != OutcomeOK {
		t.Fatalf("allowed call events %+v", e.obs.recs)
	}
}

// A disabled server is not listed, not invoked and not probed.
func TestTenancy_DisabledServer(t *testing.T) {
	e := newTenancyEnv(t, onPolicy())
	off := false
	s, _ := e.m.registry.Get(context.Background(), srvPlat)
	s.Enabled = &off
	_ = e.m.registry.Put(context.Background(), s)

	if ids := e.listIDs(t, as(tokenA)); !eq(ids, srvA) {
		t.Fatalf("list with platform disabled = %v", ids)
	}
	// The admin view still shows it, so the reconcile can see it.
	if ids := e.listIDs(t, map[string]string{AdminTokenHeader: adminToken}); !eq(ids, srvA, srvPlat) {
		t.Fatalf("admin list = %v", ids)
	}
	if rec := e.invoke(t, srvPlat, methodToolsCall, toolRead, tokenA); rec.Code != http.StatusForbidden {
		t.Fatalf("invoke disabled = %d", rec.Code)
	}

	p := NewProber(e.m, ProbeConfig{}, zap.NewNop())
	e.m.SetProber(p)
	p.RunOnce(context.Background())
	if _, ok := p.ProbeStatus(srvPlat); ok {
		t.Fatal("disabled server was probed")
	}
	if _, ok := p.ProbeStatus(srvA); !ok {
		t.Fatal("enabled server was not probed")
	}
}

// Writes need the admin token when the flag is on: 401 missing, 403 wrong,
// 503 when none is configured; with it, register, PUT and delete work.
func TestTenancy_WritesRequireAdminToken(t *testing.T) {
	e := newTenancyEnv(t, onPolicy())
	reg := `{"id":"new","name":"n","endpoint":"http://new.tas-mcp-servers.svc:8080","protocol":"http"}`
	paths := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/federation/servers", reg},
		{http.MethodPut, "/api/v1/federation/servers/" + srvPlat, reg},
		{http.MethodDelete, "/api/v1/federation/servers/" + srvPlat, ""},
	}
	for _, p := range paths {
		if rec := e.do(t, p.method, p.path, p.body, nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s no token = %d", p.method, p.path, rec.Code)
		}
		if rec := e.do(t, p.method, p.path, p.body, map[string]string{AdminTokenHeader: "wrong"}); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s wrong token = %d", p.method, p.path, rec.Code)
		}
		// A tenant's TAS-Auth token is not an admin token.
		if rec := e.do(t, p.method, p.path, p.body, as(tokenA)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s tenant token = %d", p.method, p.path, rec.Code)
		}
	}

	admin := map[string]string{AdminTokenHeader: adminToken}
	if rec := e.do(t, http.MethodPost, "/api/v1/federation/servers", reg, admin); rec.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", rec.Code, rec.Body)
	}
	got, _ := e.m.GetServer("new")
	if got.ManagedBy != ManagedByOperator || !got.IsEnabled() || got.Enabled == nil {
		t.Fatalf("register did not write the defaults explicitly: %+v", got)
	}

	upd := `{"name":"renamed","endpoint":"` + e.up.URL + `","protocol":"http","auth":{"type":"none"},"allowed_tools":["` + toolRead + `"]}`
	if rec := e.do(t, http.MethodPut, "/api/v1/federation/servers/"+srvPlat, upd, admin); rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s", rec.Code, rec.Body)
	}
	if rec := e.invoke(t, srvPlat, methodToolsList, "", ""); !eq(toolNames(t, rec), toolRead) {
		t.Fatalf("PUT allowlist not applied to a cached service: %s", rec.Body)
	}
	if rec := e.do(t, http.MethodPut, "/api/v1/federation/servers/missing", upd, admin); rec.Code != http.StatusNotFound {
		t.Fatalf("put missing = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPut, "/api/v1/federation/servers/"+srvPlat, `{"id":"other"}`, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("put id mismatch = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodDelete, "/api/v1/federation/servers/new", "", admin); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}

	e.m.SetTenantPolicy(&TenantPolicy{})
	if rec := e.do(t, http.MethodPost, "/api/v1/federation/servers", reg, admin); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("register with no admin token configured = %d", rec.Code)
	}
}

// Q13: tenant registration is refused unless in-cluster (allowed namespace)
// or HTTPS to an allowlisted host.
func TestTenancy_RegisterEgress(t *testing.T) {
	e := newTenancyEnv(t, onPolicy())
	admin := map[string]string{AdminTokenHeader: adminToken}
	cases := []struct {
		name, tenant, endpoint, protocol, managedBy string
		want                                        int
	}{
		{"in-cluster allowed ns", tenantA, "http://crm.tas-mcp-servers.svc.cluster.local:8080/mcp", "http", "", http.StatusCreated},
		{"in-cluster other ns", tenantA, "http://redis-shared.tas-shared.svc:6379", "http", "", http.StatusBadRequest},
		{"external https listed", tenantA, "https://api.crm.example/mcp", "sse", "", http.StatusCreated},
		{"external wildcard", tenantA, "https://eu.vendor.example/mcp", "http", "", http.StatusCreated},
		{"wildcard apex not matched", tenantA, "https://vendor.example/mcp", "http", "", http.StatusBadRequest},
		{"external http", tenantA, "http://api.crm.example/mcp", "http", "", http.StatusBadRequest},
		{"external unlisted", tenantA, "https://evil.example/mcp", "http", "", http.StatusBadRequest},
		{"other tenant's host", tenantB, "https://api.crm.example/mcp", "http", "", http.StatusBadRequest},
		{"ip literal", tenantA, "https://10.0.0.5/mcp", "http", "", http.StatusBadRequest},
		{"bare host", tenantA, "http://dbhub:8080", "http", "", http.StatusBadRequest},
		{"userinfo", tenantA, "https://u:p@api.crm.example/mcp", "http", "", http.StatusBadRequest},
		{"stdio", tenantA, "npx something", "stdio", "", http.StatusBadRequest},
		{"tenant row as operator", tenantA, "https://api.crm.example/mcp", "http", "operator", http.StatusBadRequest},
		{"bad managed_by", "", "http://x.tas-mcp-servers.svc", "http", "someone", http.StatusBadRequest},
		{"platform anywhere", "", "http://anything:9000", "http", "", http.StatusCreated},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]interface{}{
				"id": "srv-" + string(rune('a'+i)), "name": c.name, "tenant_id": c.tenant,
				"endpoint": c.endpoint, "protocol": c.protocol, "managed_by": c.managedBy,
			})
			rec := e.do(t, http.MethodPost, "/api/v1/federation/servers", string(body), admin)
			if rec.Code != c.want {
				t.Fatalf("= %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
}

// A record written before C.1 (no tenancy fields) reads as a platform,
// operator-managed, enabled server through the Redis registry, and the view
// shows those defaults explicitly.
func TestTenancy_LazyMigrationOfLegacyRecord(t *testing.T) {
	reg := newTestRedisRegistry(t)
	rr := reg.(*redisRegistry)
	legacy := `{"id":"old","name":"Old","endpoint":"http://old:1","protocol":"http","auth":{"type":"none"},"status":"unknown"}`
	if err := rr.rdb.Set(context.Background(), rr.key("old"), legacy, 0).Err(); err != nil {
		t.Fatal(err)
	}
	m := NewManagerWithRegistry(zap.NewNop(), &MockServiceDiscovery{}, &MockProtocolBridge{}, reg)
	m.SetTenantPolicy(onPolicy())
	s, err := m.GetServer("old")
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsPlatform() || s.Owner() != ManagedByOperator || !s.IsEnabled() || !s.ToolAllowed("anything") {
		t.Fatalf("legacy record read as %+v", s)
	}
	if !m.TenantPolicy().Visible(s, nil) {
		t.Fatal("legacy record not visible to an unattributed caller")
	}

	h := NewHTTPHandlers(m, zap.NewNop())
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/federation/servers/old", http.NoBody))
	if !strings.Contains(rec.Body.String(), `"managed_by":"operator"`) || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("view = %s", rec.Body)
	}
}

// With MCP_TENANT_SERVERS off nothing changes: writes need no token, PUT is
// not routed, every caller sees and may call every server and tool, and
// invoke errors stay 500.
func TestTenancy_FlagOffUnchanged(t *testing.T) {
	e := newTenancyEnv(t, nil)
	if ids := e.listIDs(t, as(tokenB)); !eq(ids, srvA, srvPlat) {
		t.Fatalf("flag off: B lists %v", ids)
	}
	if ids := e.listIDs(t, nil); !eq(ids, srvA, srvPlat) {
		t.Fatalf("flag off: unattributed lists %v", ids)
	}
	if rec := e.invoke(t, srvA, methodToolsCall, toolWrite, tokenB); rec.Code != http.StatusOK {
		t.Fatalf("flag off: B calls A's write tool = %d", rec.Code)
	}
	if rec := e.invoke(t, srvA, methodToolsList, "", ""); !eq(toolNames(t, rec), toolRead, toolWrite) {
		t.Fatalf("flag off: tools/list filtered: %s", rec.Body)
	}
	reg := `{"id":"new","name":"n","endpoint":"http://127.0.0.1:1","protocol":"http"}`
	if rec := e.do(t, http.MethodPost, "/api/v1/federation/servers", reg, nil); rec.Code != http.StatusCreated {
		t.Fatalf("flag off: register without token = %d %s", rec.Code, rec.Body)
	}
	got, _ := e.m.GetServer("new")
	if got.ManagedBy != "" || got.Enabled != nil {
		t.Fatalf("flag off: register wrote tenancy fields: %+v", got)
	}
	if rec := e.do(t, http.MethodGet, "/api/v1/federation/servers/new", "", nil); strings.Contains(rec.Body.String(), "managed_by") {
		t.Fatalf("flag off: view shows tenancy fields: %s", rec.Body)
	}
	if rec := e.do(t, http.MethodPut, "/api/v1/federation/servers/new", reg, nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("flag off: PUT = %d, want 405 (not routed)", rec.Code)
	}
	if rec := e.do(t, http.MethodDelete, "/api/v1/federation/servers/new", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("flag off: delete without token = %d", rec.Code)
	}
	if rec := e.invoke(t, "missing", methodToolsCall, toolRead, ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("flag off: invoke missing = %d", rec.Code)
	}
	// A disabled flag on a record is ignored when the registry is not tenant-scoped.
	off := false
	s, _ := e.m.registry.Get(context.Background(), srvPlat)
	s.Enabled = &off
	if rec := e.invoke(t, srvPlat, methodToolsCall, toolRead, ""); rec.Code != http.StatusOK {
		t.Fatalf("flag off: enabled=false honored = %d", rec.Code)
	}
}

// Broadcast reaches only the servers the caller may call.
func TestTenancy_BroadcastScoped(t *testing.T) {
	e := newTenancyEnv(t, onPolicy())
	for _, id := range []string{srvPlat, srvA} {
		if _, err := e.m.ensureService(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		s, _ := e.m.registry.Get(context.Background(), id)
		s.Status = StatusHealthy
	}
	ctxB := identity.WithIdentity(context.Background(), &identity.Identity{Attributed: true, TenantID: tenantB})
	resps, err := e.m.BroadcastRequest(ctxB, &MCPRequest{ID: "b", Method: methodToolsList})
	if err != nil || len(resps) != 1 {
		t.Fatalf("B broadcast reached %d servers (%v), want 1", len(resps), err)
	}
	ctxA := identity.WithIdentity(context.Background(), &identity.Identity{Attributed: true, TenantID: tenantA})
	if resps, _ := e.m.BroadcastRequest(ctxA, &MCPRequest{ID: "a", Method: methodToolsList}); len(resps) != 2 {
		t.Fatalf("A broadcast reached %d servers, want 2", len(resps))
	}
}

func TestTenancy_UnitHelpers(t *testing.T) {
	var nilPolicy *TenantPolicy
	s := &MCPServer{ID: "x", TenantID: tenantA}
	if !nilPolicy.Visible(s, nil) || nilPolicy.CheckInvoke(s, nil) != nil || nilPolicy.Validate(s) != nil {
		t.Fatal("nil policy must allow everything")
	}
	p := onPolicy()
	if err := p.CheckInvoke(s, &identity.Identity{Attributed: false, TenantID: tenantA}); !errors.Is(err, ErrTenantForbidden) {
		t.Fatalf("an unattributed identity's tenant must not count: %v", err)
	}
	if got := filterToolsList(&MCPResponse{Result: "not a map"}, &MCPServer{AllowedTools: []string{"a"}}); got.Result != "not a map" {
		t.Fatal("non-tools/list result changed")
	}
	orig := &MCPResponse{Result: map[string]interface{}{"tools": []interface{}{
		map[string]interface{}{"name": "a"}, map[string]interface{}{"name": "b"},
	}, "nextCursor": "c"}}
	got := filterToolsList(orig, &MCPServer{AllowedTools: []string{"a"}})
	res := got.Result.(map[string]interface{})
	if len(res["tools"].([]interface{})) != 1 || res["nextCursor"] != "c" {
		t.Fatalf("filtered = %+v", res)
	}
	if len(orig.Result.(map[string]interface{})["tools"].([]interface{})) != 2 {
		t.Fatal("filterToolsList mutated its input")
	}
	if ns, ok := clusterNamespace("svc.ns.svc.cluster.local"); !ok || ns != "ns" {
		t.Fatalf("clusterNamespace = %q %v", ns, ok)
	}
	if _, ok := clusterNamespace("a.b.c.svc"); ok {
		t.Fatal("pod-style host read as a service")
	}
}
