package federation

// Server probes (Plan #22 T.4, design §9.5 "Server health").
//
// A FederatedMCPServer CR that says Registered only proves the proxy accepted
// a registration. From 2026-11-03 to 2026-11-19 every CR said Registered while
// the proxy could reach 1 of its 12 servers (MCP-14). The prober is the
// signal that was missing: every interval it asks each registered server for
// its tools the way a real client would, and publishes the answer as
//
//   - tas_mcp_server_up{server_id}               1 when the last probe passed
//   - tas_mcp_server_probe_duration_seconds      histogram, by server and result
//   - GET /api/v1/federation/servers/{id}        "probe" block (the operator reads this)
//   - GET /api/v1/federation/health              "probes" map
//   - a ToolCatalogue snapshot per server, for the catalogue event
//
// A probe is `initialize` then `tools/list` for a Streamable-HTTP ("sse")
// server. A plain "http" server speaks the TAS bridge convention, which has
// no initialize endpoint: the bridge performs the MCP handshake with its
// child itself, behind GET /mcp/tools/list, so for those the probe is
// tools/list alone and a passing tools/list proves the handshake too.
//
// Probes never touch the tool-call path. They run on their own goroutines,
// bounded by Concurrency, each under its own Timeout; a server whose previous
// probe is still running is skipped for that round rather than queued, so a
// hung server costs one goroutine, not one per interval. They bypass
// InvokeServer, so they add nothing to the request counters, the upstream
// error counters MCP-16 alerts on, or the tool-call events.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// Probe defaults.
const (
	DefaultProbeInterval    = 60 * time.Second
	DefaultProbeTimeout     = 10 * time.Second
	DefaultProbeConcurrency = 4

	// ProbeStageInitialize and ProbeStageToolsList name the step a probe
	// failed at (ProbeResult.Stage).
	ProbeStageInitialize = "initialize"
	ProbeStageToolsList  = "tools/list"

	// probeErrorPreview bounds the error text kept on a result.
	probeErrorPreview = 300
)

// ProbeConfig configures the prober.
type ProbeConfig struct {
	Interval    time.Duration
	Timeout     time.Duration
	Concurrency int
}

func (c ProbeConfig) withDefaults() ProbeConfig {
	if c.Interval <= 0 {
		c.Interval = DefaultProbeInterval
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultProbeTimeout
	}
	if c.Concurrency <= 0 {
		c.Concurrency = DefaultProbeConcurrency
	}
	return c
}

// CatalogueTool is one tool a server lists: its name and the JSON size of its
// inputSchema (0 when the tool declares none).
type CatalogueTool struct {
	Name        string `json:"name"`
	SchemaBytes int    `json:"schema_bytes"`
}

// ProbeResult is the last probe of one server, as served by the federation API.
type ProbeResult struct {
	ServerID string `json:"server_id"`
	// Up is true when every probe step passed.
	Up bool `json:"up"`
	// Stage is the step that failed; empty when Up.
	Stage string `json:"stage,omitempty"`
	// Error is the failure, truncated; empty when Up.
	Error      string    `json:"error,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	DurationMS int64     `json:"duration_ms"`
	// LastUpAt is when a probe last passed (nil if never, on this replica).
	LastUpAt *time.Time `json:"last_up_at,omitempty"`
	// ConsecutiveFailures counts failed probes since the last pass.
	ConsecutiveFailures int `json:"consecutive_failures"`
	// ToolCount and ToolsetHash describe the last successful tools/list.
	ToolCount   int    `json:"tool_count"`
	ToolsetHash string `json:"toolset_hash,omitempty"`
	// IntervalSeconds is the probe cadence, so a reader can tell a stale
	// result from a fresh one.
	IntervalSeconds int `json:"interval_seconds"`
}

// ToolCatalogue is one server's tools/list at one moment.
type ToolCatalogue struct {
	ServerID   string
	ObservedAt time.Time
	Tools      []CatalogueTool
	// Hash identifies the toolset (names and schema sizes), so a change is
	// detectable without comparing lists.
	Hash string
}

// CatalogueObserver receives a server's catalogue after each successful probe.
// Deciding whether to publish it (on change, or hourly) is the observer's job.
// Implementations MUST NOT block.
type CatalogueObserver interface {
	ObserveCatalogue(c ToolCatalogue)
}

// ProbeStatusProvider is implemented by a TASManager that probes its servers.
// The HTTP handlers use it to add probe results to the federation API.
type ProbeStatusProvider interface {
	ProbeStatus(serverID string) (ProbeResult, bool)
	ProbeStatuses() map[string]ProbeResult
}

var (
	// ServerUp is 1 when the last probe of a server passed, 0 when it failed.
	// A server never probed on this replica has no series.
	ServerUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "tas_mcp",
		Name:      "server_up",
		Help:      "1 when the last initialize+tools/list probe of a federated server passed, else 0.",
	}, []string{"server_id"})

	// ProbeSeconds is the probe round trip, by result (up|down).
	ProbeSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "tas_mcp",
		Name:      "server_probe_duration_seconds",
		Help:      "Duration of the initialize+tools/list probe of a federated server.",
		Buckets:   []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"server_id", "result"})
)

// probeTarget is implemented by services that can be probed without going
// through Invoke (so a probe never counts as upstream traffic).
type probeTarget interface {
	probe(ctx context.Context) (stage string, result interface{}, err error)
}

// initializer is implemented by clients whose transport has an MCP
// initialize step of its own (Streamable HTTP).
type initializer interface {
	Initialize(ctx context.Context) error
}

// probe runs initialize (when the transport has one) then tools/list
// directly on the client.
func (s *GenericService) probe(ctx context.Context) (stage string, result interface{}, err error) {
	if s.client == nil {
		return ProbeStageInitialize, nil, errors.New("no client for server")
	}
	if in, ok := s.client.(initializer); ok {
		if err := in.Initialize(ctx); err != nil {
			return ProbeStageInitialize, nil, err
		}
	}
	res, err := s.client.Call(ctx, methodToolsList, nil)
	if err != nil {
		return ProbeStageToolsList, nil, err
	}
	return ProbeStageToolsList, res, nil
}

// Prober probes every registered server of a Manager on an interval.
type Prober struct {
	m      *Manager
	cfg    ProbeConfig
	logger *zap.Logger
	sem    chan struct{}

	mu       sync.RWMutex
	results  map[string]ProbeResult
	inflight map[string]bool
	observer CatalogueObserver

	now func() time.Time
}

// NewProber builds a prober for m. Call Run to start it.
func NewProber(m *Manager, cfg ProbeConfig, logger *zap.Logger) *Prober {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Prober{
		m:        m,
		cfg:      cfg,
		logger:   logger,
		sem:      make(chan struct{}, cfg.Concurrency),
		results:  map[string]ProbeResult{},
		inflight: map[string]bool{},
		now:      time.Now,
	}
}

// SetCatalogueObserver installs the catalogue observer. Nil turns it off.
func (p *Prober) SetCatalogueObserver(o CatalogueObserver) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.observer = o
}

// Run probes immediately, then every Interval, until ctx is done. It returns
// once every probe it started has finished.
func (p *Prober) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	t := time.NewTicker(p.cfg.Interval)
	defer t.Stop()
	for {
		p.round(ctx, &wg)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce probes every registered server and waits for all probes. For tests
// and for an on-demand refresh.
func (p *Prober) RunOnce(ctx context.Context) {
	var wg sync.WaitGroup
	p.round(ctx, &wg)
	wg.Wait()
}

func (p *Prober) round(ctx context.Context, wg *sync.WaitGroup) {
	servers, err := p.m.ListServers()
	if err != nil {
		p.logger.Debug("server probes: list servers failed; skipping round", zap.Error(err))
		return
	}
	if p.m.TenantPolicy() != nil {
		// A disabled server is not probed, and is pruned like an
		// unregistered one so its gauge does not read as down.
		enabled := servers[:0]
		for _, s := range servers {
			if s.IsEnabled() {
				enabled = append(enabled, s)
			}
		}
		servers = enabled
	}
	live := make(map[string]bool, len(servers))
	for _, s := range servers {
		live[s.ID] = true
	}
	p.prune(live)

	for _, s := range servers {
		id := s.ID
		if !p.claim(id) {
			continue // previous probe still running: skip, don't queue
		}
		select {
		case p.sem <- struct{}{}:
		case <-ctx.Done():
			p.release(id)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-p.sem }()
			defer p.release(id)
			p.probeOne(ctx, id)
		}()
	}
}

func (p *Prober) claim(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inflight[id] {
		return false
	}
	p.inflight[id] = true
	return true
}

func (p *Prober) release(id string) {
	p.mu.Lock()
	delete(p.inflight, id)
	p.mu.Unlock()
}

// prune forgets servers that are no longer registered, including their gauge
// series, so an unregistered server does not read as down forever.
func (p *Prober) prune(live map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.results {
		if !live[id] {
			delete(p.results, id)
			ServerUp.DeleteLabelValues(id)
		}
	}
}

func (p *Prober) probeOne(parent context.Context, id string) {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("server probe panicked", zap.String("server_id", id), zap.Any("panic", r))
		}
	}()
	ctx, cancel := context.WithTimeout(parent, p.cfg.Timeout)
	defer cancel()

	start := p.now()
	stage, raw, err := p.run(ctx, id)
	dur := p.now().Sub(start)

	var tools []CatalogueTool
	if err == nil {
		tools, err = parseToolsList(raw)
		if err != nil {
			stage = ProbeStageToolsList
		}
	}
	if parent.Err() != nil && err != nil {
		return // shutting down: don't record a failure we caused
	}
	p.record(id, start, dur, stage, tools, err)
}

func (p *Prober) run(ctx context.Context, id string) (stage string, result interface{}, err error) {
	svc, err := p.m.ensureService(ctx, id)
	if err != nil {
		return ProbeStageInitialize, nil, err
	}
	if t, ok := svc.(probeTarget); ok {
		return t.probe(ctx)
	}
	// A service without a direct probe (a test double): tools/list via Invoke.
	resp, err := svc.Invoke(ctx, &MCPRequest{ID: "probe", Method: methodToolsList})
	if err != nil {
		return ProbeStageToolsList, nil, err
	}
	if resp == nil {
		return ProbeStageToolsList, nil, errors.New("empty response")
	}
	if resp.Error != nil {
		return ProbeStageToolsList, nil, fmt.Errorf("tools/list error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return ProbeStageToolsList, resp.Result, nil
}

func (p *Prober) record(id string, start time.Time, dur time.Duration, stage string, tools []CatalogueTool, err error) {
	result := "up"
	if err != nil {
		result = "down"
	}
	ProbeSeconds.WithLabelValues(id, result).Observe(dur.Seconds())

	p.mu.Lock()
	prev, had := p.results[id]
	r := ProbeResult{
		ServerID:        id,
		CheckedAt:       start.UTC(),
		DurationMS:      dur.Milliseconds(),
		IntervalSeconds: int(p.cfg.Interval / time.Second),
	}
	if had {
		r.LastUpAt = prev.LastUpAt
		r.ToolCount, r.ToolsetHash = prev.ToolCount, prev.ToolsetHash
	}
	var cat *ToolCatalogue
	if err == nil {
		r.Up = true
		up := start.UTC()
		r.LastUpAt = &up
		r.ToolCount = len(tools)
		r.ToolsetHash = toolsetHash(tools)
		cat = &ToolCatalogue{ServerID: id, ObservedAt: start.UTC(), Tools: tools, Hash: r.ToolsetHash}
	} else {
		r.Stage = stage
		r.Error = truncate(err.Error(), probeErrorPreview)
		r.ConsecutiveFailures = prev.ConsecutiveFailures + 1
	}
	p.results[id] = r
	obs := p.observer
	p.mu.Unlock()

	if r.Up {
		ServerUp.WithLabelValues(id).Set(1)
	} else {
		ServerUp.WithLabelValues(id).Set(0)
	}

	switch {
	case err != nil && (!had || prev.Up):
		p.logger.Warn("server probe: down", zap.String("server_id", id),
			zap.String("stage", stage), zap.Error(err))
	case err == nil && had && !prev.Up:
		p.logger.Info("server probe: up again", zap.String("server_id", id),
			zap.Int("tools", len(tools)))
	}

	if cat != nil && obs != nil {
		obs.ObserveCatalogue(*cat)
	}
}

// ProbeStatus returns the last probe of one server.
func (p *Prober) ProbeStatus(id string) (ProbeResult, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	r, ok := p.results[id]
	return r, ok
}

// ProbeStatuses returns a copy of every server's last probe.
func (p *Prober) ProbeStatuses() map[string]ProbeResult {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]ProbeResult, len(p.results))
	for k := range p.results {
		out[k] = p.results[k]
	}
	return out
}

// parseToolsList reads a tools/list result: {"tools": [{"name", "inputSchema"}]}.
// A result with no tools array is a failure, not an empty toolset: an MCP
// server always answers tools/list with one, so its absence means whatever
// answered is not a working MCP server.
func parseToolsList(raw interface{}) ([]CatalogueTool, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("tools/list: result is %T, not an object", raw)
	}
	arr, ok := m["tools"].([]interface{})
	if !ok {
		return nil, errors.New("tools/list: result has no tools array")
	}
	tools := make([]CatalogueTool, 0, len(arr))
	for _, item := range arr {
		t, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := t["name"].(string)
		if name == "" {
			continue
		}
		tools = append(tools, CatalogueTool{Name: name, SchemaBytes: schemaBytes(t)})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// schemaBytes is the compact JSON size of a tool's inputSchema; 0 when absent.
func schemaBytes(tool map[string]interface{}) int {
	s, ok := tool["inputSchema"]
	if !ok || s == nil {
		return 0
	}
	b, err := json.Marshal(s)
	if err != nil {
		return 0
	}
	return len(b)
}

// toolsetHash identifies a toolset by its sorted names and schema sizes.
func toolsetHash(tools []CatalogueTool) string {
	h := sha256.New()
	for _, t := range tools {
		_, _ = fmt.Fprintf(h, "%s\x00%d\n", t.Name, t.SchemaBytes)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// SetProber attaches a prober so the Manager can serve probe results. Nil
// detaches it.
func (m *Manager) SetProber(p *Prober) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prober = p
}

// ProbeStatus implements ProbeStatusProvider. False when probes are off or the
// server has not been probed on this replica yet.
func (m *Manager) ProbeStatus(id string) (ProbeResult, bool) {
	m.mu.RLock()
	p := m.prober
	m.mu.RUnlock()
	if p == nil {
		return ProbeResult{}, false
	}
	return p.ProbeStatus(id)
}

// ProbeStatuses implements ProbeStatusProvider. Nil when probes are off.
func (m *Manager) ProbeStatuses() map[string]ProbeResult {
	m.mu.RLock()
	p := m.prober
	m.mu.RUnlock()
	if p == nil {
		return nil
	}
	return p.ProbeStatuses()
}
