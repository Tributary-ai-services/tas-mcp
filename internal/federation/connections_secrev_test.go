package federation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

// Regression tests for the 2026-11-21 security review of tas-mcp #46. Each one
// started as the reviewer's proof of concept (secrev_poc_test.go) and failed
// before its fix.

const selectOne = "select 1"

// pointAt moves srvPlat to another upstream.
func (e *connEnv) pointAt(t *testing.T, url string) {
	t.Helper()
	s, err := e.m.registry.Get(context.Background(), srvPlat)
	if err != nil {
		t.Fatal(err)
	}
	s.Endpoint = url
	if err := e.m.registry.Put(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	e.m.mu.Lock()
	delete(e.m.services, srvPlat)
	e.m.mu.Unlock()
}

// declare sets srvPlat's connection kind in its record.
func (e *connEnv) declare(t *testing.T, kind string) {
	t.Helper()
	s, _ := e.m.registry.Get(context.Background(), srvPlat)
	s.ConnectionKind = kind
	if err := e.m.registry.Put(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

// decodedMessage is the error message a JSON client of the proxy reads.
func decodedMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var r struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Error == nil {
		return ""
	}
	return r.Error.Message
}

// Fix 2a: the 256-byte error preview used to be cut before redaction, so the
// part of a secret before the cut survived.
func TestSecRev_TruncatedSecretPrefixRedacted(t *testing.T) {
	secret := "sk-live-ZZZZ-very-long-tenant-secret-0123456789abcdef"
	for _, pad := range []int{200, 215, 230, 240} { // the cut lands at different places in the secret
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, strings.Repeat("x", pad)+" key="+r.Header.Get("Authorization"), http.StatusUnauthorized)
		}))
		e := newConnEnv(t)
		e.pointAt(t, up.URL)
		e.res.set(tenantA, srvPlat, headerConn("h", secret))
		rec := e.call(t, srvPlat, callBody("q", nil), as(tokenA))
		up.Close()
		out := rec.Body.String() + decodedMessage(t, rec)
		for _, l := range e.logs.All() {
			b, _ := json.Marshal(l.ContextMap())
			out += string(b)
		}
		if strings.Contains(out, secret[:minCutPrefix]) {
			t.Fatalf("pad %d: a prefix of the secret survives: %s", pad, rec.Body)
		}
	}
}

// Fix 2b: a password a Go upstream echoes JSON-escaped (HTML-escaped by json.Marshal) used
// to survive, and a JSON client decoding the proxy's response read it back.
func TestSecRev_JSONEscapedPasswordRedacted(t *testing.T) {
	pw := "p<ss&w>rd-9876"
	dsn := "postgres://alice:" + strings.NewReplacer("<", "%3C", "&", "%26", ">", "%3E").Replace(pw) + "@pg.vendor.example:5432/sales"
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, &Connection{ID: "d", Kind: ConnKindDatabaseDSN, Version: "1", Secret: dsn})
	rec := e.call(t, srvPlat, callBody("fail", nil), as(tokenA))
	if got := e.up.lastSeen(t); !strings.Contains(mustJSON(got.Args), "p\\u003css") {
		t.Fatalf("precondition: the upstream did not receive the escaped password: %v", got.Args)
	}
	for _, out := range []string{rec.Body.String(), decodedMessage(t, rec)} {
		if strings.Contains(out, pw) || strings.Contains(out, "p\\u003css\\u0026w") || strings.Contains(out, "p\\\\u003css") {
			t.Fatalf("password survives: %s", out)
		}
	}
	for _, l := range e.logs.All() {
		b, _ := json.Marshal(l.ContextMap())
		var back interface{}
		_ = json.Unmarshal(b, &back)
		if strings.Contains(string(b), `u003css`) || strings.Contains(mustJSON(back), pw) {
			t.Fatalf("password in a log line: %s", b)
		}
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Fix 2c/2d: base64 forms, URL-escaped forms and map keys are redacted; a
// prefix at a cut is redacted.
func TestSecRev_RedactorForms(t *testing.T) {
	sec := "Sup3r/Secret+Value=="
	r := &redactor{}
	r.add(sec)
	for _, form := range []string{sec, base64.StdEncoding.EncodeToString([]byte(sec)), base64.RawURLEncoding.EncodeToString([]byte(sec)),
		"Sup3r%2FSecret%2BValue%3D%3D"} {
		if got := r.str("x " + form + " y"); strings.Contains(got, form) {
			t.Fatalf("form %q survives: %q", form, got)
		}
	}
	v := r.value(map[string]interface{}{sec: "k", "n": map[string]interface{}{"x" + sec: 1.0}})
	if strings.Contains(mustJSON(v), "Secret") {
		t.Fatalf("a map key survives: %s", mustJSON(v))
	}
	if got := r.preview([]byte(strings.Repeat("a", 250)+sec[:10]), 256); strings.Contains(got, sec[:minCutPrefix]) {
		t.Fatalf("prefix at the cut survives: %q", got)
	}
	if got := r.cut("ends with Sup3r/Se"); strings.Contains(got, "Sup3r/S") {
		t.Fatalf("cut: %q", got)
	}
	if got := r.cut("short Sup3r/"); got != "short Sup3r/" { // under minCutPrefix: left alone
		t.Fatalf("a short prefix was redacted: %q", got)
	}
}

// downValidator always fails: a validator outage.
type downValidator struct{}

func (downValidator) Validate(context.Context, string) (*identity.Token, error) {
	return nil, errors.New("dashboard down")
}

func (e *connEnv) withValidator(v identity.Validator) {
	h := NewHTTPHandlers(e.m, e.m.logger)
	h.SetIdentityResolver(&identity.Resolver{Logger: zap.NewNop(), Validator: v})
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	e.router = r
}

// Fix 3: a validator outage used to serve the call unattributed, so a server
// with tenant connections ran on its default credentials.
func TestSecRev_ValidatorOutageFailsClosed(t *testing.T) {
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("h", secretA))
	e.withValidator(downValidator{})

	if rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusServiceUnavailable || e.up.count() != 0 {
		t.Fatalf("outage on a server with connections = %d, upstream calls %d", rec.Code, e.up.count())
	}
	// A server declared to take connections, with none stored yet: still refused.
	e2 := newConnEnv(t)
	e2.declare(t, ConnKindHeader)
	e2.withValidator(downValidator{})
	if rec := e2.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("outage on a declared server = %d", rec.Code)
	}
	// AIQG unreachable for the server list too: refused.
	e3 := newConnEnv(t)
	e3.res.serversErr = errors.New("down")
	e3.withValidator(downValidator{})
	if rec := e3.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("outage with an unknown server list = %d", rec.Code)
	}
	// Observe mode holds where nothing can be injected: an outage on a server
	// that takes no connections, and an untokened call on one that does.
	e4 := newConnEnv(t)
	e4.withValidator(downValidator{})
	if rec := e4.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusOK {
		t.Fatalf("outage on a server without connections = %d", rec.Code)
	}
	if rec := e.call(t, srvPlat, callBody("query", nil), nil); rec.Code != http.StatusOK || e.up.lastSeen(t).Auth != "" {
		t.Fatalf("untokened call = %d", rec.Code)
	}
}

// Fix 4: a caller-supplied _connection used to reach a DSN server whenever
// the tenant had no connection of its own, so any caller chose the database.
func TestSecRev_CallerDSNStripped(t *testing.T) {
	attack := map[string]interface{}{"host": "tas-postgres-shared.tas-shared.svc", "port": 5432}
	args := func() map[string]interface{} {
		return map[string]interface{}{"sql": selectOne, "_connection": attack, "dsn": "postgres://x@10.0.0.1/db"}
	}

	e := newConnEnv(t)
	e.declare(t, ConnKindDatabaseDSN)
	for _, tok := range []string{tokenA, tokenC, ""} { // no connection, another tenant, unattributed
		if rec := e.call(t, srvPlat, callBody("query", args()), as(tok)); rec.Code != http.StatusOK {
			t.Fatalf("%q: %d %s", tok, rec.Code, rec.Body)
		}
		got := e.up.lastSeen(t)
		if _, ok := got.Args["_connection"]; ok || got.Args["sql"] != selectOne {
			t.Fatalf("caller %q: upstream args %v", tok, got.Args)
		}
	}
	// A tenant's connection with a custom argument name makes that name
	// stripped for every caller of the server.
	e.res.set(tenantB, srvPlat, &Connection{ID: "b", Kind: ConnKindDatabaseDSN, Version: "1",
		Secret: "postgres://b:pw-bbbbbbbb@pg.vendor.example/b", Params: map[string]string{"argument": "dsn", "format": "string"}})
	_ = e.call(t, srvPlat, callBody("query", args()), as(tokenA))
	if _, ok := e.up.lastSeen(t).Args["dsn"]; ok {
		t.Fatal("caller dsn argument reached the upstream")
	}
	// Undeclared, with no DSN connection anywhere: arguments pass as before.
	e2 := newConnEnv(t)
	_ = e2.call(t, srvPlat, callBody("query", args()), as(tokenA))
	if _, ok := e2.up.lastSeen(t).Args["_connection"]; !ok {
		t.Fatal("a server that takes no DSN lost its caller's arguments")
	}
	// The server list unknown: the default name is stripped defensively.
	e2.res.serversErr = errors.New("down")
	_ = e2.call(t, srvPlat, callBody("query", args()), nil)
	if _, ok := e2.up.lastSeen(t).Args["_connection"]; ok {
		t.Fatal("_connection passed while the server list was unknown")
	}
}

// Fix 4: a stored DSN's host goes through Q13 (cluster infrastructure and
// unlisted hosts refused), with and without the tenant registry.
func TestSecRev_DSNHostEgress(t *testing.T) {
	for _, c := range []struct {
		name, host string
		nilPolicy  bool
		ok         bool
	}{
		{"shared postgres in tas-shared", "tas-postgres-shared.tas-shared.svc", false, false},
		{"cluster.local form", "tas-postgres-shared.tas-shared.svc.cluster.local", false, false},
		{"pod DNS", "10-0-0-5.default.pod.cluster.local", false, false},
		{"bare name", "postgres", false, false},
		{"unlisted external", "db.attacker.example", false, false},
		{"node IP", "10.0.0.1", false, false},
		{"allowed namespace", "tenantdb.tas-mcp-servers.svc", false, true},
		{"allowlisted external", "pg.vendor.example", false, true},
		{"registry off: IP literal", "127.0.0.1", true, false},
		{"registry off: svc", "x.tas-shared.svc", true, false},
		{"registry off: public name", "db.example.com", true, true},
	} {
		e := newConnEnv(t)
		if c.nilPolicy {
			e.m.connections.Policy = nil
		}
		e.res.set(tenantA, srvPlat, &Connection{ID: "d", Kind: ConnKindDatabaseDSN, Version: "1",
			Secret: "postgres://u:pw-12345678@" + c.host + ":5432/db"})
		rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA))
		if c.ok != (rec.Code == http.StatusOK) || (!c.ok && e.up.count() != 0) {
			t.Errorf("%s: %d (upstream calls %d)", c.name, rec.Code, e.up.count())
		}
		if c.ok && e.up.lastSeen(t).Tenant != tenantA {
			t.Errorf("%s: bridge tenant header = %q", c.name, e.up.lastSeen(t).Tenant)
		}
	}
	// A header connection does not send the tenant header.
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("h", secretA))
	_ = e.call(t, srvPlat, callBody("query", nil), as(tokenA))
	if e.up.lastSeen(t).Tenant != "" {
		t.Fatal("tenant header sent with a header connection")
	}
}

// Fix 5: the OAuth client refuses redirects (the target never sees the
// secret), and without the tenant registry a token URL may not be an IP
// literal or a cluster name.
func TestSecRev_OAuthRedirectsAndHosts(t *testing.T) {
	var hit atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"stolen","token_type":"bearer"}`))
	}))
	defer target.Close()
	redir := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/token", http.StatusTemporaryRedirect)
	}))
	defer redir.Close()
	e := newConnEnv(t)
	e.m.connections.HTTP = redir.Client()
	e.res.set(tenantA, srvPlat, &Connection{ID: "o", Kind: ConnKindOAuthClientCredentials, Version: "1",
		Secret: oauthSecret, Params: map[string]string{"client_id": "cid", "token_url": redir.URL}})
	if rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("redirecting token endpoint = %d", rec.Code)
	}
	if hit.Load() != 0 || e.up.count() != 0 {
		t.Fatalf("redirect followed (%d) or upstream called (%d)", hit.Load(), e.up.count())
	}

	ci := &ConnectionInjector{}
	for _, u := range []string{"https://127.0.0.1/t", "https://[::1]/t", "https://169.254.169.254/t", "https://kc.tas-shared.svc/t",
		"https://kc.tas-shared.svc.cluster.local/t", "https://localhost/t", "https://keycloak/t", "http://idp.example.com/t"} {
		if err := ci.checkTokenURL(tenantA, u); err == nil {
			t.Errorf("registry off: %s accepted", u)
		}
	}
	if err := ci.checkTokenURL(tenantA, "https://idp.example.com/oauth/token"); err != nil {
		t.Errorf("registry off: public token URL refused: %v", err)
	}
}

// suspendedValidator resolves every bearer to tenantA, suspended.
type suspendedValidator struct{}

func (suspendedValidator) Validate(context.Context, string) (*identity.Token, error) {
	return &identity.Token{TokenID: "tok-s", TenantID: tenantA, Suspended: true}, identity.ErrTokenSuspended
}

// Fix 8: a suspended account's stored connection used to be injected.
func TestSecRev_SuspendedNeverInjected(t *testing.T) {
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, headerConn("h", secretA))
	e.withValidator(suspendedValidator{})
	if rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusForbidden || e.up.count() != 0 {
		t.Fatalf("suspended with a connection = %d, upstream %d", rec.Code, e.up.count())
	}
	// Declared, no connection: refused too (it would run on defaults).
	e2 := newConnEnv(t)
	e2.declare(t, ConnKindDatabaseDSN)
	e2.withValidator(suspendedValidator{})
	if rec := e2.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusForbidden {
		t.Fatalf("suspended on a declared server = %d", rec.Code)
	}
	// No connection anywhere: observe mode, served.
	e3 := newConnEnv(t)
	e3.withValidator(suspendedValidator{})
	if rec := e3.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusOK {
		t.Fatalf("suspended on a server without connections = %d", rec.Code)
	}
}

// Fix 4/3: MCP_CONNECTION_SERVERS declares a platform server's kind.
func TestSecRev_DeclaredByConfig(t *testing.T) {
	e := newConnEnv(t)
	e.m.connections.Declared = map[string]string{srvPlat: ConnKindDatabaseDSN}
	_ = e.call(t, srvPlat, callBody("query", map[string]interface{}{"_connection": map[string]interface{}{"host": "x"}}), nil)
	if _, ok := e.up.lastSeen(t).Args["_connection"]; ok {
		t.Fatal("declared DSN server received a caller _connection")
	}
	e.withValidator(downValidator{})
	if rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("outage on a config-declared server = %d", rec.Code)
	}
}
