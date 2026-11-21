package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Validator errors. Mirror tas-llm-router pkg/aiqg/tokens so the two services
// read the same HTTP semantics the same way.
var (
	// ErrTokenNotFound — dashboard-be answered 401 token_unknown.
	ErrTokenNotFound = errors.New("identity: token not found")
	// ErrTokenSuspended — dashboard-be answered 403; the Token is still returned.
	ErrTokenSuspended = errors.New("identity: account suspended")
)

// Token is what /internal/auth/validate returns for a known bearer.
type Token struct {
	TokenID       string
	TenantID      string
	AIQGAccountID string
	SourceApp     string
	Label         string
	OwnerSub      string
	Suspended     bool
}

// Validator resolves a TAS-Auth bearer to a Token.
//
// Contract (same as the gateway's tokens.Resolver):
//   - (tok, nil)               known, active
//   - (nil, ErrTokenNotFound)  unknown
//   - (tok, ErrTokenSuspended) known, suspended
//   - (nil, other error)       could not check (outage, misconfig)
type Validator interface {
	Validate(ctx context.Context, bearer string) (*Token, error)
}

// DefaultValidateTimeout matches tas-llm-router's DashboardResolver: 2 s, the
// budget a cold BestEffort dashboard-be replica needs on its first call.
const DefaultValidateTimeout = 2 * time.Second

// maxErrorBody bounds how much of a 401 body is read to classify it.
const maxErrorBody = 4096

// validatePath is appended to the configured dashboard-be base URL.
const validatePath = "/internal/auth/validate"

// DashboardValidator calls aiqg-dashboard-be POST /internal/auth/validate. It
// mirrors tas-llm-router pkg/aiqg/tokens/dashboard_resolver.go: body
// {"bearer":…}, header Internal-Auth, and the response fields token_id,
// tenant_id, aiqg_account_id, source_app, suspended, label, owner_sub.
type DashboardValidator struct {
	HTTP              *http.Client
	BaseURL           string
	InternalAuthToken string
}

// NewDashboardValidator builds a validator against baseURL (everything before
// /internal/auth/validate, e.g. http://aiqg-dashboard-be.aiqg.svc.cluster.local:8095).
func NewDashboardValidator(baseURL, internalAuthToken string) (*DashboardValidator, error) {
	if baseURL == "" {
		return nil, errors.New("identity.NewDashboardValidator: baseURL required")
	}
	if internalAuthToken == "" {
		return nil, errors.New("identity.NewDashboardValidator: internalAuthToken required (else dashboard-be rejects)")
	}
	return &DashboardValidator{
		HTTP:              &http.Client{Timeout: DefaultValidateTimeout},
		BaseURL:           strings.TrimRight(baseURL, "/"),
		InternalAuthToken: internalAuthToken,
	}, nil
}

type validateRequest struct {
	Bearer string `json:"bearer"`
}

type validateResponse struct {
	TokenID       string `json:"token_id"`
	TenantID      string `json:"tenant_id"`
	AIQGAccountID string `json:"aiqg_account_id"`
	SourceApp     string `json:"source_app"`
	Suspended     bool   `json:"suspended"`
	Label         string `json:"label"`
	OwnerSub      string `json:"owner_sub"`
}

// Validate implements Validator.
func (v *DashboardValidator) Validate(ctx context.Context, bearer string) (*Token, error) {
	if bearer == "" {
		return nil, ErrTokenNotFound
	}
	body, err := json.Marshal(validateRequest{Bearer: bearer})
	if err != nil {
		return nil, fmt.Errorf("identity: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.BaseURL+validatePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("identity: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Internal-Auth", v.InternalAuthToken)

	resp, err := v.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("identity: validate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var r validateResponse
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return nil, fmt.Errorf("identity: decode 200: %w", err)
		}
		return &Token{
			TokenID: r.TokenID, TenantID: r.TenantID, AIQGAccountID: r.AIQGAccountID,
			SourceApp: r.SourceApp, Label: r.Label, OwnerSub: r.OwnerSub, Suspended: r.Suspended,
		}, nil
	case http.StatusUnauthorized:
		// 401 is both "token_unknown" and our own Internal-Auth being
		// rejected. The latter is misconfiguration on this side, not a bad
		// customer token, so it must not be cached as "unknown".
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if bytes.Contains(b, []byte("internal_auth")) {
			return nil, errors.New("identity: dashboard-be rejected Internal-Auth (check AIQG_DASHBOARD_INTERNAL_AUTH_TOKEN)")
		}
		return nil, ErrTokenNotFound
	case http.StatusForbidden:
		var p validateResponse
		_ = json.NewDecoder(resp.Body).Decode(&p)
		return &Token{
			TokenID: p.TokenID, TenantID: p.TenantID, AIQGAccountID: p.AIQGAccountID,
			Label: p.Label, OwnerSub: p.OwnerSub, Suspended: true,
		}, ErrTokenSuspended
	default:
		return nil, fmt.Errorf("identity: validate: unexpected status %d", resp.StatusCode)
	}
}

// DefaultCacheTTL is the "same short cache" design §9.5 asks for.
const DefaultCacheTTL = 60 * time.Second

// defaultCacheMax bounds the cache. Distinct live tokens are in the tens; the
// bound exists so a caller cycling random bearers cannot grow it without limit.
const defaultCacheMax = 10000

type cacheEntry struct {
	tok     *Token
	err     error // nil, ErrTokenNotFound or ErrTokenSuspended — never a transport error
	expires time.Time
}

// CachingValidator caches definitive answers (active, unknown, suspended) for
// TTL. Transport and unexpected-status errors are NOT cached: an outage must
// not pin a valid token as unattributed for a minute after the validator
// recovers. Keys are sha256 of the bearer, so the raw secret is never held as a
// map key.
type CachingValidator struct {
	Inner Validator
	TTL   time.Duration
	Max   int
	Now   func() time.Time

	mu      sync.Mutex
	entries map[[sha256.Size]byte]cacheEntry
}

// NewCachingValidator wraps inner with a TTL cache (ttl<=0 → DefaultCacheTTL).
func NewCachingValidator(inner Validator, ttl time.Duration) *CachingValidator {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &CachingValidator{Inner: inner, TTL: ttl, Max: defaultCacheMax, Now: time.Now}
}

// Validate implements Validator.
func (c *CachingValidator) Validate(ctx context.Context, bearer string) (*Token, error) {
	key := sha256.Sum256([]byte(bearer))
	now := c.Now()

	c.mu.Lock()
	if e, ok := c.entries[key]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		cacheLookups.WithLabelValues("hit").Inc()
		return e.tok, e.err
	}
	c.mu.Unlock()
	cacheLookups.WithLabelValues("miss").Inc()

	tok, err := c.Inner.Validate(ctx, bearer)
	if err != nil && !errors.Is(err, ErrTokenNotFound) && !errors.Is(err, ErrTokenSuspended) {
		return tok, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[[sha256.Size]byte]cacheEntry)
	}
	if c.Max > 0 && len(c.entries) >= c.Max {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.Max {
			c.entries = make(map[[sha256.Size]byte]cacheEntry)
		}
	}
	c.entries[key] = cacheEntry{tok: tok, err: err, expires: now.Add(c.TTL)}
	return tok, err
}
