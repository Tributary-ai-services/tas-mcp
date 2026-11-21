package toolevents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tributary-ai-services/tas-mcp/internal/federation"
)

// End to end through the Manager: a publisher whose Kafka is down, or wedged,
// must leave the tool call's response and latency untouched.
func TestManager_PublishFailureDoesNotAffectCall(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"result"}],"isError":false}`))
	}))
	defer upstream.Close()

	for _, tc := range []struct {
		name string
		w    *fakeWriter
	}{
		{"kafka error", &fakeWriter{err: errors.New("kafka: leader not available")}},
		{"kafka wedged", &fakeWriter{block: make(chan struct{})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := federation.NewManager(zap.NewNop(), federation.NewDiscovery(zap.NewNop()), federation.NewBridge(zap.NewNop()))
			if err := m.RegisterServer(&federation.MCPServer{
				ID: "s", Name: "s", Endpoint: upstream.URL, Protocol: federation.ProtocolHTTP,
				Auth: federation.AuthConfig{Type: federation.AuthNone},
			}); err != nil {
				t.Fatal(err)
			}
			pub := NewPublisher(tc.w, 1, nil)
			m.SetToolCallObserver(pub)

			start := time.Now()
			for i := 0; i < 20; i++ {
				resp, err := m.InvokeServer(context.Background(), "s", &federation.MCPRequest{
					ID: "1", Method: "tools/call", Params: map[string]interface{}{"name": "t"},
				})
				if err != nil || resp == nil || resp.Error != nil {
					t.Fatalf("call %d affected: %v %+v", i, err, resp)
				}
				content, _ := resp.Result.(map[string]interface{})["content"].([]interface{})
				if len(content) != 1 {
					t.Fatalf("result changed: %+v", resp.Result)
				}
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Fatalf("20 calls took %v with a failing publisher", d)
			}
			if tc.w.block != nil {
				close(tc.w.block)
			}
			_ = pub.Close()
		})
	}
}
