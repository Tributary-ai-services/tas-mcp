package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.uber.org/zap"
)

// mcpStreamablePath is the default MCP Streamable-HTTP endpoint path. A server
// whose registered endpoint already carries a path keeps that path.
const mcpStreamablePath = "/mcp"

// SSEClient speaks the MCP Streamable-HTTP transport (protocol "sse"): a single
// JSON-RPC POST to the MCP endpoint, whose response is either a JSON object or an
// SSE stream. This covers modern MCP servers (e.g. DBHub) that the plain
// /mcp/tools/{list,call} HTTP client can't reach. See #22.
type SSEClient struct {
	server      *MCPServer
	logger      *zap.Logger
	httpClient  *http.Client
	authManager *AuthenticationManager
}

// NewSSEClient creates a Streamable-HTTP/SSE MCP client.
func NewSSEClient(server *MCPServer, logger *zap.Logger) (*SSEClient, error) {
	return &SSEClient{
		server: server,
		logger: logger,
		httpClient: &http.Client{
			Timeout: HTTPClientTimeout,
		},
		authManager: NewAuthenticationManager(logger),
	}, nil
}

// jsonRPCResponse is the subset of a JSON-RPC 2.0 response we consume.
type jsonRPCResponse struct {
	Result interface{}   `json:"result"`
	Error  *jsonRPCError `json:"error"`
	ID     interface{}   `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Call sends {method, params} as a JSON-RPC request to the MCP endpoint and
// returns the response's `result`, so MCPResponse.Result carries the real MCP
// payload (as with the plain HTTP client). tools/call params ({name, arguments})
// map straight to the JSON-RPC params.
func (c *SSEClient) Call(ctx context.Context, method string, params map[string]interface{}) (interface{}, error) {
	rpcReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if len(params) > 0 {
		rpcReq["params"] = params
	}
	payload, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc request: %w", err)
	}

	endpoint, err := streamableEndpoint(c.server.Endpoint)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Accept both so the server may answer with a single JSON object or an SSE stream.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if err := c.authManager.AddAuthentication(req, c.server.ID, c.server.Auth); err != nil {
		return nil, fmt.Errorf("apply auth for %s: %w", c.server.ID, err)
	}

	c.logger.Debug("Proxying MCP call (streamable)",
		zap.String("server_id", c.server.ID),
		zap.String("method", method),
		zap.String("url", endpoint))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", c.server.ID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProxyResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", c.server.ID, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s returned %s: %s", c.server.ID, resp.Status,
			string(data[:min(len(data), errBodyPreview)]))
	}

	rpc, err := parseJSONRPCResponse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.server.ID, err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("%s jsonrpc error %d: %s", c.server.ID, rpc.Error.Code, rpc.Error.Message)
	}
	return rpc.Result, nil
}

// Health does a lightweight reachability check (an initialize round-trip) when
// health checks are enabled.
func (c *SSEClient) Health(ctx context.Context) error {
	if !c.server.HealthCheck.Enabled {
		return nil
	}
	_, err := c.Call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "tas-mcp-gateway", "version": "1.0"},
	})
	return err
}

// Start is a no-op — the client is stateless per call.
func (c *SSEClient) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (c *SSEClient) Stop(_ context.Context) error { return nil }

// Configure applies runtime configuration (none needed).
func (c *SSEClient) Configure(_ map[string]string) error { return nil }

// streamableEndpoint resolves the MCP endpoint URL: an endpoint with no path (or
// just "/") gets the default /mcp; an explicit path is respected.
func streamableEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse endpoint %q: %w", raw, err)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = mcpStreamablePath
	}
	return u.String(), nil
}

// parseJSONRPCResponse decodes a Streamable-HTTP response body, which is either a
// bare JSON-RPC object or an SSE stream (event: message\ndata: {json}). For SSE
// it returns the last data payload that parses as a JSON-RPC response carrying a
// result or error.
func parseJSONRPCResponse(data []byte) (*jsonRPCResponse, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Bare JSON object.
	if trimmed[0] == '{' {
		var r jsonRPCResponse
		if err := json.Unmarshal(trimmed, &r); err != nil {
			return nil, fmt.Errorf("decode jsonrpc: %w", err)
		}
		return &r, nil
	}

	// SSE stream: accumulate data: lines per event (blank line = boundary) and
	// return the last event that parses as a JSON-RPC response with result/error.
	var (
		found *jsonRPCResponse
		buf   strings.Builder
	)
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		var r jsonRPCResponse
		if err := json.Unmarshal([]byte(buf.String()), &r); err == nil && (r.Result != nil || r.Error != nil) {
			found = &r
		}
		buf.Reset()
	}
	for _, line := range strings.Split(string(trimmed), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			buf.WriteString(strings.TrimPrefix(after, " "))
		}
	}
	flush()

	if found == nil {
		return nil, fmt.Errorf("no jsonrpc result in SSE stream")
	}
	return found, nil
}
