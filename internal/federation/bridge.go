// Package federation provides protocol bridging for MCP servers
package federation

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

const (
	// GRPCTimeout is the default timeout for gRPC calls
	GRPCTimeout = "30s"
	// KeepAliveValue is the keep-alive header value for SSE connections
	KeepAliveValue = "keep-alive"

	// BufferSizeMultiplier is the multiplier for buffer size in string conversions
	BufferSizeMultiplier = 2

	// GRPCInvalidArgument is the gRPC status code for invalid argument errors
	GRPCInvalidArgument = 3
	// GRPCNotFound is the gRPC status code for not found errors
	GRPCNotFound = 5
	// GRPCPermissionDenied is the gRPC status code for permission denied errors
	GRPCPermissionDenied = 7
	// GRPCUnavailable is the gRPC status code for unavailable errors
	GRPCUnavailable = 14
	// GRPCCanceled is the gRPC status code for canceled operations
	GRPCCanceled = 1

	// HTTPBadRequest is the HTTP status code for bad request errors
	HTTPBadRequest = 400
	// HTTPForbidden is the HTTP status code for forbidden errors
	HTTPForbidden = 403
	// HTTPNotFound is the HTTP status code for not found errors
	HTTPNotFound = 404
	// HTTPClientClosedRequest is the HTTP status code for client closed request
	HTTPClientClosedRequest = 499
	// HTTPServiceUnavailable is the HTTP status code for service unavailable errors
	HTTPServiceUnavailable = 503
	// HTTPInternalServerError is the HTTP status code for internal server errors
	HTTPInternalServerError = 500

	// GRPCInternal is the gRPC status code for internal errors
	GRPCInternal = 13

	// EventTypeResponse is the SSE event type for responses
	EventTypeResponse = "response"
	// FormatJSONRPC is the format for StdIO JSON-RPC
	FormatJSONRPC = "json-rpc"
)

// Bridge implements the ProtocolBridge interface with advanced translation capabilities
type Bridge struct {
	logger      *zap.Logger
	translators map[protocolPair]*ProtocolTranslator
}

// protocolPair represents a source-destination protocol pair
type protocolPair struct {
	from Protocol
	to   Protocol
}

// ProtocolTranslator handles translation between specific protocol pairs
type ProtocolTranslator struct {
	FromProtocol        Protocol
	ToProtocol          Protocol
	RequestTransformer  func(*MCPRequest) (*MCPRequest, error)
	ResponseTransformer func(*MCPResponse) (*MCPResponse, error)
}

// NewBridge creates a new protocol bridge
func NewBridge(logger *zap.Logger) *Bridge {
	bridge := &Bridge{
		logger:      logger,
		translators: make(map[protocolPair]*ProtocolTranslator),
	}

	// Register default translators
	bridge.registerDefaultTranslators()

	return bridge
}

// TranslateRequest translates a request between protocols
func (b *Bridge) TranslateRequest(_ context.Context, from, to Protocol, request *MCPRequest) (*MCPRequest, error) {
	if request == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	// If same protocol, return as-is
	if from == to {
		return request, nil
	}

	// Find translator
	pair := protocolPair{from: from, to: to}
	translator, exists := b.translators[pair]
	if !exists {
		return nil, fmt.Errorf("no translator available for %s -> %s", from, to)
	}

	b.logger.Debug("Translating request",
		zap.String("from", string(from)),
		zap.String("to", string(to)),
		zap.String("method", request.Method),
		zap.String("request_id", request.ID))

	// Apply transformation
	translatedRequest, err := translator.RequestTransformer(request)
	if err != nil {
		return nil, fmt.Errorf("request translation failed: %w", err)
	}

	// Add translation metadata
	if translatedRequest.Metadata == nil {
		translatedRequest.Metadata = make(map[string]string)
	}
	translatedRequest.Metadata["original_protocol"] = string(from)
	translatedRequest.Metadata["target_protocol"] = string(to)
	translatedRequest.Metadata["translated_at"] = time.Now().UTC().Format(time.RFC3339)

	return translatedRequest, nil
}

// TranslateResponse translates a response between protocols
func (b *Bridge) TranslateResponse(_ context.Context, from, to Protocol, response *MCPResponse) (*MCPResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("response cannot be nil")
	}

	// If same protocol, return as-is
	if from == to {
		return response, nil
	}

	// Find translator
	pair := protocolPair{from: from, to: to}
	translator, exists := b.translators[pair]
	if !exists {
		return nil, fmt.Errorf("no translator available for %s -> %s", from, to)
	}

	b.logger.Debug("Translating response",
		zap.String("from", string(from)),
		zap.String("to", string(to)),
		zap.String("response_id", response.ID))

	// Apply transformation
	translatedResponse, err := translator.ResponseTransformer(response)
	if err != nil {
		return nil, fmt.Errorf("response translation failed: %w", err)
	}

	// Add translation metadata
	if translatedResponse.Meta == nil {
		translatedResponse.Meta = make(map[string]interface{})
	}
	translatedResponse.Meta["original_protocol"] = string(from)
	translatedResponse.Meta["target_protocol"] = string(to)
	translatedResponse.Meta["translated_at"] = time.Now().UTC().Format(time.RFC3339)

	return translatedResponse, nil
}

// SupportsProtocol checks if a protocol is supported
func (b *Bridge) SupportsProtocol(protocol Protocol) bool {
	supportedProtocols := []Protocol{ProtocolHTTP, ProtocolGRPC, ProtocolSSE, ProtocolStdIO}
	for _, supported := range supportedProtocols {
		if protocol == supported {
			return true
		}
	}
	return false
}

// SupportedProtocols returns all supported protocols
func (b *Bridge) SupportedProtocols() []Protocol {
	return []Protocol{ProtocolHTTP, ProtocolGRPC, ProtocolSSE, ProtocolStdIO}
}

// AddTranslator adds a custom protocol translator
func (b *Bridge) AddTranslator(translator *ProtocolTranslator) error {
	if translator == nil {
		return fmt.Errorf("translator cannot be nil")
	}

	if translator.RequestTransformer == nil || translator.ResponseTransformer == nil {
		return fmt.Errorf("translator must have both request and response transformers")
	}

	pair := protocolPair{from: translator.FromProtocol, to: translator.ToProtocol}
	b.translators[pair] = translator

	b.logger.Info("Added protocol translator",
		zap.String("from", string(translator.FromProtocol)),
		zap.String("to", string(translator.ToProtocol)))

	return nil
}

// RemoveTranslator removes a protocol translator
func (b *Bridge) RemoveTranslator(from, to Protocol) {
	pair := protocolPair{from: from, to: to}
	delete(b.translators, pair)

	b.logger.Info("Removed protocol translator",
		zap.String("from", string(from)),
		zap.String("to", string(to)))
}

// ListTranslators returns all registered translators
func (b *Bridge) ListTranslators() []ProtocolTranslator {
	translators := make([]ProtocolTranslator, 0, len(b.translators))
	for _, translator := range b.translators {
		translators = append(translators, *translator)
	}
	return translators
}

// registerDefaultTranslators registers built-in protocol translators
func (b *Bridge) registerDefaultTranslators() {
	// HTTP <-> gRPC translation
	b.registerHTTPToGRPCTranslator()
	b.registerGRPCToHTTPTranslator()

	// HTTP <-> SSE translation
	b.registerHTTPToSSETranslator()
	b.registerSSEToHTTPTranslator()

	// gRPC <-> SSE translation
	b.registerGRPCToSSETranslator()
	b.registerSSEToGRPCTranslator()

	// StdIO translations
	b.registerStdIOTranslators()
}

// registerHTTPToGRPCTranslator registers HTTP to gRPC translator
func (b *Bridge) registerHTTPToGRPCTranslator() {
	translator := &ProtocolTranslator{
		FromProtocol: ProtocolHTTP,
		ToProtocol:   ProtocolGRPC,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			// Clone request
			translated := *req

			// HTTP to gRPC specific transformations
			if translated.Metadata == nil {
				translated.Metadata = make(map[string]string)
			}

			// Add gRPC-specific metadata
			translated.Metadata["grpc-timeout"] = GRPCTimeout

			// Transform method names if needed (HTTP uses kebab-case, gRPC uses PascalCase)
			translated.Method = transformMethodHTTPToGRPC(req.Method)

			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			// Clone response
			translated := *resp

			// gRPC to HTTP specific transformations
			// Handle gRPC status codes, convert to HTTP equivalents
			if resp.Error != nil {
				translated.Error = transformErrorGRPCToHTTP(resp.Error)
			}

			return &translated, nil
		},
	}

	_ = b.AddTranslator(translator)
}

// registerGRPCToHTTPTranslator registers gRPC to HTTP translator
func (b *Bridge) registerGRPCToHTTPTranslator() {
	translator := &ProtocolTranslator{
		FromProtocol: ProtocolGRPC,
		ToProtocol:   ProtocolHTTP,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			// Clone request
			translated := *req

			// gRPC to HTTP specific transformations
			translated.Method = transformMethodGRPCToHTTP(req.Method)

			// Remove gRPC-specific metadata
			if translated.Metadata != nil {
				delete(translated.Metadata, "grpc-timeout")
			}

			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			// Clone response
			translated := *resp

			// gRPC to HTTP specific transformations
			if resp.Error != nil {
				translated.Error = transformErrorGRPCToHTTP(resp.Error)
			}

			return &translated, nil
		},
	}

	_ = b.AddTranslator(translator)
}

// registerHTTPToSSETranslator registers HTTP to SSE translator
func (b *Bridge) registerHTTPToSSETranslator() {
	translator := &ProtocolTranslator{
		FromProtocol: ProtocolHTTP,
		ToProtocol:   ProtocolSSE,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			translated := *req

			// Add SSE-specific metadata
			if translated.Metadata == nil {
				translated.Metadata = make(map[string]string)
			}
			translated.Metadata["connection"] = KeepAliveValue
			translated.Metadata["cache-control"] = "no-cache"

			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			translated := *resp

			// Wrap response for SSE format
			if translated.Meta == nil {
				translated.Meta = make(map[string]interface{})
			}
			translated.Meta["event_type"] = EventTypeResponse

			return &translated, nil
		},
	}

	_ = b.AddTranslator(translator)
}

// registerSSEToHTTPTranslator registers SSE to HTTP translator
func (b *Bridge) registerSSEToHTTPTranslator() {
	translator := &ProtocolTranslator{
		FromProtocol: ProtocolSSE,
		ToProtocol:   ProtocolHTTP,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			translated := *req

			// Remove SSE-specific metadata
			if translated.Metadata != nil {
				delete(translated.Metadata, "connection")
				delete(translated.Metadata, "cache-control")
			}

			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			translated := *resp

			// Remove SSE-specific metadata
			if translated.Meta != nil {
				delete(translated.Meta, "event_type")
			}

			return &translated, nil
		},
	}

	_ = b.AddTranslator(translator)
}

// registerGRPCToSSETranslator registers gRPC to SSE translator
func (b *Bridge) registerGRPCToSSETranslator() {
	translator := &ProtocolTranslator{
		FromProtocol: ProtocolGRPC,
		ToProtocol:   ProtocolSSE,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			translated := *req

			// Transform gRPC method to SSE format
			translated.Method = transformMethodGRPCToSSE(req.Method)

			// Add SSE metadata
			if translated.Metadata == nil {
				translated.Metadata = make(map[string]string)
			}
			translated.Metadata["connection"] = KeepAliveValue

			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			translated := *resp

			// Convert gRPC streaming response to SSE events
			if translated.Meta == nil {
				translated.Meta = make(map[string]interface{})
			}
			translated.Meta["event_type"] = "grpc_response"

			return &translated, nil
		},
	}

	_ = b.AddTranslator(translator)
}

// registerSSEToGRPCTranslator registers SSE to gRPC translator
func (b *Bridge) registerSSEToGRPCTranslator() {
	translator := &ProtocolTranslator{
		FromProtocol: ProtocolSSE,
		ToProtocol:   ProtocolGRPC,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			translated := *req

			// Transform SSE method to gRPC format
			translated.Method = transformMethodSSEToGRPC(req.Method)

			// Add gRPC metadata
			if translated.Metadata == nil {
				translated.Metadata = make(map[string]string)
			}
			translated.Metadata["grpc-timeout"] = GRPCTimeout

			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			translated := *resp

			// Remove SSE-specific metadata
			if translated.Meta != nil {
				delete(translated.Meta, "event_type")
			}

			return &translated, nil
		},
	}

	_ = b.AddTranslator(translator)
}

// registerStdIOTranslators registers StdIO protocol translators
func (b *Bridge) registerStdIOTranslators() {
	// StdIO <-> HTTP
	httpToStdIO := &ProtocolTranslator{
		FromProtocol: ProtocolHTTP,
		ToProtocol:   ProtocolStdIO,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			translated := *req
			// StdIO uses JSON-RPC format
			if translated.Metadata == nil {
				translated.Metadata = make(map[string]string)
			}
			translated.Metadata["format"] = FormatJSONRPC
			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			return resp, nil // Pass through
		},
	}

	stdIOToHTTP := &ProtocolTranslator{
		FromProtocol: ProtocolStdIO,
		ToProtocol:   ProtocolHTTP,
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			translated := *req
			// Remove StdIO-specific metadata
			if translated.Metadata != nil {
				delete(translated.Metadata, "format")
			}
			return &translated, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			return resp, nil // Pass through
		},
	}

	_ = b.AddTranslator(httpToStdIO)
	_ = b.AddTranslator(stdIOToHTTP)
}

// Helper functions for method name transformations

func transformMethodHTTPToGRPC(method string) string {
	// Convert kebab-case to PascalCase
	// e.g., "get-servers" -> "GetServers"
	return toPascalCase(method)
}

func transformMethodGRPCToHTTP(method string) string {
	// Convert PascalCase to kebab-case
	// e.g., "GetServers" -> "get-servers"
	return toKebabCase(method)
}

func transformMethodGRPCToSSE(method string) string {
	// gRPC methods in SSE are prefixed
	return "grpc." + method
}

func transformMethodSSEToGRPC(method string) string {
	// Remove SSE prefix if present
	if len(method) > 5 && method[:5] == "grpc." {
		return method[5:]
	}
	return method
}

func transformErrorGRPCToHTTP(err *MCPError) *MCPError {
	translated := *err
	// Map gRPC status codes to HTTP equivalents
	switch err.Code {
	case GRPCCanceled: // CANCELED
		translated.Code = HTTPClientClosedRequest // Client Closed Request
	case GRPCInvalidArgument: // INVALID_ARGUMENT
		translated.Code = HTTPBadRequest // Bad Request
	case GRPCNotFound: // NOT_FOUND
		translated.Code = HTTPNotFound // Not Found
	case GRPCPermissionDenied: // PERMISSION_DENIED
		translated.Code = HTTPForbidden // Forbidden
	case GRPCUnavailable: // UNAVAILABLE
		translated.Code = HTTPServiceUnavailable // Service Unavailable
	default:
		translated.Code = HTTPInternalServerError // Internal Server Error
	}
	return &translated
}

// String conversion utilities
func toPascalCase(s string) string {
	// Simple implementation for demo
	// In production, use a proper case conversion library
	if s == "" {
		return s
	}

	result := make([]byte, 0, len(s))
	capitalizeNext := true

	for i := 0; i < len(s); i++ {
		if s[i] == '-' || s[i] == '_' {
			capitalizeNext = true
			continue
		}

		if capitalizeNext && s[i] >= 'a' && s[i] <= 'z' {
			result = append(result, s[i]-'a'+'A')
			capitalizeNext = false
		} else {
			result = append(result, s[i])
			capitalizeNext = false
		}
	}

	return string(result)
}

func toKebabCase(s string) string {
	// Simple implementation for demo
	if s == "" {
		return s
	}

	result := make([]byte, 0, len(s)*BufferSizeMultiplier)

	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			if i > 0 {
				result = append(result, '-')
			}
			result = append(result, s[i]-'A'+'a')
		} else {
			result = append(result, s[i])
		}
	}

	return string(result)
}
