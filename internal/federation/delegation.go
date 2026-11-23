package federation

// The delegate credential (Plan #22 AIQG-88).
//
// One service (aether-be) holds one credential, MCP_DELEGATE_TOKEN, which it
// sends in TAS-MCP-Delegate-Token. It is accepted for exactly one thing, on
// exactly one listener: POST /api/v1/federation/servers/{id}/invoke with
// method tools/list or tools/call, on the delegated listener
// (MCP_DELEGATION_PORT, 8086), whose NetworkPolicy admits aether-backend pods
// only. The main listener refuses any request that carries it (403), so it
// can never register, update or delete a server, read the admin view, or
// broadcast, and a pod that is not aether-backend cannot use it even with the
// value. It is not a TAS-Auth token: it never has the tas_qg_live_ shape
// (startup refuses one that does), so the proxy's identity resolver and the
// LLM gateway both treat it as malformed if it is ever sent there.
//
// Header presence is what counts: a repeated delegation header, or a TAS-Auth
// or admin header next to the credential (even empty), is a 400.
//
// A delegated call names who it acts for:
//
//	TAS-MCP-On-Behalf-Of-Org:    the Aether organization id      (required)
//	TAS-MCP-On-Behalf-Of-User:   the acting user's Keycloak sub   (required)
//	TAS-MCP-On-Behalf-Of-Tenant: the AIQG tenant uuid             (optional)
//	TAS-MCP-Expected-Connection-Id: the stored connection the
//	                             caller's record is bound to     (optional; 409 on mismatch)
//
// The proxy asks aiqg-dashboard-be (POST /internal/mcp/delegation/verify,
// Internal-Auth MCP_DELEGATION_VERIFY_TOKEN) which tenant the org is linked
// to, whether a named tenant matches that link, and whether the user is a
// member of the org in AIQG's copy of Aether's membership. Only then is the
// call attributed to that tenant, which is what connection injection keys on.
// Any refusal is a 403; any failure to get an answer is a 503. Nothing is
// injected and the upstream server is not contacted in either case.
//
// Answers (allowed and refused) are cached for MCP_DELEGATION_CACHE_TTL
// (default 60 s), keyed by a hash of (org, tenant, user); errors are never
// cached.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Delegation headers.
const (
	DelegateTokenHeader  = "TAS-MCP-Delegate-Token" // #nosec G101 -- a header name, not a credential
	OnBehalfOfOrgHeader  = "TAS-MCP-On-Behalf-Of-Org"
	OnBehalfOfUserHeader = "TAS-MCP-On-Behalf-Of-User"
)

// gatewayTokenShape is the prefix of an AIQG gateway (TAS-Auth) token. A
// delegate token must never have it.
const gatewayTokenShape = "tas_qg_live_" // #nosec G101 -- the public prefix of a gateway token, not a credential

// minDelegateTokenLen is the shortest delegate token accepted at startup.
const minDelegateTokenLen = 32

// maxDelegationAnswer bounds the verify answer read.
const maxDelegationAnswer = 16 << 10

// defaultDelegationCacheTTL matches the identity validator's 60 s cache.
const defaultDelegationCacheTTL = 60 * time.Second

// delegationVerifyPath is appended to AIQG_DASHBOARD_URL.
const delegationVerifyPath = "/internal/mcp/delegation/verify"

// Errors.
var (
	// ErrDelegationRefused — AIQG answered with a definitive refusal: the org
	// is not linked, the tenant does not match the link, the user is not a
	// member, or the account is suspended. Its Code says which.
	ErrDelegationRefused = errors.New("delegation refused")
	// ErrDelegationUnavailable — AIQG could not answer. Fail closed: 503.
	ErrDelegationUnavailable = errors.New("delegation could not be verified")
)

// refusalUnspecified is the code of a refusal that named none.
const refusalUnspecified = "refused"

// DelegationRefusal carries AIQG's refusal code. It wraps ErrDelegationRefused.
type DelegationRefusal struct{ Code string }

func (r *DelegationRefusal) Error() string { return "delegation refused: " + r.Code }

// Unwrap lets errors.Is match ErrDelegationRefused.
func (r *DelegationRefusal) Unwrap() error { return ErrDelegationRefused }

// DelegationGrant is a verified delegation.
type DelegationGrant struct {
	TenantID    string `json:"tenant_id"`
	AetherOrgID string `json:"aether_org_id"`
	UserSub     string `json:"user_sub"`
	Role        string `json:"role,omitempty"`
	Suspended   bool   `json:"suspended"`
}

// DelegationVerifier answers whether user may act for org (and tenant, when
// named).
type DelegationVerifier interface {
	Verify(ctx context.Context, org, tenant, user string) (*DelegationGrant, error)
}

// Delegation is the proxy's delegate configuration. Nil on HTTPHandlers =
// MCP_DELEGATION_ENABLED off: the delegate headers mean nothing, exactly as
// before.
type Delegation struct {
	tokenSum [sha256.Size]byte
	Name     string
	Verifier DelegationVerifier
}

// NewDelegation validates the configuration. It refuses a token that could
// be mistaken for a gateway token, a short one, or one equal to the admin
// token.
func NewDelegation(token, name, adminToken string, v DelegationVerifier) (*Delegation, error) {
	switch {
	case v == nil:
		return nil, errors.New("delegation: no verifier (AIQG_DASHBOARD_URL and MCP_DELEGATION_VERIFY_TOKEN)")
	case len(token) < minDelegateTokenLen:
		return nil, fmt.Errorf("delegation: MCP_DELEGATE_TOKEN must be at least %d characters", minDelegateTokenLen)
	case strings.HasPrefix(token, gatewayTokenShape):
		return nil, errors.New("delegation: MCP_DELEGATE_TOKEN must not look like a gateway token (tas_qg_live_)")
	case adminToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(adminToken)) == 1:
		return nil, errors.New("delegation: MCP_DELEGATE_TOKEN must differ from MCP_ADMIN_TOKEN")
	}
	if strings.TrimSpace(name) == "" {
		name = "delegate"
	}
	return &Delegation{tokenSum: sha256.Sum256([]byte(token)), Name: name, Verifier: v}, nil
}

// Matches reports, in constant time, whether presented is the delegate token.
// Both sides are hashed first, so the comparison does not leak the length.
func (d *Delegation) Matches(presented string) bool {
	if d == nil || presented == "" {
		return false
	}
	sum := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(sum[:], d.tokenSum[:]) == 1
}

// ExpectedConnectionHeader names the stored connection a delegated call
// expects the proxy to inject (AIQG-88 LOW-2). aether-be sends the id its
// Database row is bound to; a different connection, or none, is a 409, so
// replacing a tenant's connection in AIQG can never silently repoint a row.
const ExpectedConnectionHeader = "TAS-MCP-Expected-Connection-Id"

// HasDelegateHeader reports whether r carries the delegate credential header
// at all, with any value, empty included.
func HasDelegateHeader(r *http.Request) bool {
	return len(r.Header.Values(DelegateTokenHeader)) > 0
}

// headerPresent reports whether name appears at all, with any value.
func headerPresent(r *http.Request, name string) bool {
	return len(r.Header.Values(name)) > 0
}

// singleHeader returns the one value of name, and false when it is absent
// or repeated (a repeated delegation header is ambiguous, never "the first").
func singleHeader(r *http.Request, name string) (string, bool) {
	v := r.Header.Values(name)
	if len(v) != 1 {
		return "", false
	}
	return strings.TrimSpace(v[0]), true
}

// RefuseDelegateHeader wraps the main listener: with delegation on, a request
// that carries the delegate credential is refused on every route, invoke
// included. Delegated calls are served only by the delegated listener
// (DelegatedHandler, MCP_DELEGATION_PORT), whose NetworkPolicy admits
// aether-backend pods only. With delegation off (d == nil) it passes
// everything through unchanged.
func RefuseDelegateHeader(d *Delegation, next http.Handler) http.Handler {
	if d == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if HasDelegateHeader(r) {
			delegationOutcomes.WithLabelValues("wrong_listener").Inc()
			http.Error(w, "the delegate credential is accepted only on the delegated listener", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SubHash is how a user sub appears in logs: a short sha256, so refusals
// cannot be mined from Loki for valid (org, user) pairs.
func SubHash(sub string) string {
	sum := sha256.Sum256([]byte("tas-mcp-delegation-sub\x00" + sub))
	return hex.EncodeToString(sum[:6])
}

// DelegableMethod reports whether an MCP method may run on a delegated call.
func DelegableMethod(m string) bool { return m == methodToolsList || m == methodToolsCall }

// header value rules: org and user ids are opaque but bounded.
var delegateIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]{0,127}$`)

// ValidDelegateID reports whether s may be sent as an org or user id.
func ValidDelegateID(s string) bool { return delegateIDRe.MatchString(s) }

// ---- AIQG verifier ----------------------------------------------------------

// DashboardDelegationVerifier calls aiqg-dashboard-be.
type DashboardDelegationVerifier struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

// DefaultDelegationTimeout matches the identity validator's.
const DefaultDelegationTimeout = 2 * time.Second

// NewDashboardDelegationVerifier builds the verifier.
func NewDashboardDelegationVerifier(baseURL, token string) (*DashboardDelegationVerifier, error) {
	if baseURL == "" || token == "" {
		return nil, errors.New("delegation verifier needs AIQG_DASHBOARD_URL and MCP_DELEGATION_VERIFY_TOKEN")
	}
	return &DashboardDelegationVerifier{HTTP: &http.Client{Timeout: DefaultDelegationTimeout, CheckRedirect: refuseRedirect},
		BaseURL: strings.TrimRight(baseURL, "/"), Token: token}, nil
}

// refuseRedirect never follows a redirect from the verify endpoint: the
// target passed no check and would receive the Internal-Auth token.
func refuseRedirect(*http.Request, []*http.Request) error {
	return errors.New("delegation verify redirected; redirects are refused")
}

type delegationVerifyBody struct {
	AetherOrgID string `json:"aether_org_id"`
	TenantID    string `json:"tenant_id,omitempty"`
	UserSub     string `json:"user_sub"`
}

// Verify implements DelegationVerifier.
func (v *DashboardDelegationVerifier) Verify(ctx context.Context, org, tenant, user string) (*DelegationGrant, error) {
	body, err := json.Marshal(delegationVerifyBody{AetherOrgID: org, TenantID: tenant, UserSub: user})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDelegationUnavailable, err)
	}
	// #nosec G704 -- BaseURL is operator config (AIQG_DASHBOARD_URL), not caller input
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.BaseURL+delegationVerifyPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDelegationUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Internal-Auth", v.Token)
	resp, err := v.HTTP.Do(req) // #nosec G704 -- see above
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDelegationUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxDelegationAnswer))

	switch resp.StatusCode {
	case http.StatusOK:
		var g DelegationGrant
		if err := json.Unmarshal(raw, &g); err != nil || g.TenantID == "" || g.UserSub != user || g.AetherOrgID != org {
			// An answer that does not echo what was asked is not an answer.
			return nil, fmt.Errorf("%w: malformed verify answer", ErrDelegationUnavailable)
		}
		if tenant != "" && !strings.EqualFold(g.TenantID, tenant) {
			return nil, &DelegationRefusal{Code: "tenant_mismatch"}
		}
		g.TenantID = strings.ToLower(g.TenantID)
		return &g, nil
	case http.StatusForbidden, http.StatusNotFound:
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		code := e.Error.Code
		if code == "" {
			code = refusalUnspecified
		}
		return nil, &DelegationRefusal{Code: code}
	default:
		// 401 here is our own Internal-Auth being rejected: misconfiguration,
		// never a refusal of the user, so it is not cached.
		return nil, fmt.Errorf("%w: verify answered %d", ErrDelegationUnavailable, resp.StatusCode)
	}
}

// ---- cache ------------------------------------------------------------------

type delegationEntry struct {
	grant   *DelegationGrant
	err     error // nil or a *DelegationRefusal; never an availability error
	expires time.Time
}

const delegationCacheMax = 10000

// CachingDelegationVerifier caches definitive answers for TTL.
type CachingDelegationVerifier struct {
	Inner DelegationVerifier
	TTL   time.Duration
	Now   func() time.Time

	mu      sync.Mutex
	entries map[[sha256.Size]byte]delegationEntry
}

// NewCachingDelegationVerifier wraps inner (ttl<=0 → 60 s).
func NewCachingDelegationVerifier(inner DelegationVerifier, ttl time.Duration) *CachingDelegationVerifier {
	if ttl <= 0 {
		ttl = defaultDelegationCacheTTL
	}
	return &CachingDelegationVerifier{Inner: inner, TTL: ttl, Now: time.Now}
}

// Verify implements DelegationVerifier.
func (c *CachingDelegationVerifier) Verify(ctx context.Context, org, tenant, user string) (*DelegationGrant, error) {
	key := sha256.Sum256([]byte(org + "\x00" + strings.ToLower(tenant) + "\x00" + user))
	now := c.Now()
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.grant, e.err
	}
	c.mu.Unlock()

	g, err := c.Inner.Verify(ctx, org, tenant, user)
	if err != nil && !errors.Is(err, ErrDelegationRefused) {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil || len(c.entries) >= delegationCacheMax {
		c.entries = map[[sha256.Size]byte]delegationEntry{}
	}
	c.entries[key] = delegationEntry{grant: g, err: err, expires: now.Add(c.TTL)}
	return g, err
}

// delegationOutcomes counts delegated-call decisions, by outcome: a closed
// set (verified, bad_token, ambiguous, bad_headers, refused, unavailable,
// wrong_method, wrong_listener, not_delegated).
var delegationOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tas_mcp",
	Name:      "delegation_decisions_total",
	Help:      "Delegate-credential decisions at the MCP proxy (AIQG-88), by outcome.",
}, []string{"outcome"})
