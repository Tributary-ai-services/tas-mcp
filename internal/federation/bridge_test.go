package federation

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestNewBridge(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	if bridge == nil {
		t.Fatal("Expected bridge instance, got nil")
	}

	if bridge.translators == nil {
		t.Error("Expected translators map to be initialized")
	}

	// Check that default translators were registered
	translators := bridge.ListTranslators()
	if len(translators) == 0 {
		t.Error("Expected default translators to be registered")
	}
}

func TestSupportsProtocol(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	// Test supported protocols
	supportedProtocols := []Protocol{ProtocolHTTP, ProtocolGRPC, ProtocolSSE, ProtocolStdIO}
	for _, protocol := range supportedProtocols {
		if !bridge.SupportsProtocol(protocol) {
			t.Errorf("Expected protocol %s to be supported", protocol)
		}
	}

	// Test unsupported protocol
	if bridge.SupportsProtocol("unsupported") {
		t.Error("Expected unsupported protocol to return false")
	}
}

func TestSupportedProtocols(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	protocols := bridge.SupportedProtocols()
	expectedCount := 4 // HTTP, gRPC, SSE, StdIO

	if len(protocols) != expectedCount {
		t.Errorf("Expected %d supported protocols, got %d", expectedCount, len(protocols))
	}

	// Check all expected protocols are present
	protocolMap := make(map[Protocol]bool)
	for _, p := range protocols {
		protocolMap[p] = true
	}

	expected := []Protocol{ProtocolHTTP, ProtocolGRPC, ProtocolSSE, ProtocolStdIO}
	for _, p := range expected {
		if !protocolMap[p] {
			t.Errorf("Expected protocol %s to be in supported list", p)
		}
	}
}

func TestTranslateRequestSameProtocol(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	request := &MCPRequest{
		ID:     "test-1",
		Method: "test_method",
		Params: map[string]interface{}{"param": "value"},
	}

	// Same protocol should return original request
	translated, err := bridge.TranslateRequest(context.Background(), ProtocolHTTP, ProtocolHTTP, request)
	if err != nil {
		t.Fatalf("Expected successful translation for same protocol: %v", err)
	}

	if translated != request {
		t.Error("Expected same request instance for same protocol")
	}
}

func TestTranslateRequestNilRequest(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	_, err := bridge.TranslateRequest(context.Background(), ProtocolHTTP, ProtocolGRPC, nil)
	if err == nil {
		t.Error("Expected error for nil request")
	}
}

func TestTranslateRequestHTTPToGRPC(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	request := &MCPRequest{
		ID:     "test-1",
		Method: "get-servers",
		Params: map[string]interface{}{"param": "value"},
	}

	translated, err := bridge.TranslateRequest(context.Background(), ProtocolHTTP, ProtocolGRPC, request)
	if err != nil {
		t.Fatalf("Expected successful HTTP to gRPC translation: %v", err)
	}

	// Check method transformation
	if translated.Method != "GetServers" {
		t.Errorf("Expected method 'GetServers', got %s", translated.Method)
	}

	// Check metadata was added
	if translated.Metadata == nil {
		t.Fatal("Expected metadata to be set")
	}

	if translated.Metadata["grpc-timeout"] != "30s" {
		t.Error("Expected gRPC timeout metadata to be set")
	}

	if translated.Metadata["original_protocol"] != string(ProtocolHTTP) {
		t.Error("Expected original_protocol metadata to be set")
	}

	if translated.Metadata["target_protocol"] != string(ProtocolGRPC) {
		t.Error("Expected target_protocol metadata to be set")
	}
}

func TestTranslateRequestGRPCToHTTP(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	request := &MCPRequest{
		ID:     "test-1",
		Method: "GetServers",
		Params: map[string]interface{}{"param": "value"},
		Metadata: map[string]string{
			"grpc-timeout": "30s",
		},
	}

	translated, err := bridge.TranslateRequest(context.Background(), ProtocolGRPC, ProtocolHTTP, request)
	if err != nil {
		t.Fatalf("Expected successful gRPC to HTTP translation: %v", err)
	}

	// Check method transformation
	if translated.Method != "get-servers" {
		t.Errorf("Expected method 'get-servers', got %s", translated.Method)
	}

	// Check gRPC-specific metadata was removed
	if _, exists := translated.Metadata["grpc-timeout"]; exists {
		t.Error("Expected gRPC timeout metadata to be removed")
	}
}

func TestTranslateResponseSameProtocol(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	response := &MCPResponse{
		ID:     "test-1",
		Result: map[string]interface{}{"result": "value"},
	}

	translated, err := bridge.TranslateResponse(context.Background(), ProtocolHTTP, ProtocolHTTP, response)
	if err != nil {
		t.Fatalf("Expected successful translation for same protocol: %v", err)
	}

	if translated != response {
		t.Error("Expected same response instance for same protocol")
	}
}

func TestTranslateResponseNilResponse(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	_, err := bridge.TranslateResponse(context.Background(), ProtocolHTTP, ProtocolGRPC, nil)
	if err == nil {
		t.Error("Expected error for nil response")
	}
}

func TestTranslateResponseWithError(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	response := &MCPResponse{
		ID: "test-1",
		Error: &MCPError{
			Code:    3, // gRPC INVALID_ARGUMENT
			Message: "Invalid request",
		},
	}

	// gRPC to HTTP translation should transform error codes
	translated, err := bridge.TranslateResponse(context.Background(), ProtocolGRPC, ProtocolHTTP, response)
	if err != nil {
		t.Fatalf("Expected successful response translation: %v", err)
	}

	if translated.Error.Code != 400 { // HTTP Bad Request
		t.Errorf("Expected error code 400, got %d", translated.Error.Code)
	}

	// Check metadata was added
	if translated.Meta == nil {
		t.Fatal("Expected meta to be set")
	}

	if translated.Meta["original_protocol"] != string(ProtocolGRPC) {
		t.Error("Expected original_protocol meta to be set")
	}
}

func TestAddCustomTranslator(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	initialCount := len(bridge.ListTranslators())

	// Add custom translator
	translator := &ProtocolTranslator{
		FromProtocol: "custom1",
		ToProtocol:   "custom2",
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			return req, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			return resp, nil
		},
	}

	err := bridge.AddTranslator(translator)
	if err != nil {
		t.Fatalf("Expected successful translator addition: %v", err)
	}

	// Check translator was added
	if len(bridge.ListTranslators()) != initialCount+1 {
		t.Error("Expected translator count to increase")
	}
}

func TestAddInvalidTranslator(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	// Test nil translator
	err := bridge.AddTranslator(nil)
	if err == nil {
		t.Error("Expected error for nil translator")
	}

	// Test translator without request transformer
	translator := &ProtocolTranslator{
		FromProtocol:        "test1",
		ToProtocol:          "test2",
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) { return resp, nil },
	}

	err = bridge.AddTranslator(translator)
	if err == nil {
		t.Error("Expected error for translator without request transformer")
	}

	// Test translator without response transformer
	translator = &ProtocolTranslator{
		FromProtocol:       "test1",
		ToProtocol:         "test2",
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) { return req, nil },
	}

	err = bridge.AddTranslator(translator)
	if err == nil {
		t.Error("Expected error for translator without response transformer")
	}
}

func TestRemoveTranslator(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	// Add a custom translator
	translator := &ProtocolTranslator{
		FromProtocol: "test1",
		ToProtocol:   "test2",
		RequestTransformer: func(req *MCPRequest) (*MCPRequest, error) {
			return req, nil
		},
		ResponseTransformer: func(resp *MCPResponse) (*MCPResponse, error) {
			return resp, nil
		},
	}

	_ = bridge.AddTranslator(translator)
	initialCount := len(bridge.ListTranslators())

	// Remove the translator
	bridge.RemoveTranslator("test1", "test2")

	// Check translator was removed
	if len(bridge.ListTranslators()) != initialCount-1 {
		t.Error("Expected translator count to decrease")
	}

	// Try to use removed translator
	request := &MCPRequest{ID: "test", Method: "test"}
	_, err := bridge.TranslateRequest(context.Background(), "test1", "test2", request)
	if err == nil {
		t.Error("Expected error when using removed translator")
	}
}

func TestUnsupportedTranslation(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	request := &MCPRequest{
		ID:     "test-1",
		Method: "test_method",
	}

	// Try translation for unsupported protocol pair
	_, err := bridge.TranslateRequest(context.Background(), "unsupported1", "unsupported2", request)
	if err == nil {
		t.Error("Expected error for unsupported protocol translation")
	}
}

func TestMethodNameTransformations(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		function func(string) string
	}{
		{"get-servers", "GetServers", toPascalCase},
		{"list-mcp-servers", "ListMcpServers", toPascalCase},
		{"simple", "Simple", toPascalCase},
		{"GetServers", "get-servers", toKebabCase},
		{"ListMCPServers", "list-m-c-p-servers", toKebabCase},
		{"Simple", "simple", toKebabCase},
	}

	for _, test := range tests {
		result := test.function(test.input)
		if result != test.expected {
			t.Errorf("For input %s, expected %s, got %s", test.input, test.expected, result)
		}
	}
}

func TestErrorCodeTransformations(t *testing.T) {
	// Test gRPC to HTTP error transformation
	grpcError := &MCPError{Code: 3, Message: "Invalid argument"} // gRPC INVALID_ARGUMENT
	httpError := transformErrorGRPCToHTTP(grpcError)
	if httpError.Code != 400 { // HTTP Bad Request
		t.Errorf("Expected HTTP error code 400, got %d", httpError.Code)
	}
}

func TestSSETranslations(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	// Test HTTP to SSE
	request := &MCPRequest{
		ID:     "test-1",
		Method: "test_method",
	}

	translated, err := bridge.TranslateRequest(context.Background(), ProtocolHTTP, ProtocolSSE, request)
	if err != nil {
		t.Fatalf("Expected successful HTTP to SSE translation: %v", err)
	}

	if translated.Metadata["connection"] != "keep-alive" {
		t.Error("Expected SSE connection metadata to be set")
	}

	// Test SSE response
	response := &MCPResponse{
		ID:     "test-1",
		Result: "test result",
	}

	translatedResp, err := bridge.TranslateResponse(context.Background(), ProtocolHTTP, ProtocolSSE, response)
	if err != nil {
		t.Fatalf("Expected successful HTTP to SSE response translation: %v", err)
	}

	if translatedResp.Meta["event_type"] != "response" {
		t.Error("Expected SSE event_type metadata to be set")
	}
}

func TestStdIOTranslations(t *testing.T) {
	logger := zap.NewNop()
	bridge := NewBridge(logger)

	// Test HTTP to StdIO
	request := &MCPRequest{
		ID:     "test-1",
		Method: "test_method",
	}

	translated, err := bridge.TranslateRequest(context.Background(), ProtocolHTTP, ProtocolStdIO, request)
	if err != nil {
		t.Fatalf("Expected successful HTTP to StdIO translation: %v", err)
	}

	if translated.Metadata["format"] != "json-rpc" {
		t.Error("Expected StdIO format metadata to be set")
	}

	// Test StdIO to HTTP
	stdIORequest := &MCPRequest{
		ID:     "test-2",
		Method: "test_method",
		Metadata: map[string]string{
			"format": "json-rpc",
		},
	}

	translated2, err := bridge.TranslateRequest(context.Background(), ProtocolStdIO, ProtocolHTTP, stdIORequest)
	if err != nil {
		t.Fatalf("Expected successful StdIO to HTTP translation: %v", err)
	}

	if _, exists := translated2.Metadata["format"]; exists {
		t.Error("Expected format metadata to be removed")
	}
}
