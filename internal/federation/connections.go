package federation

// Per-tenant connection injection (Plan #22 C.4, design §9.6 "Connections").
//
// A connection is one tenant's credentials and parameters for one server: a
// header (an API key), OAuth client credentials (a CRM), or a database DSN
// (dbhub). AIQG stores them encrypted and is the only place they are written.
// On each invoke the proxy resolves the caller's connection for the server
// through aiqg-dashboard-be's /internal/mcp/connections/resolve, caches it in
// memory for 60 s (never persisted), and injects it the way the server expects.
// This is the server-side form of aether-be's injectConnectionParams.
//
// Three rules hold everywhere in this file:
//
//   - The tenant comes from the caller's resolved TAS-Auth token (identity,
//     T.1), never from the request body or a self-asserted header.
//   - A connection is keyed by (tenant_id, server_id) and nothing else.
//   - An injected value never reaches the proxy's own outputs: it is added to
//     a copy of the request that only the upstream call sees, and every
//     injected value is redacted from the response, the error, the logs, the
//     scan findings and the tool-call event (see redactor).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Connection kinds (Q14: a database DSN and OAuth client credentials ship
// first; a plain header covers API keys).
const (
	// ConnKindHeader puts the secret in one request header: params "header"
	// (default Authorization) and "prefix" (e.g. "Bearer ").
	ConnKindHeader = "header"
	// ConnKindOAuthClientCredentials runs the client-credentials grant with
	// params "client_id", "token_url", optional "scopes" (space-separated) and
	// "audience"; the secret is the client secret. The access token goes in
	// Authorization: Bearer (or params "header").
	ConnKindOAuthClientCredentials = "oauth_client_credentials" // #nosec G101 -- a kind name, not a credential
	// ConnKindDatabaseDSN puts the database into the tools/call arguments, as
	// aether-be's injectConnectionParams does: params "argument" (default
	// "_connection") and "format" ("object", the default, aether-be's
	// connectionInfo shape; or "string", the DSN itself). The secret is the DSN.
	ConnKindDatabaseDSN = "database_dsn"
)

// Defaults for the injection parameters.
const (
	defaultConnArgument = "_connection"
	connFormatObject    = "object"
	connFormatString    = "string"
	redactedMarker      = "[REDACTED]"
	// minRedactLen is the shortest injected value that is redacted. Shorter
	// values (a 3-character password) would rewrite ordinary output wherever
	// those characters happen to occur.
	minRedactLen = 4
)

// DefaultConnectionCacheTTL is how long a resolved connection is reused
// (design §9.6: "caches it in memory for 60 seconds"). Rotation takes effect
// within this window.
const DefaultConnectionCacheTTL = 60 * time.Second

// ErrConnectionUnavailable — the caller's connection could not be resolved
// (AIQG unreachable, a bad answer, or a connection that cannot be applied).
// The call fails closed rather than running without the tenant's connection,
// which could reach the server's default target instead of the tenant's.
var ErrConnectionUnavailable = errors.New("connection unavailable: the tenant's connection for this server could not be resolved")

// Connection is one tenant's resolved connection for one server. Secret is
// the only sensitive field; String and GoString never print it.
type Connection struct {
	ID      string            `json:"connection_id"`
	Kind    string            `json:"kind"`
	Version string            `json:"version"`
	Params  map[string]string `json:"params,omitempty"`
	Secret  string            `json:"secret"`
}

// String implements fmt.Stringer without the secret, so a stray %v or zap.Any
// cannot log it.
func (c *Connection) String() string {
	if c == nil {
		return "<nil connection>"
	}
	return fmt.Sprintf("connection{id=%s kind=%s version=%s}", c.ID, c.Kind, c.Version)
}

// GoString implements fmt.GoStringer without the secret.
func (c *Connection) GoString() string { return c.String() }

// ConnectionResolver returns the caller tenant's connection for a server, or
// (nil, nil) when the tenant has none.
type ConnectionResolver interface {
	Resolve(ctx context.Context, tenantID, serverID string) (*Connection, error)
}

// connectionsResolvePath is appended to aiqg-dashboard-be's base URL.
const connectionsResolvePath = "/internal/mcp/connections/resolve"

// DefaultConnectionResolveTimeout bounds one resolve call (same budget as the
// identity validator: a cold BestEffort dashboard-be replica).
const DefaultConnectionResolveTimeout = 2 * time.Second

// DashboardConnectionResolver calls aiqg-dashboard-be POST
// /internal/mcp/connections/resolve with the service token (Internal-Auth),
// the same pattern as identity.DashboardValidator. Body {tenant_id,
// server_id}; 200 = the connection, 404 = none.
type DashboardConnectionResolver struct {
	HTTP              *http.Client
	BaseURL           string
	InternalAuthToken string
}

// NewDashboardConnectionResolver builds a resolver against baseURL.
func NewDashboardConnectionResolver(baseURL, internalAuthToken string) (*DashboardConnectionResolver, error) {
	if baseURL == "" {
		return nil, errors.New("federation: connection resolver needs the dashboard URL")
	}
	if internalAuthToken == "" {
		return nil, errors.New("federation: connection resolver needs the internal-auth token")
	}
	return &DashboardConnectionResolver{
		HTTP:              &http.Client{Timeout: DefaultConnectionResolveTimeout},
		BaseURL:           strings.TrimRight(baseURL, "/"),
		InternalAuthToken: internalAuthToken,
	}, nil
}

// Resolve implements ConnectionResolver.
func (d *DashboardConnectionResolver) Resolve(ctx context.Context, tenantID, serverID string) (*Connection, error) {
	body, err := json.Marshal(map[string]string{"tenant_id": tenantID, "server_id": serverID})
	if err != nil {
		return nil, err
	}
	// BaseURL is operator config (AIQG_DASHBOARD_URL), never request input.
	// #nosec G704 -- the URL is fixed at startup from config, not caller-controlled
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.BaseURL+connectionsResolvePath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Internal-Auth", d.InternalAuthToken)
	resp, err := d.HTTP.Do(req) // #nosec G704 -- see above
	if err != nil {
		return nil, fmt.Errorf("connection resolve: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		var c Connection
		if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
			return nil, fmt.Errorf("connection resolve: decode: %w", err)
		}
		return &c, nil
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("connection resolve: unexpected status %d", resp.StatusCode)
	}
}

// defaultConnectionCacheMax bounds the cache; (tenant, server) pairs number in
// the hundreds at most.
const defaultConnectionCacheMax = 10000

type connCacheEntry struct {
	conn    *Connection // nil = the tenant has none (cached too)
	expires time.Time
}

// CachingConnectionResolver caches definitive answers (a connection, or none)
// for TTL in memory. Errors are not cached, so an AIQG outage does not pin a
// tenant as connection-less after it recovers.
type CachingConnectionResolver struct {
	Inner ConnectionResolver
	TTL   time.Duration
	Max   int
	Now   func() time.Time

	mu      sync.Mutex
	entries map[string]connCacheEntry
}

// NewCachingConnectionResolver wraps inner (ttl<=0 → DefaultConnectionCacheTTL).
func NewCachingConnectionResolver(inner ConnectionResolver, ttl time.Duration) *CachingConnectionResolver {
	if ttl <= 0 {
		ttl = DefaultConnectionCacheTTL
	}
	return &CachingConnectionResolver{Inner: inner, TTL: ttl, Max: defaultConnectionCacheMax, Now: time.Now}
}

// connKey is the only key a connection is looked up by.
func connKey(tenantID, serverID string) string { return tenantID + "\x00" + serverID }

// Resolve implements ConnectionResolver.
func (c *CachingConnectionResolver) Resolve(ctx context.Context, tenantID, serverID string) (*Connection, error) {
	key := connKey(tenantID, serverID)
	now := c.Now()
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		connectionCacheLookups.WithLabelValues("hit").Inc()
		return e.conn, nil
	}
	c.mu.Unlock()
	connectionCacheLookups.WithLabelValues("miss").Inc()

	conn, err := c.Inner.Resolve(ctx, tenantID, serverID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]connCacheEntry{}
	}
	if c.Max > 0 && len(c.entries) >= c.Max {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.Max {
			c.entries = map[string]connCacheEntry{}
		}
	}
	c.entries[key] = connCacheEntry{conn: conn, expires: now.Add(c.TTL)}
	return conn, nil
}

// callHeadersKey carries per-call upstream headers (a tenant's connection)
// from InvokeServer to the HTTP and SSE clients.
type callHeadersKey struct{}

func withCallHeaders(ctx context.Context, h http.Header) context.Context {
	if len(h) == 0 {
		return ctx
	}
	return context.WithValue(ctx, callHeadersKey{}, h)
}

// applyCallHeaders sets the per-call headers on an upstream request. They
// replace any header of the same name, including the server's own AuthConfig:
// the tenant's connection wins.
func applyCallHeaders(ctx context.Context, req *http.Request) {
	h, _ := ctx.Value(callHeadersKey{}).(http.Header)
	for k, vs := range h {
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
}

// injection is what one connection adds to one call.
type injection struct {
	headers http.Header
	req     *MCPRequest // the upstream copy; nil = the request is unchanged
	red     *redactor
}

// oauthTokenCache holds access tokens from client-credentials grants, keyed by
// tenant, server, connection id and version, so a rotation fetches a new one.
type oauthTokenCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]oauthEntry
}

type oauthEntry struct {
	token   string
	expires time.Time
}

// oauthEarlyRefresh renews a token this long before it expires.
const oauthEarlyRefresh = 30 * time.Second

// oauthDefaultLifetime is used when a token endpoint gives no expiry.
const oauthDefaultLifetime = 5 * time.Minute

// oauthTimeout bounds one token request.
const oauthTimeout = 10 * time.Second

// ConnectionInjector applies connections to calls.
type ConnectionInjector struct {
	Resolver ConnectionResolver
	// Policy checks an OAuth token URL the way a tenant server endpoint is
	// checked (Q13). Nil = https only.
	Policy *TenantPolicy
	// HTTP is the client for token requests; nil = a default with oauthTimeout.
	HTTP *http.Client
	Now  func() time.Time

	tokens oauthTokenCache
}

func (ci *ConnectionInjector) now() time.Time {
	if ci.Now != nil {
		return ci.Now()
	}
	return time.Now()
}

// prepare resolves tenantID's connection for serverID and builds what it adds
// to req. (nil, nil) = no connection, nothing to inject.
func (ci *ConnectionInjector) prepare(ctx context.Context, tenantID, serverID string, req *MCPRequest) (*injection, error) {
	conn, err := ci.Resolver.Resolve(ctx, tenantID, serverID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnectionUnavailable, err)
	}
	if conn == nil {
		return nil, nil
	}
	inj := &injection{red: &redactor{}}
	switch conn.Kind {
	case ConnKindHeader:
		name := paramOr(conn.Params, "header", "Authorization")
		val := conn.Params["prefix"] + conn.Secret
		inj.headers = http.Header{}
		inj.headers.Set(name, val)
		inj.red.add(conn.Secret, val)
	case ConnKindOAuthClientCredentials:
		tok, err := ci.oauthToken(ctx, tenantID, serverID, conn)
		if err != nil {
			// The token endpoint's error may quote the request; never pass it on.
			return nil, fmt.Errorf("%w: oauth client-credentials grant failed", ErrConnectionUnavailable)
		}
		inj.headers = http.Header{}
		inj.headers.Set(paramOr(conn.Params, "header", "Authorization"), "Bearer "+tok)
		inj.red.add(conn.Secret, tok)
	case ConnKindDatabaseDSN:
		inj.red.add(conn.Secret)
		if req.Method != methodToolsCall {
			return inj, nil // a DSN is a tool argument; other methods carry none
		}
		value, secrets, err := dsnValue(conn.Secret, paramOr(conn.Params, "format", connFormatObject))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrConnectionUnavailable, err)
		}
		inj.red.add(secrets...)
		inj.req = withToolArgument(req, paramOr(conn.Params, "argument", defaultConnArgument), value)
	default:
		return nil, fmt.Errorf("%w: unknown connection kind %q", ErrConnectionUnavailable, conn.Kind)
	}
	return inj, nil
}

func paramOr(p map[string]string, k, def string) string {
	if v := strings.TrimSpace(p[k]); v != "" {
		return v
	}
	return def
}

// withToolArgument returns a copy of a tools/call request with arguments[name]
// set. The original request, which the scanner, the reducer and the tool-call
// event see, is not modified. A caller-supplied argument of the same name is
// replaced: the tenant's stored connection wins.
func withToolArgument(req *MCPRequest, name string, value interface{}) *MCPRequest {
	c := *req
	c.Params = make(map[string]interface{}, len(req.Params)+1)
	for k, v := range req.Params {
		c.Params[k] = v
	}
	args := map[string]interface{}{}
	if orig, ok := req.Params["arguments"].(map[string]interface{}); ok {
		for k, v := range orig {
			args[k] = v
		}
	}
	args[name] = value
	c.Params["arguments"] = args
	return &c
}

// dbConnection is aether-be's connectionInfo (handlers/mcp_handler.go), the
// shape the TAS database MCP servers read from "_connection".
type dbConnection struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	Protocol string `json:"protocol,omitempty"`
	SSLMode  string `json:"ssl_mode,omitempty"`
}

// Default database ports, by DSN scheme.
const (
	portPostgres  = 5432
	portMySQL     = 3306
	portSQLServer = 1433
)

var defaultDBPorts = map[string]int{"postgres": portPostgres, "postgresql": portPostgres,
	"mysql": portMySQL, "mariadb": portMySQL, "sqlserver": portSQLServer}

// dsnValue turns a DSN into the injected argument and lists the values to
// redact (the DSN and its password, raw and escaped).
func dsnValue(dsn, format string) (value interface{}, secrets []string, err error) {
	u, perr := url.Parse(dsn)
	if perr != nil || u.Scheme == "" || u.Host == "" {
		return nil, nil, errors.New("database DSN is not a URL")
	}
	pw, _ := u.User.Password()
	secrets = []string{dsn, pw, url.QueryEscape(pw), url.PathEscape(pw)}
	if format == connFormatString {
		return dsn, secrets, nil
	}
	if format != connFormatObject {
		return nil, nil, fmt.Errorf("database DSN format %q must be %q or %q", format, connFormatObject, connFormatString)
	}
	scheme := strings.ToLower(u.Scheme)
	port := defaultDBPorts[scheme]
	if p := u.Port(); p != "" {
		n, aerr := strconv.Atoi(p)
		if aerr != nil {
			return nil, nil, errors.New("database DSN port is not a number")
		}
		port = n
	}
	return dbConnection{
		Host: u.Hostname(), Port: port, Username: u.User.Username(), Password: pw,
		Database: strings.TrimPrefix(u.Path, "/"), Protocol: scheme, SSLMode: u.Query().Get("sslmode"),
	}, secrets, nil
}

// oauthToken returns a cached or fresh access token for conn.
func (ci *ConnectionInjector) oauthToken(ctx context.Context, tenantID, serverID string, conn *Connection) (string, error) {
	key := sha256.Sum256([]byte(strings.Join([]string{tenantID, serverID, conn.ID, conn.Version}, "\x00")))
	now := ci.now()
	ci.tokens.mu.Lock()
	if e, ok := ci.tokens.entries[key]; ok && now.Before(e.expires) {
		ci.tokens.mu.Unlock()
		return e.token, nil
	}
	ci.tokens.mu.Unlock()

	tokenURL := conn.Params["token_url"]
	if err := ci.checkTokenURL(tenantID, tokenURL); err != nil {
		return "", err
	}
	cfg := clientcredentials.Config{
		ClientID:     conn.Params["client_id"],
		ClientSecret: conn.Secret,
		TokenURL:     tokenURL,
		Scopes:       strings.Fields(conn.Params["scopes"]),
	}
	if a := conn.Params["audience"]; a != "" {
		cfg.EndpointParams = url.Values{"audience": {a}}
	}
	hc := ci.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: oauthTimeout}
	}
	tctx, cancel := context.WithTimeout(context.WithValue(ctx, oauth2.HTTPClient, hc), oauthTimeout)
	defer cancel()
	tok, err := cfg.Token(tctx)
	if err != nil {
		return "", err
	}
	exp := tok.Expiry
	if exp.IsZero() {
		exp = now.Add(oauthDefaultLifetime)
	}
	ci.tokens.mu.Lock()
	defer ci.tokens.mu.Unlock()
	if ci.tokens.entries == nil {
		ci.tokens.entries = map[[sha256.Size]byte]oauthEntry{}
	}
	if len(ci.tokens.entries) >= defaultConnectionCacheMax {
		ci.tokens.entries = map[[sha256.Size]byte]oauthEntry{}
	}
	ci.tokens.entries[key] = oauthEntry{token: tok.AccessToken, expires: exp.Add(-oauthEarlyRefresh)}
	return tok.AccessToken, nil
}

// checkTokenURL refuses a token URL the proxy should not call: with the tenant
// registry on, the same egress rule as a tenant server endpoint (Q13);
// otherwise https only.
func (ci *ConnectionInjector) checkTokenURL(tenantID, raw string) error {
	if ci.Policy != nil {
		return ci.Policy.checkEgress(tenantID, raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != schemeHTTPS || u.Host == "" {
		return fmt.Errorf("%w: token_url must be an https URL", ErrEgressDenied)
	}
	return nil
}

// redactor removes injected values from everything the proxy produces.
type redactor struct{ secrets []string }

func (r *redactor) add(vals ...string) {
	for _, v := range vals {
		if len(v) < minRedactLen {
			continue
		}
		dup := false
		for _, s := range r.secrets {
			if s == v {
				dup = true
				break
			}
		}
		if !dup {
			r.secrets = append(r.secrets, v)
		}
	}
	// Longest first, so a DSN is replaced whole before its password is.
	for i := 1; i < len(r.secrets); i++ {
		for j := i; j > 0 && len(r.secrets[j]) > len(r.secrets[j-1]); j-- {
			r.secrets[j], r.secrets[j-1] = r.secrets[j-1], r.secrets[j]
		}
	}
}

func (r *redactor) str(s string) string {
	if r == nil {
		return s
	}
	for _, v := range r.secrets {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, redactedMarker)
		}
	}
	return s
}

func (r *redactor) value(v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return r.str(t)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, x := range t {
			out[k] = r.value(x)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, x := range t {
			out[i] = r.value(x)
		}
		return out
	case nil, bool, float64, int, int64, json.Number:
		return v
	default:
		// A typed value from a client: redact through JSON.
		b, err := json.Marshal(t)
		if err != nil {
			return v
		}
		if s := r.str(string(b)); s != string(b) {
			var x interface{}
			if json.Unmarshal([]byte(s), &x) == nil {
				return x
			}
			return redactedMarker
		}
		return v
	}
}

func (r *redactor) response(resp *MCPResponse) *MCPResponse {
	if r == nil || resp == nil || len(r.secrets) == 0 {
		return resp
	}
	c := *resp
	c.Result = r.value(resp.Result)
	if resp.Error != nil {
		e := *resp.Error
		e.Message = r.str(e.Message)
		e.Data = r.value(e.Data)
		c.Error = &e
	}
	return &c
}

// redactedError keeps errors.Is/As working on the original while printing the
// redacted text.
type redactedError struct {
	msg   string
	inner error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.inner }

func (r *redactor) err(err error) error {
	if r == nil || err == nil || len(r.secrets) == 0 {
		return err
	}
	if s := r.str(err.Error()); s != err.Error() {
		return &redactedError{msg: s, inner: err}
	}
	return err
}

var (
	// connectionCacheLookups counts connection cache lookups (hit|miss).
	connectionCacheLookups = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tas_mcp",
		Name:      "connection_cache_lookups_total",
		Help:      "Per-tenant connection cache lookups at the MCP proxy, by result (hit|miss).",
	}, []string{"result"})

	// ConnectionInjections counts invokes by what connection injection did:
	// injected (a kind), none (the tenant has no connection), unattributed (no
	// tenant, so nothing is looked up) or failed (refused, fail closed).
	ConnectionInjections = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tas_mcp",
		Name:      "connection_injections_total",
		Help:      "Invokes by per-tenant connection injection result.",
	}, []string{"result"})
)
