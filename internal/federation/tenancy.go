package federation

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/tributary-ai-services/tas-mcp/internal/identity"
)

// AdminTokenHeader carries MCP_ADMIN_TOKEN on the federation write routes
// (register, update, delete) when MCP_TENANT_SERVERS is on. A request that
// carries it on a read route also sees every row, every tenant's included.
const AdminTokenHeader = "TAS-MCP-Admin-Token" // #nosec G101 -- a header name, not a credential

// Tenancy errors. The HTTP handlers map them to status codes (statusFor).
var (
	// ErrTenantForbidden — the caller's token does not resolve to the tenant
	// that owns the server. Never says which tenant does.
	ErrTenantForbidden = errors.New("forbidden: this server belongs to a tenant; call it with that tenant's TAS-Auth token")
	// ErrServerDisabled — the server is registered but switched off.
	ErrServerDisabled = errors.New("forbidden: server is disabled")
	// ErrToolNotAllowed — the tool is outside the server's allowed_tools.
	ErrToolNotAllowed = errors.New("forbidden: tool is not in this server's allowed_tools")
	// ErrInvalidServer — the definition breaks a tenancy rule (bad
	// managed_by, a tenant row managed by the operator, a stdio tenant server).
	ErrInvalidServer = errors.New("invalid server definition")
	// ErrEgressDenied — a tenant server outside the cluster is not HTTPS, or
	// its host is not on that tenant's egress allowlist (Q13).
	ErrEgressDenied = errors.New("egress denied")
)

const schemeHTTPS = "https"

// DefaultTenantClusterNamespaces is where an in-cluster tenant server may live.
// It matches what MCP-14's NetworkPolicy lets the proxy reach.
var DefaultTenantClusterNamespaces = []string{"tas-mcp-servers"}

// TenantPolicy is the tenant-scoped registry (Plan #22 C.1, design §9.6). A nil
// *TenantPolicy — MCP_TENANT_SERVERS off — means none of it applies: every
// caller sees and may call every server, and writes need no token, exactly as
// before C.1.
type TenantPolicy struct {
	// AdminToken is MCP_ADMIN_TOKEN, required on register, update and delete
	// in AdminTokenHeader. Empty = every write is refused (503): the flag is
	// on, so failing open would let anyone write.
	AdminToken string
	// Egress is the per-tenant allowlist of hosts outside the cluster (Q13):
	// tenant id -> host names. "*.example.com" matches any subdomain of
	// example.com (not example.com itself). Matching is on the host name only,
	// case-insensitive; the port is not part of it.
	Egress map[string][]string
	// ClusterNamespaces are the namespaces an in-cluster tenant server may be
	// in (host <svc>.<ns>.svc[.cluster.local]). Nil = the default.
	ClusterNamespaces []string
}

// callerTenant is the tenant a request's TAS-Auth token resolved to, or ""
// when the call is unattributed (no, unknown or unverifiable token, or identity
// off). It never reads the request body or a self-asserted header.
func callerTenant(id *identity.Identity) string {
	if id == nil || !id.Attributed {
		return ""
	}
	return id.TenantID
}

// Visible reports whether a caller sees s in list and get: an enabled server
// that is a platform server or belongs to the caller's own tenant.
func (p *TenantPolicy) Visible(s *MCPServer, id *identity.Identity) bool {
	if p == nil {
		return true
	}
	if s == nil || !s.IsEnabled() {
		return false
	}
	return s.IsPlatform() || s.TenantID == callerTenant(id)
}

// CheckInvoke decides whether a caller may invoke s at all (any method).
func (p *TenantPolicy) CheckInvoke(s *MCPServer, id *identity.Identity) error {
	if p == nil {
		return nil
	}
	if !s.IsEnabled() {
		return ErrServerDisabled
	}
	if s.IsPlatform() {
		return nil // observe-only default for platform servers (Q12)
	}
	if t := callerTenant(id); t == "" || t != s.TenantID {
		return ErrTenantForbidden
	}
	return nil
}

// Normalize writes the lazily-migrated defaults explicitly: managed_by (the
// operator for a platform row, AIQG for a tenant row) and enabled.
func (p *TenantPolicy) Normalize(s *MCPServer) {
	if p == nil || s == nil {
		return
	}
	if s.ManagedBy == "" {
		if s.IsPlatform() {
			s.ManagedBy = ManagedByOperator
		} else {
			s.ManagedBy = ManagedByAIQG
		}
	}
	if s.Enabled == nil {
		on := true
		s.Enabled = &on
	}
}

// Validate checks a definition about to be written. Call Normalize first.
func (p *TenantPolicy) Validate(s *MCPServer) error {
	if p == nil {
		return nil
	}
	switch s.ManagedBy {
	case ManagedByOperator, ManagedByAIQG:
	default:
		return fmt.Errorf("%w: managed_by %q must be %q or %q", ErrInvalidServer, s.ManagedBy, ManagedByOperator, ManagedByAIQG)
	}
	if s.IsPlatform() {
		return nil
	}
	if s.ManagedBy != ManagedByAIQG {
		return fmt.Errorf("%w: a tenant server is managed_by %q; the operator manages platform servers only", ErrInvalidServer, ManagedByAIQG)
	}
	if s.Protocol != ProtocolHTTP && s.Protocol != ProtocolSSE {
		// stdio would run a command on the proxy; grpc has no URL to check.
		return fmt.Errorf("%w: a tenant server's protocol must be %q or %q, not %q", ErrInvalidServer, ProtocolHTTP, ProtocolSSE, s.Protocol)
	}
	return p.checkEgress(s.TenantID, s.Endpoint)
}

// checkEgress is Q13: a tenant server inside the cluster must be in an allowed
// namespace; one outside must be HTTPS with its host on the tenant's allowlist.
func (p *TenantPolicy) checkEgress(tenant, endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: endpoint must be an absolute http(s) URL", ErrEgressDenied)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != schemeHTTPS {
		return fmt.Errorf("%w: endpoint scheme %q is not http(s)", ErrEgressDenied, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: endpoint must not carry credentials", ErrEgressDenied)
	}
	return p.checkHost(tenant, u.Hostname(), scheme == schemeHTTPS, true)
}

// checkHost is the host half of Q13, shared by tenant server endpoints, OAuth
// token URLs and database DSNs (Plan #22 C.4): an in-cluster host must be a
// service in an allowed namespace; anything else must be on the tenant's
// allowlist (and use TLS when needTLS). An IP literal counts as outside the
// cluster, so it passes only when it is on the allowlist itself.
func (p *TenantPolicy) checkHost(tenant, rawHost string, tls, needTLS bool) error {
	host := strings.ToLower(strings.TrimSuffix(rawHost, "."))
	if host == "" {
		return fmt.Errorf("%w: no host", ErrEgressDenied)
	}

	if ns, ok := clusterNamespace(host); ok {
		for _, allowed := range p.clusterNamespaces() {
			if ns == allowed {
				return nil
			}
		}
		return fmt.Errorf("%w: in-cluster tenant servers must be in namespace %s, not %q",
			ErrEgressDenied, strings.Join(p.clusterNamespaces(), ", "), ns)
	}
	if !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return fmt.Errorf("%w: host %q is a bare name; use <service>.<namespace>.svc", ErrEgressDenied, host)
	}
	if isClusterInternal(host) {
		return fmt.Errorf("%w: host %q is cluster-internal; use <service>.<namespace>.svc in an allowed namespace", ErrEgressDenied, host)
	}
	if needTLS && !tls {
		return fmt.Errorf("%w: a tenant server outside the cluster must use https", ErrEgressDenied)
	}
	if !hostAllowed(host, p.Egress[tenant]) {
		return fmt.Errorf("%w: host %q is not on this tenant's egress allowlist (MCP_TENANT_EGRESS_ALLOWLIST)", ErrEgressDenied, host)
	}
	return nil
}

// isClusterInternal reports a host that names cluster infrastructure in a form
// clusterNamespace does not accept (a pod or headless name under .svc,
// anything under .cluster.local, localhost).
func isClusterInternal(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".svc") ||
		strings.HasSuffix(host, ".cluster.local") || strings.HasSuffix(host, ".internal")
}

// basicHostCheck is the egress rule when the tenant registry is off (no
// allowlist to consult): a public DNS name only. IP literals, bare names and
// cluster-internal names are refused, so a tenant-supplied URL cannot point
// the proxy at the cluster or the node.
func basicHostCheck(rawHost string) error {
	host := strings.ToLower(strings.TrimSuffix(rawHost, "."))
	switch {
	case host == "":
		return fmt.Errorf("%w: no host", ErrEgressDenied)
	case net.ParseIP(strings.Trim(host, "[]")) != nil:
		return fmt.Errorf("%w: IP literal %q is not allowed", ErrEgressDenied, host)
	case !strings.Contains(host, "."):
		return fmt.Errorf("%w: bare host name %q is not allowed", ErrEgressDenied, host)
	case isClusterInternal(host):
		return fmt.Errorf("%w: cluster-internal host %q is not allowed", ErrEgressDenied, host)
	}
	return nil
}

func (p *TenantPolicy) clusterNamespaces() []string {
	if len(p.ClusterNamespaces) == 0 {
		return DefaultTenantClusterNamespaces
	}
	return p.ClusterNamespaces
}

// clusterNamespace returns the namespace of a Kubernetes service host
// (<svc>.<ns>.svc or <svc>.<ns>.svc.cluster.local).
func clusterNamespace(host string) (string, bool) {
	h := strings.TrimSuffix(host, ".cluster.local")
	if !strings.HasSuffix(h, ".svc") {
		return "", false
	}
	labels := strings.Split(strings.TrimSuffix(h, ".svc"), ".")
	if len(labels) != 2 || labels[0] == "" || labels[1] == "" {
		return "", false
	}
	return labels[1], true
}

func hostAllowed(host string, allow []string) bool {
	for _, a := range allow {
		a = strings.ToLower(strings.TrimSpace(a))
		switch {
		case a == "":
		case strings.HasPrefix(a, "*."):
			// a[1:] keeps the dot, so the apex itself never matches.
			if strings.HasSuffix(host, a[1:]) {
				return true
			}
		case host == a:
			return true
		}
	}
	return false
}

// AdminStatus checks the admin token on r: 0 when it is present and right,
// else the status to refuse with (503 unconfigured, 401 missing, 403 wrong).
func (p *TenantPolicy) AdminStatus(r *http.Request) int {
	if p.AdminToken == "" {
		return http.StatusServiceUnavailable
	}
	got := r.Header.Get(AdminTokenHeader)
	if got == "" {
		return http.StatusUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(p.AdminToken)) != 1 {
		return http.StatusForbidden
	}
	return 0
}

// IsAdmin reports whether r carries the right admin token.
func (p *TenantPolicy) IsAdmin(r *http.Request) bool {
	return p != nil && p.AdminStatus(r) == 0
}

// filterToolsList drops tools outside s's allowlist from a tools/list result
// ({"tools": [{"name": ...}, ...]}). The response is copied, not mutated in
// place; anything not shaped like a tools/list result passes through.
func filterToolsList(resp *MCPResponse, s *MCPServer) *MCPResponse {
	if resp == nil || s == nil || len(s.AllowedTools) == 0 {
		return resp
	}
	res, ok := resp.Result.(map[string]interface{})
	if !ok {
		return resp
	}
	tools, ok := res["tools"].([]interface{})
	if !ok {
		return resp
	}
	kept := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		if name, _ := tm["name"].(string); s.ToolAllowed(name) {
			kept = append(kept, t)
		}
	}
	out := make(map[string]interface{}, len(res))
	for k, v := range res {
		out[k] = v
	}
	out["tools"] = kept
	cp := *resp
	cp.Result = out
	return &cp
}
