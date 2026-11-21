// Package identity resolves who is calling the MCP federation proxy (Plan #22
// T.1, design §9.5 "Caller identity at the proxy").
//
// Until now the proxy knew nothing about its caller: no tenant, user or token.
// Callers now send the same TAS-Auth token the LLM gateway accepts, plus W3C
// `baggage` and the TAS-* agent headers. The proxy resolves the token through
// aiqg-dashboard-be's POST /internal/auth/validate — the endpoint, request and
// response shape tas-llm-router's DashboardResolver uses — behind a short cache,
// and carries the result in the request context so the tool-call event (T.2)
// can stamp it.
//
// Mode is OBSERVE only. A call with no token, a malformed token, an unknown
// token, or a token the validator could not check (outage) is still served; it
// is recorded as (unattributed). Refusing such calls is a later per-tenant
// setting (open question Q12) and deliberately not implemented here.
package identity

import "context"

// Unattributed is the label an operator view shows for a call whose caller
// could not be established. Events carry empty identity fields instead of this
// string; it exists for logs and UIs.
const Unattributed = "(unattributed)"

// Resolution results. A closed set: it is a Prometheus label.
const (
	// ResultAttributed — the token resolved to an active tenant.
	ResultAttributed = "attributed"
	// ResultSuspended — the token resolved, but its account is suspended. Still
	// attributed (the tenant is known); in observe mode it is still served.
	ResultSuspended = "suspended"
	// ResultNoToken — no TAS-Auth header.
	ResultNoToken = "no_token"
	// ResultMalformed — TAS-Auth present but not a tas_qg_live_ token, so the
	// validator was not called.
	ResultMalformed = "malformed"
	// ResultUnknown — the validator answered 401: no such token.
	ResultUnknown = "unknown"
	// ResultValidatorUnavailable — the validator could not be reached or
	// answered unexpectedly. Fail-open: the call is served, unattributed.
	ResultValidatorUnavailable = "validator_unavailable"
	// ResultValidatorUnconfigured — identity is enabled but no validator URL
	// or internal-auth token is configured.
	ResultValidatorUnconfigured = "validator_unconfigured"
)

// Identity is the resolved caller of one proxy request.
//
// The token-derived fields (TenantID, TokenID, AIQGAccountID, OwnerSub, Label)
// are authenticated: the bearer proves the token. The header-derived fields
// (UserID from baggage, AgentID, FlowID, StepID, a TAS-Source-App override) are
// self-asserted by the caller, exactly as at the LLM gateway.
type Identity struct {
	// Attributed is true when the token resolved (active or suspended). When
	// false every identity field below is empty — an unauthenticated caller's
	// self-asserted user or agent id is not recorded against anyone.
	Attributed bool
	// Result is one of the Result* constants: why the call is or is not
	// attributed.
	Result string

	TenantID      string
	TokenID       string
	AIQGAccountID string
	OwnerSub      string
	Label         string
	Suspended     bool

	// SourceApp is the TAS-Source-App header when present (≤128 chars), else
	// the token's source_app — the gateway's precedence.
	SourceApp string
	// UserID is W3C baggage `user.id`.
	UserID string
	// SessionID is W3C baggage `session.id`.
	SessionID string
	// AgentID is TAS-Agent-Id, falling back to the OTel gen_ai.agent.id header.
	AgentID string
	// FlowID is TAS-Flow-Id.
	FlowID string
	// StepID is TAS-Step-Id.
	StepID string
	// ToolCallID is TAS-Tool-Call-Id: the LLM's tool_call_id for the call this
	// request executes, when the agent forwards it. It is what lets the proxy's
	// event join the gateway's on tool_call_id (T.7). Carried here, not in the
	// MCP body, so a client needs no protocol change to send it. Recorded for
	// unattributed calls too — it is a join key, not an identity claim.
	ToolCallID string
}

type ctxKey struct{}

// WithIdentity returns ctx carrying id.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	if id == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the identity stored by WithIdentity, or nil when the
// request never went through identity resolution (identity disabled).
func FromContext(ctx context.Context) *Identity {
	if ctx == nil {
		return nil
	}
	id, _ := ctx.Value(ctxKey{}).(*Identity)
	return id
}
