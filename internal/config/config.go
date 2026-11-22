// Package config provides configuration management for the TAS MCP server.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default configuration constants
const (
	// DefaultHTTPPort is the default HTTP server port
	DefaultHTTPPort = 8082
	// DefaultGRPCPort is the default gRPC server port
	DefaultGRPCPort = 50052
	// DefaultHealthCheckPort is the default health check port
	DefaultHealthCheckPort = 8083
	// DefaultForwardTimeout is the default timeout for forwarding requests
	DefaultForwardTimeout = 30 * time.Second
	// DefaultMaxEventSize is the default maximum event size (1MB)
	DefaultMaxEventSize = 1024 * 1024
	// DefaultBufferSize is the default buffer size for events
	DefaultBufferSize = 1000
	// DefaultMaxConnections is the default maximum number of connections
	DefaultMaxConnections = 100
	// DefaultRetryAttempts is the default number of retry attempts
	DefaultRetryAttempts = 3
	// DefaultForwardingWorkers is the default number of forwarding workers
	DefaultForwardingWorkers = 5
	// DefaultFilePermissions is the default file permissions for config files
	DefaultFilePermissions = 0600
	// DefaultReductionOllamaURL is the default embedder/SLM endpoint for
	// reduce-at-source (the relevance step always runs on Ollama).
	DefaultReductionOllamaURL = "http://ollama:11434"
	// DefaultFederationRedisURL is the default shared-registry Redis endpoint
	// (used only when FEDERATION_REGISTRY=redis).
	DefaultFederationRedisURL = "redis://redis-shared.tas-shared.svc.cluster.local:6379/0"
)

// Config holds all configuration for the TAS MCP server
type Config struct {
	HTTPPort        int
	GRPCPort        int
	HealthCheckPort int
	LogLevel        string
	ForwardTo       []string
	ForwardTimeout  time.Duration
	MaxEventSize    int64
	BufferSize      int
	MaxConnections  int
	Version         string
	Forwarding      *ForwardingConfig     `json:"forwarding,omitempty"`
	Reduction       *ReductionConfig      `json:"reduction,omitempty"`
	Scanning        *ScanningConfig       `json:"scanning,omitempty"`
	Federation      *FederationConfig     `json:"federation,omitempty"`
	Identity        *IdentityConfig       `json:"identity,omitempty"`
	ToolCallEvents  *ToolCallEventsConfig `json:"tool_call_events,omitempty"`
	ServerProbes    *ServerProbesConfig   `json:"server_probes,omitempty"`
	// ToolCatalogueEvents turns on com.tas.aiqg.tool_catalogue.v1 (Plan #22
	// T.4). Nil (TOOL_CATALOGUE_EVENTS_ENABLED unset) publishes none. Uses
	// the same brokers, topic and buffer as ToolCallEvents, and needs
	// ServerProbes: the catalogue is what the probes see.
	ToolCatalogueEvents *ToolCallEventsConfig `json:"tool_catalogue_events,omitempty"`
	// TenantServers turns on the tenant-scoped federation registry (Plan #22
	// C.1). Nil (MCP_TENANT_SERVERS unset) leaves the registry global and
	// its write routes unauthenticated, exactly as before.
	TenantServers *TenantServersConfig `json:"tenant_servers,omitempty"`
}

// TenantServersConfig configures the tenant-scoped federation registry
// (Plan #22 C.1, design §9.6).
type TenantServersConfig struct {
	Enabled bool `json:"enabled"`
	// AdminToken is MCP_ADMIN_TOKEN: the service token register, update and
	// delete require (header TAS-MCP-Admin-Token). Empty with the flag on =
	// every write is refused. Never logged or serialized.
	AdminToken string `json:"-"`
	// EgressAllowlist is MCP_TENANT_EGRESS_ALLOWLIST, a JSON object of
	// tenant id -> host names a tenant server outside the cluster may use
	// (Q13), e.g. {"<tenant-uuid>":["api.salesforce.com","*.qualtrics.com"]}.
	EgressAllowlist map[string][]string `json:"egress_allowlist,omitempty"`
	// EgressAllowlistErr is set when MCP_TENANT_EGRESS_ALLOWLIST does not
	// parse; the allowlist is then empty (fail closed: no external hosts).
	EgressAllowlistErr error `json:"-"`
	// ClusterNamespaces is MCP_TENANT_CLUSTER_NAMESPACES: the namespaces an
	// in-cluster tenant server may live in. Empty = tas-mcp-servers.
	ClusterNamespaces []string `json:"cluster_namespaces,omitempty"`
}

// ToolCallEventsConfig configures the com.tas.aiqg.tool_call.v1 publisher
// (Plan #22 T.2). Nil (the default, TOOL_CALL_EVENTS_ENABLED unset) publishes
// nothing; the tas_mcp_tool_call_seconds histogram is recorded regardless.
type ToolCallEventsConfig struct {
	Enabled bool `json:"enabled"`
	// Brokers and Topic reuse tas-llm-router's AIQG_KAFKA_* names, since both
	// publish to the same AIQG topic.
	Brokers []string `json:"brokers,omitempty"`
	Topic   string   `json:"topic,omitempty"`
	// BufferSize bounds events waiting for Kafka; past it events are dropped
	// (and counted) rather than slowing a tool call.
	BufferSize int `json:"buffer_size,omitempty"`
}

// Tool-call event defaults.
const (
	DefaultToolCallEventsTopic  = "tas.aiqg.events.v1"
	DefaultToolCallEventsBuffer = 1024
)

// ServerProbesConfig configures the federated-server prober (Plan #22 T.4):
// initialize + tools/list against every registered server, every Interval.
// Nil (SERVER_PROBES_ENABLED unset) means no probes, no tas_mcp_server_up
// series, and no probe block on the federation API.
type ServerProbesConfig struct {
	Enabled     bool          `json:"enabled"`
	Interval    time.Duration `json:"interval,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	Concurrency int           `json:"concurrency,omitempty"`
}

// Server probe defaults.
const (
	DefaultServerProbeInterval    = 60 * time.Second
	DefaultServerProbeTimeout     = 10 * time.Second
	DefaultServerProbeConcurrency = 4
)

// IdentityConfig configures caller-identity resolution at the federation proxy
// (Plan #22 T.1). Nil (the default, IDENTITY_ENABLED unset) means the proxy
// reads no TAS-Auth and makes no validator calls — the deploy is inert.
//
// Mode is observe only: an unknown or missing token is still served, recorded
// unattributed; a validator outage is fail-open.
type IdentityConfig struct {
	Enabled bool `json:"enabled"`
	// DashboardURL is aiqg-dashboard-be's base URL (everything before
	// /internal/auth/validate). Same variable name as tas-llm-router.
	DashboardURL string `json:"dashboard_url,omitempty"`
	// DashboardInternalAuthToken is the shared secret dashboard-be requires on
	// /internal/*. Same variable name as tas-llm-router. Never logged.
	DashboardInternalAuthToken string `json:"-"`
	// CacheTTL is how long a validator answer is reused (default 60s).
	CacheTTL time.Duration `json:"cache_ttl,omitempty"`
}

// DefaultIdentityCacheTTL is the validator cache TTL (design §9.5).
const DefaultIdentityCacheTTL = 60 * time.Second

// ScanningConfig configures Gatekeeper boundary scanning of federated tool
// results (docs/AIQG-GATEKEEPER-INTEGRATION.md §4, tas-llm-router#101 G2). Nil
// (the default, when SCANNING_ENABLED is unset) leaves the boundary unscanned.
//
// Scanning is the security control at the MCP proxy boundary: federated tool
// output is TierExternal content flowing into LLM prompts. It runs independently
// of reduction (which is a per-server opt-in optimization).
type ScanningConfig struct {
	Enabled bool `json:"enabled"`

	// Redact controls whether findings are redacted in place. Default false =
	// LOG-ONLY (observe + report, modify nothing) — the draft→enforce first
	// stage, so the finding rate is understood before any tool output is altered.
	Redact bool `json:"redact"`

	// RedactStrategy is applied when Redact is true: "mask" (default), "replace",
	// or "hash". All deterministic and infra-free. Tokenize is NOT offered — it
	// needs Databunker (not deployed) and would break prompt caching.
	RedactStrategy string `json:"redact_strategy,omitempty"`

	// Profile selects the scan profile: "full" (default), "pii_only",
	// "injection_only", "compliance".
	Profile string `json:"profile,omitempty"`

	// MinConfidence is the detection threshold (0 → Gatekeeper default 0.7).
	MinConfidence float64 `json:"min_confidence,omitempty"`

	// Attest writes each scanned tool result to the gateway's scan-once
	// store (Plan #22 T.3, GK-5b). Nil (SCAN_ATTEST_ENABLED unset) = off.
	Attest *ScanAttestConfig `json:"attest,omitempty"`
}

// ScanAttestConfig configures proxy scan attestations (GK-5b).
type ScanAttestConfig struct {
	Enabled bool `json:"enabled"`
	// RedisURL is the gateway's scan-once store. Default: the federation
	// registry's Redis, which is the same instance.
	RedisURL string `json:"redis_url,omitempty"`
	// GatewayBuilds are the gateway builds (its events.GatewayVersion, the
	// git short SHA) whose keys to write. Required: the build is part of
	// the gateway's key.
	GatewayBuilds []string `json:"gateway_builds,omitempty"`
	// GatewayTier is the gateway's inbound trust tier, the tier term of its
	// key ("internal" live). Default "internal".
	GatewayTier string        `json:"gateway_tier,omitempty"`
	TTL         time.Duration `json:"ttl,omitempty"`
}

// DefaultScanAttestTTL matches the gateway's scan-once TTL.
const DefaultScanAttestTTL = 24 * time.Hour

// FederationConfig selects the federation registry backend. Nil (the default)
// means the in-memory registry (single-pod semantics). Set Registry=redis for a
// shared registry so multiple gateway replicas agree on the federated set.
type FederationConfig struct {
	// Registry is the backend: "memory" (default) or "redis".
	Registry string `json:"registry"`
	// RedisURL is the shared-registry endpoint (used when Registry=redis).
	RedisURL string `json:"redis_url,omitempty"`
}

// ReductionConfig configures cache-safe reduce-at-source for federated tool
// results. It is a plain description of the knobs — the federation layer maps it
// to the Gatekeeper-backed reducer at wiring time (see internal/reduction), so
// this package stays free of the extractor dependency. Nil (the default) means
// reduction is off and tool results pass through untouched.
type ReductionConfig struct {
	Enabled bool `json:"enabled"`

	// EmbedModel / OllamaURL configure the relevance step's embedder.
	EmbedModel string `json:"embed_model,omitempty"`
	OllamaURL  string `json:"ollama_url,omitempty"`

	// MinContentSize is the byte threshold below which a tool result is left
	// alone (0 = extractor default). Small results aren't worth reducing.
	MinContentSize int `json:"min_content_size,omitempty"`

	// SLM* configure the optional summarization step. Off → relevance-only.
	SLMEnabled   bool   `json:"slm_enabled"`
	SLMProvider  string `json:"slm_provider,omitempty"` // ollama | openai | anthropic
	SLMBaseURL   string `json:"slm_base_url,omitempty"`
	SLMModel     string `json:"slm_model,omitempty"`
	SLMAPIKey    string `json:"slm_api_key,omitempty"`
	SLMMaxTokens int    `json:"slm_max_tokens,omitempty"`

	// MaxOutputBytes caps the reduced size of a single tool-result block
	// (0 = extractor default).
	MaxOutputBytes int `json:"max_output_bytes,omitempty"`
}

// ForwardingConfig holds event forwarding configuration
type ForwardingConfig struct {
	Enabled              bool                   `json:"enabled"`
	DefaultRetryAttempts int                    `json:"default_retry_attempts"`
	DefaultTimeout       time.Duration          `json:"default_timeout"`
	BufferSize           int                    `json:"buffer_size"`
	Workers              int                    `json:"workers"`
	Targets              []*TargetConfiguration `json:"targets"`
	Rules                []*GlobalRule          `json:"rules,omitempty"`
}

// TargetConfiguration holds configuration for a forwarding target
type TargetConfiguration struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"` // grpc, http, kafka, webhook, argo-events
	Endpoint string            `json:"endpoint"`
	Config   *TargetConfig     `json:"config,omitempty"`
	Rules    []*ForwardingRule `json:"rules,omitempty"`
}

// TargetConfig holds detailed target configuration
type TargetConfig struct {
	Timeout        time.Duration     `json:"timeout"`
	RetryAttempts  int               `json:"retry_attempts"`
	RetryDelay     time.Duration     `json:"retry_delay"`
	HealthCheckURL string            `json:"health_check_url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Authentication *AuthConfig       `json:"authentication,omitempty"`
	BatchSize      int               `json:"batch_size,omitempty"`
	BatchTimeout   time.Duration     `json:"batch_timeout,omitempty"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Type   string            `json:"type"` // bearer, basic, api-key, oauth2
	Token  string            `json:"token,omitempty"`
	Header string            `json:"header,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// ForwardingRule defines conditions for event forwarding
type ForwardingRule struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled"`
	Priority   int               `json:"priority"`
	Conditions []*RuleCondition  `json:"conditions"`
	Transform  *EventTransform   `json:"transform,omitempty"`
	RateLimit  *RateLimit        `json:"rate_limit,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// GlobalRule defines global forwarding rules
type GlobalRule struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled"`
	Priority   int               `json:"priority"`
	Conditions []*RuleCondition  `json:"conditions"`
	Actions    []*RuleAction     `json:"actions"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// RuleCondition defines a condition for rule evaluation
type RuleCondition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"` // eq, ne, gt, lt, gte, lte, contains, regex, in, not_in
	Value    interface{} `json:"value"`
	Negate   bool        `json:"negate,omitempty"`
}

// RuleAction defines an action to take when a rule matches
type RuleAction struct {
	Type    string                 `json:"type"` // forward, drop, transform, alert
	Targets []string               `json:"targets,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// EventTransform defines how to transform events
type EventTransform struct {
	AddFields    map[string]interface{} `json:"add_fields,omitempty"`
	RemoveFields []string               `json:"remove_fields,omitempty"`
	RenameFields map[string]string      `json:"rename_fields,omitempty"`
	Template     string                 `json:"template,omitempty"`
	Script       string                 `json:"script,omitempty"`
}

// RateLimit defines rate limiting configuration
type RateLimit struct {
	RequestsPerSecond int           `json:"requests_per_second"`
	BurstSize         int           `json:"burst_size"`
	Window            time.Duration `json:"window"`
}

// Load reads configuration from environment variables with defaults
func Load() *Config {
	config := &Config{
		HTTPPort:        getEnvAsInt("HTTP_PORT", DefaultHTTPPort),
		GRPCPort:        getEnvAsInt("GRPC_PORT", DefaultGRPCPort),
		HealthCheckPort: getEnvAsInt("HEALTH_CHECK_PORT", DefaultHealthCheckPort),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		ForwardTo:       getEnvAsSlice("FORWARD_TO", []string{}),
		ForwardTimeout:  getEnvAsDuration("FORWARD_TIMEOUT", DefaultForwardTimeout),
		MaxEventSize:    getEnvAsInt64("MAX_EVENT_SIZE", DefaultMaxEventSize),
		BufferSize:      getEnvAsInt("BUFFER_SIZE", DefaultBufferSize),
		MaxConnections:  getEnvAsInt("MAX_CONNECTIONS", DefaultMaxConnections),
		Version:         getEnv("VERSION", "dev"),
	}

	// Load forwarding configuration if enabled
	if getEnvAsBool("FORWARDING_ENABLED", false) {
		config.Forwarding = loadForwardingConfig()
	}

	// Load reduce-at-source configuration if enabled
	if getEnvAsBool("REDUCTION_ENABLED", false) {
		config.Reduction = loadReductionConfig()
	}

	// Load boundary-scanning configuration if enabled
	if getEnvAsBool("SCANNING_ENABLED", false) {
		config.Scanning = loadScanningConfig()
	}

	// Load caller-identity configuration if enabled (Plan #22 T.1)
	if getEnvAsBool("IDENTITY_ENABLED", false) {
		config.Identity = loadIdentityConfig()
	}

	// Load tool-call event publishing if enabled (Plan #22 T.2)
	if getEnvAsBool("TOOL_CALL_EVENTS_ENABLED", false) {
		config.ToolCallEvents = &ToolCallEventsConfig{
			Enabled:    true,
			Brokers:    getEnvAsSlice("AIQG_KAFKA_BROKERS", []string{}),
			Topic:      getEnv("AIQG_KAFKA_TOPIC", DefaultToolCallEventsTopic),
			BufferSize: getEnvAsInt("TOOL_CALL_EVENTS_BUFFER", DefaultToolCallEventsBuffer),
		}
	}

	// Server probes and the tool catalogue (Plan #22 T.4)
	if getEnvAsBool("SERVER_PROBES_ENABLED", false) {
		config.ServerProbes = &ServerProbesConfig{
			Enabled:     true,
			Interval:    getEnvAsDuration("SERVER_PROBE_INTERVAL", DefaultServerProbeInterval),
			Timeout:     getEnvAsDuration("SERVER_PROBE_TIMEOUT", DefaultServerProbeTimeout),
			Concurrency: getEnvAsInt("SERVER_PROBE_CONCURRENCY", DefaultServerProbeConcurrency),
		}
	}
	if getEnvAsBool("TOOL_CATALOGUE_EVENTS_ENABLED", false) {
		config.ToolCatalogueEvents = &ToolCallEventsConfig{
			Enabled:    true,
			Brokers:    getEnvAsSlice("AIQG_KAFKA_BROKERS", []string{}),
			Topic:      getEnv("AIQG_KAFKA_TOPIC", DefaultToolCallEventsTopic),
			BufferSize: getEnvAsInt("TOOL_CALL_EVENTS_BUFFER", DefaultToolCallEventsBuffer),
		}
	}

	if getEnvAsBool("MCP_TENANT_SERVERS", false) {
		config.TenantServers = loadTenantServersConfig()
	}

	// Load federation registry configuration if a backend is chosen
	if getEnv("FEDERATION_REGISTRY", "") != "" {
		config.Federation = loadFederationConfig()
	}

	return config
}

// loadTenantServersConfig loads the tenant-scoped registry configuration.
func loadTenantServersConfig() *TenantServersConfig {
	c := &TenantServersConfig{
		Enabled:           true,
		AdminToken:        getEnv("MCP_ADMIN_TOKEN", ""),
		ClusterNamespaces: getEnvAsSlice("MCP_TENANT_CLUSTER_NAMESPACES", nil),
	}
	if raw := strings.TrimSpace(getEnv("MCP_TENANT_EGRESS_ALLOWLIST", "")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.EgressAllowlist); err != nil {
			c.EgressAllowlist = nil
			c.EgressAllowlistErr = fmt.Errorf("MCP_TENANT_EGRESS_ALLOWLIST: %w", err)
		}
	}
	return c
}

// loadIdentityConfig loads caller-identity configuration from environment.
func loadIdentityConfig() *IdentityConfig {
	return &IdentityConfig{
		Enabled:                    true,
		DashboardURL:               getEnv("AIQG_DASHBOARD_URL", ""),
		DashboardInternalAuthToken: getEnv("AIQG_DASHBOARD_INTERNAL_AUTH_TOKEN", ""),
		CacheTTL:                   getEnvAsDuration("IDENTITY_CACHE_TTL", DefaultIdentityCacheTTL),
	}
}

// loadFederationConfig loads federation registry configuration from environment
func loadFederationConfig() *FederationConfig {
	return &FederationConfig{
		Registry: getEnv("FEDERATION_REGISTRY", "memory"),
		RedisURL: getEnv("FEDERATION_REDIS_URL", DefaultFederationRedisURL),
	}
}

// LoadFromFile loads configuration from a JSON file
func LoadFromFile(filePath string) (*Config, error) {
	// #nosec G304 -- filePath is validated by caller
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(&config)

	return &config, nil
}

// SaveToFile saves the configuration to a JSON file
func (c *Config) SaveToFile(filePath string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(filePath, data, DefaultFilePermissions); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// loadForwardingConfig loads forwarding configuration from environment
func loadForwardingConfig() *ForwardingConfig {
	return &ForwardingConfig{
		Enabled:              getEnvAsBool("FORWARDING_ENABLED", true),
		DefaultRetryAttempts: getEnvAsInt("FORWARDING_RETRY_ATTEMPTS", DefaultRetryAttempts),
		DefaultTimeout:       getEnvAsDuration("FORWARDING_TIMEOUT", DefaultForwardTimeout),
		BufferSize:           getEnvAsInt("FORWARDING_BUFFER_SIZE", DefaultBufferSize),
		Workers:              getEnvAsInt("FORWARDING_WORKERS", DefaultForwardingWorkers),
		Targets:              loadTargetsFromEnv(),
	}
}

// loadReductionConfig loads reduce-at-source configuration from environment
func loadReductionConfig() *ReductionConfig {
	return &ReductionConfig{
		Enabled:        true,
		EmbedModel:     getEnv("REDUCTION_EMBED_MODEL", ""),
		OllamaURL:      getEnv("REDUCTION_OLLAMA_URL", DefaultReductionOllamaURL),
		MinContentSize: getEnvAsInt("REDUCTION_MIN_CONTENT_SIZE", 0),
		SLMEnabled:     getEnvAsBool("REDUCTION_SLM_ENABLED", false),
		SLMProvider:    getEnv("REDUCTION_SLM_PROVIDER", ""),
		SLMBaseURL:     getEnv("REDUCTION_SLM_BASE_URL", ""),
		SLMModel:       getEnv("REDUCTION_SLM_MODEL", ""),
		SLMAPIKey:      getEnv("REDUCTION_SLM_API_KEY", ""),
		SLMMaxTokens:   getEnvAsInt("REDUCTION_SLM_MAX_TOKENS", 0),
		MaxOutputBytes: getEnvAsInt("REDUCTION_MAX_OUTPUT_BYTES", 0),
	}
}

// loadScanningConfig loads boundary-scanning configuration from environment
// variables. Redaction defaults OFF (log-only): the safe first stage.
func loadScanningConfig() *ScanningConfig {
	minConf := 0.0
	if v := getEnv("SCANNING_MIN_CONFIDENCE", ""); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			minConf = f
		}
	}
	return &ScanningConfig{
		Enabled:        true,
		Redact:         getEnvAsBool("SCANNING_REDACT", false),
		RedactStrategy: getEnv("SCANNING_REDACT_STRATEGY", ""),
		Profile:        getEnv("SCANNING_PROFILE", ""),
		MinConfidence:  minConf,
		Attest:         loadScanAttestConfig(),
	}
}

// loadScanAttestConfig loads GK-5b attestation config; nil unless
// SCAN_ATTEST_ENABLED.
func loadScanAttestConfig() *ScanAttestConfig {
	if !getEnvAsBool("SCAN_ATTEST_ENABLED", false) {
		return nil
	}
	return &ScanAttestConfig{
		Enabled:       true,
		RedisURL:      getEnv("SCAN_ATTEST_REDIS_URL", getEnv("FEDERATION_REDIS_URL", DefaultFederationRedisURL)),
		GatewayBuilds: getEnvAsSlice("SCAN_ATTEST_GATEWAY_BUILDS", []string{}),
		GatewayTier:   getEnv("SCAN_ATTEST_GATEWAY_TIER", "internal"),
		TTL:           getEnvAsDuration("SCAN_ATTEST_TTL", DefaultScanAttestTTL),
	}
}

// loadTargetsFromEnv loads forwarding targets from environment variables
func loadTargetsFromEnv() []*TargetConfiguration {
	targetsJSON := getEnv("FORWARDING_TARGETS", "")
	if targetsJSON == "" {
		return []*TargetConfiguration{}
	}

	var targets []*TargetConfiguration
	if err := json.Unmarshal([]byte(targetsJSON), &targets); err != nil {
		return []*TargetConfiguration{}
	}

	return targets
}

// applyEnvOverrides applies environment variable overrides to config
func applyEnvOverrides(config *Config) {
	if httpPort := os.Getenv("HTTP_PORT"); httpPort != "" {
		if port, err := strconv.Atoi(httpPort); err == nil {
			config.HTTPPort = port
		}
	}
	if grpcPort := os.Getenv("GRPC_PORT"); grpcPort != "" {
		if port, err := strconv.Atoi(grpcPort); err == nil {
			config.GRPCPort = port
		}
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.LogLevel = logLevel
	}
}

// GetForwardingTargetByID returns a forwarding target by ID
func (c *Config) GetForwardingTargetByID(id string) *TargetConfiguration {
	if c.Forwarding == nil {
		return nil
	}
	for _, target := range c.Forwarding.Targets {
		if target.ID == id {
			return target
		}
	}
	return nil
}

// AddForwardingTarget adds a new forwarding target
func (c *Config) AddForwardingTarget(target *TargetConfiguration) error {
	if c.Forwarding == nil {
		c.Forwarding = &ForwardingConfig{
			Enabled:              true,
			DefaultRetryAttempts: DefaultRetryAttempts,
			DefaultTimeout:       DefaultForwardTimeout,
			BufferSize:           DefaultBufferSize,
			Workers:              DefaultForwardingWorkers,
			Targets:              []*TargetConfiguration{},
		}
	}

	if c.GetForwardingTargetByID(target.ID) != nil {
		return fmt.Errorf("target with ID %s already exists", target.ID)
	}

	c.Forwarding.Targets = append(c.Forwarding.Targets, target)
	return nil
}

// RemoveForwardingTarget removes a forwarding target by ID
func (c *Config) RemoveForwardingTarget(id string) error {
	if c.Forwarding == nil {
		return fmt.Errorf("forwarding not configured")
	}

	for i, target := range c.Forwarding.Targets {
		if target.ID == id {
			c.Forwarding.Targets = append(c.Forwarding.Targets[:i], c.Forwarding.Targets[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("target with ID %s not found", id)
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as an integer with a default value
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsInt64 gets an environment variable as an int64 with a default value
func getEnvAsInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsDuration gets an environment variable as a duration with a default value
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// getEnvAsSlice gets an environment variable as a string slice with a default value
func getEnvAsSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}

// getEnvAsBool gets an environment variable as a boolean with a default value
func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		switch strings.ToLower(value) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return defaultValue
}
