package scanning

// Scan once across the boundary (Plan #22 T.3, GK-5b; design §9.5 "Scan
// once: the attestation").
//
// The proxy scans every federated tool result (G2). The gateway then met the
// same text again inside the next LLM request and scanned it again, on every
// turn of the conversation, and counted its findings again each time. The
// gateway's scan-once store (tas-llm-router GK-5a, internal/gatekeeper/
// scanonce.go) already remembers, per tenant, what each message's content
// produced. This file writes the proxy's scan into that same store, under
// the same key, so the gateway's first sighting of a tool result is already
// a hit:
//
//   - clean result      -> the gateway skips the scan
//   - result with hits  -> the gateway re-scans (blocking and redaction still
//     act) but does not count the findings again: the proxy counted them, in
//     its tool-call event
//   - redacted result   -> the delivered (redacted) text is attested too,
//     with what a scan of it finds, so the gateway recognizes it later
//
// Store, key, value and TTL are the gateway's, byte for byte:
//
//	key   gk:scan1:<tenant>:<sha256 hex of NFC, right-trimmed content>:
//	      <profile>:<trust tier int>:<Gatekeeper ruleset digest>:<gateway build>
//	value {"p":["<pattern id>", ...]}   ({} when clean)
//	TTL   24h, set on write
//
// The last term is the gateway's build (its events.GatewayVersion, the git
// short SHA). The gateway folds its build into every key so a deploy re-scans
// everything once; the proxy cannot know it, so it is configuration
// (SCAN_ATTEST_GATEWAY_BUILDS, one key per listed build). A stale value fails
// safe: the gateway misses and scans as before.
//
// The tier term is the GATEWAY's inbound tier, not the proxy's: the gateway
// looks up the tier it scans at (configs/config.yaml inbound.trust_tier,
// "internal" = 0 live on 2026-11-21), while the proxy scans tool results at
// External (2). Gatekeeper's own attestation rule (pkg/attest isTierAdequate)
// is that a scan at a tier covers every lower tier, and the tier changes no
// scan result in pkg/scan, so an External scan may vouch for an Internal
// lookup. A proxy scan below the gateway's tier is never attested.
// SCAN_ATTEST_GATEWAY_TIER, default "internal".
//
// Fail open everywhere. Attest never blocks the tool call (a bounded queue, one
// background writer, drops counted). Any store error is counted and the
// gateway, finding nothing, scans normally. An attestation is only written
// for a result whose every text block scanned without error, for an
// attributed tenant, and under the gateway's default confidence threshold.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Tributary-ai-services/Gatekeeper/pkg/scan"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/text/unicode/norm"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

const (
	// AttestKeyPrefix is the gateway's scan-once key prefix (GK-5a).
	AttestKeyPrefix = "gk:scan1:"
	// DefaultAttestTTL matches the gateway's scan-once TTL.
	DefaultAttestTTL = 24 * time.Hour
	// DefaultAttestBuffer bounds attestations waiting for the store.
	DefaultAttestBuffer = 256
	// attestWriteTimeout matches the gateway's best-effort record budget.
	attestWriteTimeout = 250 * time.Millisecond
	closeAttestTimeout = 2 * time.Second
)

// AttestConfig configures proxy attestations.
type AttestConfig struct {
	Enabled bool
	// RedisURL is the shared scan-once store (the gateway's
	// AIQG_LINKAGE_REDIS_URL / GATEKEEPER_SCAN_ONCE_REDIS_URL).
	RedisURL string
	// GatewayBuilds are the gateway builds (events.GatewayVersion) to write
	// keys for. Empty disables attestation: no key could match.
	GatewayBuilds []string
	// GatewayTier is the gateway's inbound trust tier ("internal",
	// "partner", "external"), the tier term of the key it looks up.
	// Empty = "internal", the live gateway's setting.
	GatewayTier string
	TTL         time.Duration
	BufferSize  int
}

// AttestRecord is the gateway's ScanRecord: the pattern ids found, sorted;
// empty means clean. The JSON shape is the gateway's ({"p": [...]}).
type AttestRecord struct {
	Patterns []string `json:"p,omitempty"`
}

// AttestStore writes records. Implementations treat every error as a miss
// for the reader, so a failed write only costs the gateway a scan.
type AttestStore interface {
	SetMany(ctx context.Context, recs map[string]AttestRecord, ttl time.Duration) error
}

type redisAttestStore struct{ rdb redis.UniversalClient }

// NewRedisAttestStore connects to url (redis://...).
func NewRedisAttestStore(url string) (AttestStore, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &redisAttestStore{rdb: redis.NewClient(opt)}, nil
}

func (s *redisAttestStore) SetMany(ctx context.Context, recs map[string]AttestRecord, ttl time.Duration) error {
	if len(recs) == 0 {
		return nil
	}
	pipe := s.rdb.Pipeline()
	for k, rec := range recs {
		b, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		pipe.Set(ctx, k, b, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// NormalizeForAttestKey is the gateway's normalizeForScanKey: Unicode NFC and
// trailing whitespace trimmed, nothing more.
func NormalizeForAttestKey(s string) string {
	return strings.TrimRightFunc(norm.NFC.String(s), unicode.IsSpace)
}

// AttestKey is the gateway's scanOnceKey.
func AttestKey(tenant, content string, profile scan.ScanProfile, tier scan.TrustTier, ruleset, build string) string {
	sum := sha256.Sum256([]byte(NormalizeForAttestKey(content)))
	return AttestKeyPrefix + tenant + ":" + hex.EncodeToString(sum[:]) + ":" +
		string(profile) + ":" + strconv.Itoa(int(tier)) + ":" + ruleset + ":" + build
}

var attestations = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "tas_mcp",
	Name:      "scan_attestations_total",
	Help: "Scan-once attestations of tool results (GK-5b), by result: " +
		"written, dropped (queue full or closed), failed (store or output scan error).",
}, []string{"result"})

// Attestor is a federation.ScanAttestor writing to the scan-once store.
type Attestor struct {
	store   AttestStore
	scanner *Scanner
	ruleset string
	builds  []string
	gwTier  scan.TrustTier
	ttl     time.Duration
	log     *zap.Logger

	ch     chan federation.ScanAttestation
	done   chan struct{}
	mu     sync.RWMutex
	closed bool
}

// NewAttestor starts an attestor. scanner is the boundary scanner: its
// profile and rule set are what the attestation vouches for, and it re-scans
// redacted output. Returns an error when attestation cannot match the
// gateway (no builds, or a non-default confidence threshold).
//
//nolint:gocritic // startup-only config
func NewAttestor(store AttestStore, scanner *Scanner, cfg AttestConfig, log *zap.Logger) (*Attestor, error) {
	if store == nil || scanner == nil {
		return nil, errors.New("attest: store and scanner required")
	}
	builds := make([]string, 0, len(cfg.GatewayBuilds))
	for _, b := range cfg.GatewayBuilds {
		if b = strings.TrimSpace(b); b != "" {
			builds = append(builds, b)
		}
	}
	if len(builds) == 0 {
		return nil, errors.New("attest: no gateway build configured (SCAN_ATTEST_GATEWAY_BUILDS); no key could match the gateway's")
	}
	if scanner.minConf > 0 {
		// The gateway scans at Gatekeeper's default threshold. A different
		// threshold here could vouch "clean" for content the gateway flags.
		return nil, errors.New("attest: SCANNING_MIN_CONFIDENCE is set; attestations require the gateway's default threshold")
	}
	gwTier := scan.TierInternal
	switch strings.ToLower(strings.TrimSpace(cfg.GatewayTier)) {
	case "", "internal":
	case "partner":
		gwTier = scan.TierPartner
	case "external":
		gwTier = scan.TierExternal
	default:
		return nil, errors.New("attest: SCAN_ATTEST_GATEWAY_TIER must be internal, partner or external")
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultAttestTTL
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = DefaultAttestBuffer
	}
	if log == nil {
		log = zap.NewNop()
	}
	a := &Attestor{
		store:   store,
		scanner: scanner,
		ruleset: scanner.RulesetVersion(),
		builds:  builds,
		gwTier:  gwTier,
		ttl:     cfg.TTL,
		log:     log,
		ch:      make(chan federation.ScanAttestation, cfg.BufferSize),
		done:    make(chan struct{}),
	}
	go a.run()
	return a, nil
}

// RulesetVersion is the rule-set digest this attestor writes under.
func (a *Attestor) RulesetVersion() string { return a.ruleset }

// Attest implements federation.ScanAttestor. Never blocks.
func (a *Attestor) Attest(_ context.Context, at federation.ScanAttestation) { //nolint:gocritic // interface signature
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		attestations.WithLabelValues("dropped").Inc()
		return
	}
	select {
	case a.ch <- at:
	default:
		attestations.WithLabelValues("dropped").Inc()
	}
}

func (a *Attestor) run() {
	defer close(a.done)
	for at := range a.ch {
		a.write(at)
	}
}

// Records builds the keys and records for one attestation: the scanned
// content, and when redacted the delivered output, each under every
// configured gateway build. It may re-scan the output.
//
//nolint:gocritic // value by design
func (a *Attestor) Records(ctx context.Context, at federation.ScanAttestation) (map[string]AttestRecord, error) {
	recs := map[string]AttestRecord{}
	if toGatekeeperTier(at.Tier) < a.gwTier {
		// Scanned less strictly than the gateway asks for: not adequate.
		return recs, nil
	}
	tier := a.gwTier
	put := func(content string, patterns []string) {
		for _, b := range a.builds {
			recs[AttestKey(at.TenantID, content, a.scanner.profile, tier, a.ruleset, b)] = AttestRecord{Patterns: patterns}
		}
	}
	put(at.Content, at.Patterns)
	if at.Redacted && at.Output != "" && at.Output != at.Content {
		// What the gateway will meet is the redacted text. Vouch for what a
		// scan of it actually finds (normally nothing), not for "clean".
		patterns, err := a.scanPatterns(ctx, at.Output, at.Tier)
		if err != nil {
			return recs, err // the input record is still good
		}
		put(at.Output, patterns)
	}
	return recs, nil
}

func (a *Attestor) write(at federation.ScanAttestation) { //nolint:gocritic // value by design
	ctx, cancel := context.WithTimeout(context.Background(), attestWriteTimeout)
	defer cancel()
	recs, rerr := a.Records(ctx, at)
	if rerr != nil {
		attestations.WithLabelValues("failed").Inc()
		a.log.Debug("scan attestation: output re-scan failed; output not attested", zap.Error(rerr))
	}
	if err := a.store.SetMany(ctx, recs, a.ttl); err != nil {
		attestations.WithLabelValues("failed").Add(float64(len(recs)))
		a.log.Debug("scan attestation: store write failed; the gateway will scan", zap.Error(err))
		return
	}
	attestations.WithLabelValues("written").Add(float64(len(recs)))
}

func (a *Attestor) scanPatterns(ctx context.Context, content string, tier federation.TrustTier) ([]string, error) {
	// Detection only: the attestor never redacts.
	det := *a.scanner
	det.redact = false
	out, err := det.Scan(ctx, content, tier)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for i := range out.Findings {
		set[out.Findings[i].PatternID] = true
	}
	if len(set) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// Close stops accepting attestations and drains the queue (bounded wait).
func (a *Attestor) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	close(a.ch)
	a.mu.Unlock()
	select {
	case <-a.done:
	case <-time.After(closeAttestTimeout):
	}
	return nil
}

// RulesetVersion is the digest of the scanner's registered rules (Gatekeeper
// scan.RulesetVersioner), "unknown" when the scanner cannot name it — the
// gateway's fallback too.
func (s *Scanner) RulesetVersion() string {
	if s != nil {
		if v, ok := s.sc.(scan.RulesetVersioner); ok {
			return v.RulesetVersion()
		}
	}
	return "unknown"
}
