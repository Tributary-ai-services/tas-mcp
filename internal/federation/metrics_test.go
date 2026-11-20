package federation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"refused", fmt.Errorf("redis set: %w", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}), ErrKindConnectionRefused},
		{"refused string", errors.New("dial tcp 10.43.0.1:6379: connect: connection refused"), ErrKindConnectionRefused},
		{"deadline", fmt.Errorf("call x: %w", context.DeadlineExceeded), ErrKindTimeout},
		{"canceled", fmt.Errorf("call x: %w", context.Canceled), ErrKindCanceled},
		{"dns", fmt.Errorf("call x: %w", &net.DNSError{Err: "no such host", Name: "nope"}), ErrKindDNS},
		{"status", fmt.Errorf("x returned 502: %w: body", errUpstreamStatus), ErrKindHTTPStatus},
		{"jsonrpc", fmt.Errorf("x %w -32601: nope", errUpstreamJSONRPC), ErrKindJSONRPC},
		{"other", errors.New("boom"), ErrKindOther},
	}
	for _, tc := range cases {
		if got := classifyError(tc.err); got != tc.want {
			t.Errorf("%s: classifyError = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestInstrumentedRegistryCountsFailuresNotMisses(t *testing.T) {
	failing := &failingRegistry{Registry: NewMemoryRegistry(), err: errors.New("dial tcp: connect: connection refused")}
	reg := newInstrumentedRegistry(failing)

	before := testutil.ToFloat64(RegistryErrors.WithLabelValues("put", ErrKindConnectionRefused))
	_ = reg.Put(context.Background(), &MCPServer{ID: "a"})
	if got := testutil.ToFloat64(RegistryErrors.WithLabelValues("put", ErrKindConnectionRefused)) - before; got != 1 {
		t.Fatalf("put failure counted %v times, want 1", got)
	}

	// A miss is a normal answer and must not be counted.
	ok := newInstrumentedRegistry(NewMemoryRegistry())
	beforeGet := testutil.ToFloat64(RegistryErrors.WithLabelValues("get", ErrKindOther))
	if _, err := ok.Get(context.Background(), "absent"); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("expected ErrServerNotFound, got %v", err)
	}
	if got := testutil.ToFloat64(RegistryErrors.WithLabelValues("get", ErrKindOther)) - beforeGet; got != 0 {
		t.Fatalf("a registry miss was counted as an error")
	}

	if newInstrumentedRegistry(reg) != reg {
		t.Fatal("double wrapping should be a no-op")
	}
}

func TestInvokeRecordsUpstreamOutcome(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()

	server := &MCPServer{ID: "metrics-test-bad", Protocol: ProtocolHTTP, Endpoint: bad.URL, Auth: AuthConfig{Type: AuthNone}}
	svc, err := NewGenericService(server, zap.NewNop(), nil)
	if err != nil {
		t.Fatalf("NewGenericService: %v", err)
	}
	before := testutil.ToFloat64(UpstreamErrors.WithLabelValues(server.ID, ErrKindHTTPStatus))
	resp, _ := svc.Invoke(context.Background(), &MCPRequest{ID: "1", Method: "tools/list"})
	if resp != nil && resp.Error != nil {
		t.Logf("invoke error: %s (kind %s)", resp.Error.Message, classifyError(errors.New(resp.Error.Message)))
	}
	if got := testutil.ToFloat64(UpstreamErrors.WithLabelValues(server.ID, ErrKindHTTPStatus)) - before; got != 1 {
		t.Fatalf("http_status errors = %v, want 1", got)
	}
	if got := testutil.ToFloat64(UpstreamRequests.WithLabelValues(server.ID, "error")); got < 1 {
		t.Fatalf("upstream_requests_total{outcome=error} = %v, want >= 1", got)
	}

	// A closed port: the MCP-14 failure shape.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	closed := &MCPServer{ID: "metrics-test-closed", Protocol: ProtocolHTTP, Endpoint: "http://" + addr, Auth: AuthConfig{Type: AuthNone}}
	svc2, err := NewGenericService(closed, zap.NewNop(), nil)
	if err != nil {
		t.Fatalf("NewGenericService: %v", err)
	}
	_, _ = svc2.Invoke(context.Background(), &MCPRequest{ID: "2", Method: "tools/list"})
	if got := testutil.ToFloat64(UpstreamErrors.WithLabelValues(closed.ID, ErrKindConnectionRefused)); got != 1 {
		t.Fatalf("connection_refused errors = %v, want 1", got)
	}
}

type failingRegistry struct {
	Registry
	err error
}

func (f *failingRegistry) Put(context.Context, *MCPServer) error { return f.err }
