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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
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
// (nil, nil) when the tenant has none. Servers lists every server that any
// tenant holds a live connection for, with the kinds and the tool arguments
// those connections inject; the proxy uses it to fail closed and to strip
// caller-supplied DSN arguments on calls it cannot attribute.
type ConnectionResolver interface {
	Resolve(ctx context.Context, tenantID, serverID string) (*Connection, error)
	Servers(ctx context.Context) (map[string]ServerConnections, error)
}

// ServerConnections is what the proxy knows of one server's connections
// across every tenant. It carries no tenant and no secret.
type ServerConnections struct {
	Kinds     []string `json:"kinds"`
	Arguments []string `json:"arguments,omitempty"`
}

// connectionsResolvePath and connectionsServersPath are appended to
// aiqg-dashboard-be's base URL.
const (
	connectionsResolvePath = "/internal/mcp/connections/resolve"
	connectionsServersPath = "/internal/mcp/connections/servers"
)

// ResolveTokenHeader carries MCP_CONNECTIONS_RESOLVE_TOKEN, a token that
// serves the connection endpoints only (not the shared INTERNAL_AUTH_TOKEN).
const ResolveTokenHeader = "Internal-Auth" // #nosec G101 -- a header name, not a credential

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
		return nil, errors.New("federation: connection resolver needs MCP_CONNECTIONS_RESOLVE_TOKEN")
	}
	return &DashboardConnectionResolver{
		HTTP:              &http.Client{Timeout: DefaultConnectionResolveTimeout},
		BaseURL:           strings.TrimRight(baseURL, "/"),
		InternalAuthToken: internalAuthToken,
	}, nil
}

func (d *DashboardConnectionResolver) post(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// BaseURL is operator config (AIQG_DASHBOARD_URL), never request input.
	// #nosec G704 -- the URL is fixed at startup from config, not caller-controlled
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ResolveTokenHeader, d.InternalAuthToken)
	return d.HTTP.Do(req) // #nosec G704 -- see above
}

// Servers implements ConnectionResolver.
func (d *DashboardConnectionResolver) Servers(ctx context.Context) (map[string]ServerConnections, error) {
	resp, err := d.post(ctx, connectionsServersPath, map[string]string{})
	if err != nil {
		return nil, fmt.Errorf("connection servers: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("connection servers: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		Servers map[string]ServerConnections `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("connection servers: decode: %w", err)
	}
	if out.Servers == nil {
		out.Servers = map[string]ServerConnections{}
	}
	return out.Servers, nil
}

// Resolve implements ConnectionResolver.
func (d *DashboardConnectionResolver) Resolve(ctx context.Context, tenantID, serverID string) (*Connection, error) {
	resp, err := d.post(ctx, connectionsResolvePath, map[string]string{"tenant_id": tenantID, "server_id": serverID})
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

	mu         sync.Mutex
	entries    map[string]connCacheEntry
	servers    map[string]ServerConnections
	serversExp time.Time
}

// Servers implements ConnectionResolver, cached for TTL; errors are not cached.
func (c *CachingConnectionResolver) Servers(ctx context.Context) (map[string]ServerConnections, error) {
	now := c.Now()
	c.mu.Lock()
	if c.servers != nil && now.Before(c.serversExp) {
		out := c.servers
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()
	out, err := c.Inner.Servers(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.servers, c.serversExp = out, now.Add(c.TTL)
	c.mu.Unlock()
	return out, nil
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

// TenantHeader tells a database bridge which tenant an injected DSN belongs
// to, so it never reuses one tenant's session for another.
const TenantHeader = "TAS-Tenant-Id"

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

// ErrAccountSuspended — the caller's token belongs to a suspended account and
// the call would use a stored connection. Matches aiqg-dashboard-be's
// data-plane 403 (account_suspended): a suspended tenant's credentials are
// never used.
var ErrAccountSuspended = errors.New("forbidden: account suspended; its stored connections are not used")

// ConnectionInjector applies connections to calls.
type ConnectionInjector struct {
	Resolver ConnectionResolver
	// Declared maps a server id to the connection kind it takes
	// (MCP_CONNECTION_SERVERS, for platform servers whose CR cannot say so).
	// A tenant server declares its kind in its record (connection_kind).
	Declared map[string]string
	// Policy checks an OAuth token URL the way a tenant server endpoint is
	// checked (Q13). Nil = https only.
	Policy *TenantPolicy
	// DSNAllow is MCP_DSN_EGRESS_ALLOW: the host:port pairs every tenant's
	// stored DSN may name, on top of Policy's rule (MCP-19, DSNRule).
	DSNAllow []HostPort
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

// capability is what the proxy knows of a server's connections: the kinds it
// takes and the tool arguments a DSN connection injects. known is false when
// the cross-tenant list could not be read (AIQG unreachable).
type capability struct {
	kinds     map[string]bool
	arguments []string
	known     bool
}

func (c capability) takesConnections() bool { return len(c.kinds) > 0 }

// capabilityOf merges the server's declared kind with every tenant's live
// connections for it.
func (ci *ConnectionInjector) capabilityOf(ctx context.Context, srv *MCPServer, serverID string) capability {
	c := capability{kinds: map[string]bool{}, known: true}
	declare := func(kind string) {
		if kind == "" || kind == "none" {
			return
		}
		c.kinds[kind] = true
		if kind == ConnKindDatabaseDSN && !slices.Contains(c.arguments, defaultConnArgument) {
			c.arguments = append(c.arguments, defaultConnArgument)
		}
	}
	if srv != nil {
		declare(srv.ConnectionKind)
	}
	declare(ci.Declared[serverID])
	all, err := ci.Resolver.Servers(ctx)
	if err != nil {
		c.known = false
		return c
	}
	if sc, ok := all[serverID]; ok {
		for _, k := range sc.Kinds {
			declare(k)
		}
		for _, a := range sc.Arguments {
			if a != "" && !slices.Contains(c.arguments, a) {
				c.arguments = append(c.arguments, a)
			}
		}
	}
	return c
}

// withoutArguments returns a copy of a tools/call request without the named
// arguments (nil when none was present). A caller never chooses the database
// a DSN server connects to: only a stored, egress-checked connection does.
func withoutArguments(req *MCPRequest, names []string) *MCPRequest {
	args, ok := req.Params["arguments"].(map[string]interface{})
	if !ok {
		return nil
	}
	found := false
	for _, n := range names {
		if _, ok := args[n]; ok {
			found = true
		}
	}
	if !found {
		return nil
	}
	c := *req
	c.Params = make(map[string]interface{}, len(req.Params))
	for k, v := range req.Params {
		c.Params[k] = v
	}
	clean := make(map[string]interface{}, len(args))
	for k, v := range args {
		if !slices.Contains(names, k) {
			clean[k] = v
		}
	}
	c.Params["arguments"] = clean
	return &c
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
		value, secrets, host, port, err := dsnValue(conn.Secret, paramOr(conn.Params, "format", connFormatObject))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrConnectionUnavailable, err)
		}
		inj.red.add(secrets...)
		// Q13 for the database host too: a stored DSN must not point the
		// server at cluster infrastructure or an unlisted host (SSRF). MCP-19:
		// by host:port, so the platform list can open one service in
		// tas-shared without the rest of the namespace.
		if err := ci.checkDSNHost(tenantID, host, port); err != nil {
			return nil, fmt.Errorf("%w: database host: %w", ErrConnectionUnavailable, err)
		}
		inj.req = withToolArgument(req, paramOr(conn.Params, "argument", defaultConnArgument), value)
		// The bridge keys its database sessions by tenant as well as by the
		// full connection (tas-mcp-servers bridge). Sent for DSNs only: the
		// server is ours or the tenant's own database proxy.
		inj.headers = http.Header{TenantHeader: {tenantID}}
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

// dsnValue turns a DSN into the injected argument, lists the values to
// redact (the DSN and its password, raw and escaped), and returns the host and
// port the server will dial (the scheme's default port when none is given; 0
// for an unknown scheme without a port, which the egress rule refuses).
func dsnValue(dsn, format string) (value interface{}, secrets []string, host string, port int, err error) {
	u, perr := url.Parse(dsn)
	if perr != nil || u.Scheme == "" || u.Host == "" {
		return nil, nil, "", 0, errors.New("database DSN is not a URL")
	}
	pw, _ := u.User.Password()
	secrets = []string{dsn, pw}
	host = u.Hostname()
	scheme := strings.ToLower(u.Scheme)
	port = defaultDBPorts[scheme]
	if p := u.Port(); p != "" {
		n, aerr := strconv.Atoi(p)
		if aerr != nil {
			return nil, nil, "", 0, errors.New("database DSN port is not a number")
		}
		port = n
	}
	if format == connFormatString {
		return dsn, secrets, host, port, nil
	}
	if format != connFormatObject {
		return nil, nil, "", 0, fmt.Errorf("database DSN format %q must be %q or %q", format, connFormatObject, connFormatString)
	}
	return dbConnection{
		Host: u.Hostname(), Port: port, Username: u.User.Username(), Password: pw,
		Database: strings.TrimPrefix(u.Path, "/"), Protocol: scheme, SSLMode: u.Query().Get("sslmode"),
	}, secrets, host, port, nil
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
	hc := &http.Client{Timeout: oauthTimeout}
	if ci.HTTP != nil {
		c := *ci.HTTP
		hc = &c
	}
	// Never follow a redirect: the token URL passed the egress check, the
	// URL it redirects to did not (and would receive the client secret).
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return errOAuthRedirect }
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

// errOAuthRedirect refuses a token endpoint's redirect.
var errOAuthRedirect = errors.New("oauth token endpoint redirected; redirects are refused")

// checkTokenURL refuses a token URL the proxy should not call: https always;
// with the tenant registry on, the same egress rule as a tenant server
// endpoint (Q13); otherwise a public DNS name only (basicHostCheck).
func (ci *ConnectionInjector) checkTokenURL(tenantID, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != schemeHTTPS || u.Host == "" || u.User != nil {
		return fmt.Errorf("%w: token_url must be an https URL without credentials", ErrEgressDenied)
	}
	if ci.Policy != nil {
		return ci.Policy.checkEgress(tenantID, raw)
	}
	return basicHostCheck(u.Hostname())
}

// checkDSNHost applies the DSN egress rule (MCP-19, DSNRule) to a database
// host and port.
func (ci *ConnectionInjector) checkDSNHost(tenantID, host string, port int) error {
	return DSNRule{Platform: ci.DSNAllow, Policy: ci.Policy}.Check(tenantID, host, port)
}

// redactor removes injected values from everything the proxy produces.
//
// Each secret is redacted in every form an upstream is likely to echo it in:
// raw; JSON-escaped (with and without Go's HTML escaping of <, > and &);
// URL query- and path-escaped; and base64 (standard and URL, padded and raw).
// A string truncated for a preview has any prefix of 8 or more characters of
// a secret at its cut redacted too.
type redactor struct {
	forms []string // every form of every secret, longest first
}

// minCutPrefix is the shortest secret prefix redacted at a truncation cut.
const minCutPrefix = 8

// secretForms returns v and the encodings it may be echoed in.
func secretForms(v string) []string {
	forms := []string{v, url.QueryEscape(v), url.PathEscape(v),
		base64.StdEncoding.EncodeToString([]byte(v)), base64.URLEncoding.EncodeToString([]byte(v)),
		base64.RawStdEncoding.EncodeToString([]byte(v)), base64.RawURLEncoding.EncodeToString([]byte(v))}
	if b, err := json.Marshal(v); err == nil {
		forms = append(forms, string(b[1:len(b)-1])) // HTML-escaped (\u003c)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) == nil {
		b := bytes.TrimSpace(buf.Bytes())
		forms = append(forms, string(b[1:len(b)-1]))
	}
	return forms
}

func (r *redactor) add(vals ...string) {
	for _, v := range vals {
		if len(v) < minRedactLen {
			continue
		}
		for _, f := range secretForms(v) {
			if len(f) < minRedactLen || slices.Contains(r.forms, f) {
				continue
			}
			r.forms = append(r.forms, f)
		}
	}
	// Longest first, so a DSN is replaced whole before its password is.
	sort.SliceStable(r.forms, func(i, j int) bool { return len(r.forms[i]) > len(r.forms[j]) })
}

func (r *redactor) str(s string) string {
	if r == nil {
		return s
	}
	for _, v := range r.forms {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, redactedMarker)
		}
	}
	return s
}

// cut redacts a secret prefix (at least minCutPrefix long) that a truncation
// left at the end of s.
func (r *redactor) cut(s string) string {
	if r == nil {
		return s
	}
	for _, v := range r.forms {
		for k := len(v) - 1; k >= minCutPrefix; k-- {
			if strings.HasSuffix(s, v[:k]) {
				return s[:len(s)-k] + redactedMarker
			}
		}
	}
	return s
}

// preview is the redacted, truncated form of an upstream body for an error:
// redact the whole body first, then truncate, then redact a prefix at the cut.
func (r *redactor) preview(data []byte, n int) string {
	s := r.str(string(data))
	if len(s) > n {
		s = r.cut(s[:n])
	}
	return s
}

type redactorKey struct{}

// withRedactor carries the call's redactor to the upstream clients, which
// build error text from upstream bodies before the manager sees it.
func withRedactor(ctx context.Context, r *redactor) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, redactorKey{}, r)
}

// bodyPreview is what an upstream client puts in an error: the body,
// redacted for this call's injected values, then truncated.
func bodyPreview(ctx context.Context, data []byte) string {
	r, _ := ctx.Value(redactorKey{}).(*redactor)
	return r.preview(data, errBodyPreview)
}

func (r *redactor) value(v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return r.str(t)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, x := range t {
			out[r.str(k)] = r.value(x)
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
	if r == nil || resp == nil || len(r.forms) == 0 {
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
	if r == nil || err == nil || len(r.forms) == 0 {
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
