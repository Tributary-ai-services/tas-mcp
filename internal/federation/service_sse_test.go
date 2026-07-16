package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

// newStreamableMCPServer serves the MCP Streamable-HTTP transport at /mcp. It
// answers tools/list with a canned result; when the request Accept prefers SSE
// (and sseMode is true) it replies as an event stream, otherwise as JSON.
func newStreamableMCPServer(t *testing.T, sseMode bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		result := `{"tools":[{"name":"execute_sql"}]}`
		if req.Method == "boom" {
			body := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"kaboom"}}`
			writeStreamable(w, sseMode, body)
			return
		}
		writeStreamable(w, sseMode, `{"jsonrpc":"2.0","id":1,"result":`+result+`}`)
	})
	return httptest.NewServer(mux)
}

func writeStreamable(w http.ResponseWriter, sseMode bool, jsonBody string) {
	if sseMode {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: " + jsonBody + "\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonBody))
}

func TestSSEClient_Call_JSON(t *testing.T) {
	ts := newStreamableMCPServer(t, false)
	defer ts.Close()

	server := createTestServer()
	server.Endpoint = ts.URL
	server.Protocol = ProtocolSSE
	client, _ := NewSSEClient(server, zap.NewNop())

	res, err := client.Call(context.Background(), methodToolsList, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if m, ok := res.(map[string]interface{}); !ok || m["tools"] == nil {
		t.Errorf("expected tools in result, got %+v", res)
	}
}

func TestSSEClient_Call_SSEStream(t *testing.T) {
	ts := newStreamableMCPServer(t, true) // server replies as an SSE stream
	defer ts.Close()

	server := createTestServer()
	server.Endpoint = ts.URL
	server.Protocol = ProtocolSSE
	client, _ := NewSSEClient(server, zap.NewNop())

	res, err := client.Call(context.Background(), methodToolsList, nil)
	if err != nil {
		t.Fatalf("Call (SSE): %v", err)
	}
	if m, ok := res.(map[string]interface{}); !ok || m["tools"] == nil {
		t.Errorf("expected tools parsed from SSE stream, got %+v", res)
	}
}

func TestSSEClient_Call_JSONRPCError(t *testing.T) {
	ts := newStreamableMCPServer(t, false)
	defer ts.Close()

	server := createTestServer()
	server.Endpoint = ts.URL
	server.Protocol = ProtocolSSE
	client, _ := NewSSEClient(server, zap.NewNop())

	if _, err := client.Call(context.Background(), "boom", nil); err == nil {
		t.Error("a jsonrpc error response should surface as an error")
	}
}

// The Manager builds an SSEClient for protocol "sse" and invokes it end-to-end.
func TestInvokeServer_SSEProtocol(t *testing.T) {
	ts := newStreamableMCPServer(t, false)
	defer ts.Close()

	manager := createTestManager()
	server := createTestServer()
	server.Endpoint = ts.URL
	server.Protocol = ProtocolSSE
	if err := manager.registry.Put(context.Background(), server); err != nil {
		t.Fatalf("registry.Put: %v", err)
	}

	resp, err := manager.InvokeServer(context.Background(), server.ID,
		&MCPRequest{ID: "1", Method: methodToolsList})
	if err != nil {
		t.Fatalf("InvokeServer: %v", err)
	}
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected a successful sse invoke, got %+v", resp)
	}
	if m, ok := resp.Result.(map[string]interface{}); !ok || m["tools"] == nil {
		t.Errorf("expected tools payload, got %+v", resp.Result)
	}
}

func TestStreamableEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://dbhub:8080":     "http://dbhub:8080/mcp",
		"http://dbhub:8080/":    "http://dbhub:8080/mcp",
		"http://dbhub:8080/mcp": "http://dbhub:8080/mcp",
		"http://dbhub:8080/rpc": "http://dbhub:8080/rpc", // explicit path respected
	}
	for in, want := range cases {
		got, err := streamableEndpoint(in)
		if err != nil || got != want {
			t.Errorf("streamableEndpoint(%q) = %q,%v; want %q", in, got, err, want)
		}
	}
}
