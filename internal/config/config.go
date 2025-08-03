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
	DefaultHTTPPort = 8080
	// DefaultGRPCPort is the default gRPC server port
	DefaultGRPCPort = 50051
	// DefaultHealthCheckPort is the default health check port
	DefaultHealthCheckPort = 8082
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
	Forwarding      *ForwardingConfig `json:"forwarding,omitempty"`
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

	return config
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
