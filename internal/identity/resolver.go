package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"go.uber.org/zap"
)

// liveShapePrefix is the shape of an AIQG gateway token. A TAS-Auth value without
// it is malformed and never sent to the validator (the gateway rejects the
// same values with ErrAuthMalformed).
const liveShapePrefix = "tas_qg_live_"

// maxSourceAppLen matches the gateway's TAS-Source-App limit.
const maxSourceAppLen = 128

// baggageKVParts is key and value of one baggage member.
const baggageKVParts = 2

// maxHeaderValueLen caps every self-asserted header we copy into an event.
const maxHeaderValueLen = 256

// Resolver turns request headers into an Identity. Mode is observe: it never
// fails a request, it only reports what it could establish.
type Resolver struct {
	// Validator resolves the bearer. Nil means identity is enabled but no
	// validator is configured: every token resolves validator_unconfigured.
	Validator Validator
	Logger    *zap.Logger
}

// Resolve reads TAS-Auth, baggage and the TAS-* agent headers. It always
// returns a non-nil Identity.
func (r *Resolver) Resolve(ctx context.Context, h http.Header) *Identity {
	id := r.resolve(ctx, h)
	resolutions.WithLabelValues(id.Result).Inc()
	return id
}

func (r *Resolver) resolve(ctx context.Context, h http.Header) *Identity {
	toolCallID := clip(h.Get("TAS-Tool-Call-Id"))
	bearer := strings.TrimSpace(h.Get("TAS-Auth"))
	if bearer == "" {
		return &Identity{Result: ResultNoToken, ToolCallID: toolCallID}
	}
	if !strings.HasPrefix(bearer, liveShapePrefix) {
		return &Identity{Result: ResultMalformed, ToolCallID: toolCallID}
	}
	if r == nil || r.Validator == nil {
		return &Identity{Result: ResultValidatorUnconfigured, ToolCallID: toolCallID}
	}

	tok, err := r.Validator.Validate(ctx, bearer)
	result := ResultAttributed
	switch {
	case err == nil && tok != nil:
	case errors.Is(err, ErrTokenSuspended) && tok != nil:
		result = ResultSuspended
	case errors.Is(err, ErrTokenNotFound):
		return &Identity{Result: ResultUnknown, ToolCallID: toolCallID}
	default:
		// Fail-open: serve the call, unattributed, and count it. The bearer
		// is never logged; the error text comes from our own client.
		if r.Logger != nil {
			r.Logger.Warn("identity: validator unavailable; serving call unattributed", zap.Error(err))
		}
		return &Identity{Result: ResultValidatorUnavailable, ToolCallID: toolCallID}
	}

	bag := ParseBaggage(h.Get("baggage"))
	id := &Identity{
		Attributed:    true,
		Result:        result,
		TenantID:      tok.TenantID,
		TokenID:       tok.TokenID,
		AIQGAccountID: tok.AIQGAccountID,
		OwnerSub:      tok.OwnerSub,
		Label:         tok.Label,
		Suspended:     tok.Suspended || result == ResultSuspended,
		SourceApp:     tok.SourceApp,
		UserID:        clip(bag["user.id"]),
		SessionID:     clip(bag["session.id"]),
		AgentID:       clip(firstNonEmpty(h.Get("TAS-Agent-Id"), h.Get("gen_ai.agent.id"))),
		FlowID:        clip(h.Get("TAS-Flow-Id")),
		StepID:        clip(h.Get("TAS-Step-Id")),
		ToolCallID:    toolCallID,
	}
	// TAS-Source-App overrides the token's source app, as at the gateway. An
	// over-long value is ignored rather than truncated.
	if sa := strings.TrimSpace(h.Get("TAS-Source-App")); sa != "" && len(sa) <= maxSourceAppLen {
		id.SourceApp = sa
	}
	return id
}

// Middleware resolves the caller and stores the Identity in the request
// context. It never rejects a request (observe mode).
func (r *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := r.Resolve(req.Context(), req.Header)
		next.ServeHTTP(w, req.WithContext(WithIdentity(req.Context(), id)))
	})
}

// ParseBaggage parses a W3C baggage header into key → value. Same rules as
// tas-llm-router's parseBaggage: ;-properties dropped, malformed pairs skipped,
// values percent-decoded. Returns nil for an empty header.
func ParseBaggage(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		if i := strings.IndexByte(pair, ';'); i >= 0 {
			pair = pair[:i]
		}
		kv := strings.SplitN(pair, "=", baggageKVParts)
		if len(kv) != baggageKVParts {
			continue
		}
		k, v := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
		if k == "" || v == "" {
			continue
		}
		if dv, err := url.QueryUnescape(v); err == nil {
			v = dv
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// clip trims s and caps it at maxHeaderValueLen.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxHeaderValueLen {
		return s[:maxHeaderValueLen]
	}
	return s
}
