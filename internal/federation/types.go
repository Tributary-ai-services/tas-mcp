// Package federation provides the core infrastructure for connecting to external MCP servers.
package federation

import (
	"context"
	"time"
)

// MCPServer represents an external MCP server that can be federated
type MCPServer struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Version      string            `json:"version"`
	Category     string            `json:"category"`
	Endpoint     string            `json:"endpoint"`
	Protocol     Protocol          `json:"protocol"`
	Auth         AuthConfig        `json:"auth"`
	Capabilities []string          `json:"capabilities"`
	Tags         []string          `json:"tags"`
	Metadata     map[string]string `json:"metadata"`
	Status       ServerStatus      `json:"status"`
	HealthCheck  HealthCheckConfig `json:"health_check"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// Protocol defines the communication protocol for MCP servers
type Protocol string

// Communication protocols supported by MCP servers
const (
	ProtocolHTTP  Protocol = "http"
	ProtocolGRPC  Protocol = "grpc"
	ProtocolSSE   Protocol = "sse"
	ProtocolStdIO Protocol = "stdio"
)

// ServerStatus represents the current status of an MCP server
type ServerStatus string

// Server status values
const (
	StatusUnknown     ServerStatus = "unknown"
	StatusHealthy     ServerStatus = "healthy"
	StatusUnhealthy   ServerStatus = "unhealthy"
	StatusMaintenance ServerStatus = "maintenance"
	StatusDeprecated  ServerStatus = "deprecated"
)

// AuthConfig defines authentication configuration for MCP servers
type AuthConfig struct {
	Type   AuthType          `json:"type"`
	Config map[string]string `json:"config"`
}

// AuthType defines the authentication method
type AuthType string

// Authentication types supported for MCP servers
const (
	AuthNone   AuthType = "none"
	AuthAPIKey AuthType = "api_key"
	AuthOAuth2 AuthType = "oauth2"
	AuthJWT    AuthType = "jwt"
	AuthBasic  AuthType = "basic"
	AuthBearer AuthType = "bearer"
)

// HealthCheckConfig defines health check configuration
type HealthCheckConfig struct {
	Enabled            bool          `json:"enabled"`
	Interval           time.Duration `json:"interval"`
	Timeout            time.Duration `json:"timeout"`
	HealthyThreshold   int           `json:"healthy_threshold"`
	UnhealthyThreshold int           `json:"unhealthy_threshold"`
	Path               string        `json:"path,omitempty"`
}

// MCPRequest represents a request to an MCP server
type MCPRequest struct {
	ID       string                 `json:"id"`
	Method   string                 `json:"method"`
	Params   map[string]interface{} `json:"params,omitempty"`
	Metadata map[string]string      `json:"metadata,omitempty"`
}

// MCPResponse represents a response from an MCP server
type MCPResponse struct {
	ID     string                 `json:"id"`
	Result interface{}            `json:"result,omitempty"`
	Error  *MCPError              `json:"error,omitempty"`
	Meta   map[string]interface{} `json:"meta,omitempty"`
}

// MCPError represents an error from an MCP server
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPService defines the interface for interacting with MCP servers
type MCPService interface {
	// Server information
	ID() string
	Name() string
	Category() string
	Capabilities() []string
	Status() ServerStatus

	// Core operations
	Invoke(ctx context.Context, request *MCPRequest) (*MCPResponse, error)
	Health(ctx context.Context) error

	// Lifecycle management
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// TASManager manages the collection of federated MCP servers
type TASManager interface {
	// Server registration
	RegisterServer(server *MCPServer) error
	UnregisterServer(id string) error
	GetServer(id string) (*MCPServer, error)
	ListServers() ([]*MCPServer, error)
	ListServersByCategory(category string) ([]*MCPServer, error)

	// Server operations
	InvokeServer(ctx context.Context, serverID string, request *MCPRequest) (*MCPResponse, error)
	BroadcastRequest(ctx context.Context, request *MCPRequest) ([]*MCPResponse, error)

	// Health monitoring
	CheckHealth(ctx context.Context, serverID string) error
	GetHealthStatus() (map[string]ServerStatus, error)

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// ServiceDiscovery defines the interface for discovering MCP servers
type ServiceDiscovery interface {
	// Discovery operations
	Discover(ctx context.Context) ([]*MCPServer, error)
	Watch(ctx context.Context, callback func(*MCPServer, DiscoveryEvent)) error

	// Configuration
	AddSource(source DiscoverySource) error
	RemoveSource(sourceID string) error
	ListSources() ([]DiscoverySource, error)
}

// DiscoveryEvent represents events from service discovery
type DiscoveryEvent string

// Discovery events for server lifecycle
const (
	EventServerAdded     DiscoveryEvent = "server_added"
	EventServerRemoved   DiscoveryEvent = "server_removed"
	EventServerUpdated   DiscoveryEvent = "server_updated"
	EventServerUnhealthy DiscoveryEvent = "server_unhealthy"
)

// DiscoverySource represents a source for discovering MCP servers
type DiscoverySource struct {
	ID       string            `json:"id"`
	Type     SourceType        `json:"type"`
	Config   map[string]string `json:"config"`
	Enabled  bool              `json:"enabled"`
	Priority int               `json:"priority"`
}

// SourceType defines the type of discovery source
type SourceType string

// Discovery source types for finding MCP servers
const (
	SourceStatic     SourceType = "static"
	SourceKubernetes SourceType = "kubernetes"
	SourceConsul     SourceType = "consul"
	SourceEtcd       SourceType = "etcd"
	SourceRegistry   SourceType = "registry"
	SourceDNS        SourceType = "dns"
)

// ProtocolBridge defines the interface for protocol translation
type ProtocolBridge interface {
	// Protocol translation
	TranslateRequest(ctx context.Context, from Protocol, to Protocol, request *MCPRequest) (*MCPRequest, error)
	TranslateResponse(ctx context.Context, from Protocol, to Protocol, response *MCPResponse) (*MCPResponse, error)

	// Protocol support
	SupportsProtocol(protocol Protocol) bool
	SupportedProtocols() []Protocol
}
