package federation

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus metrics for the federation layer (MCP-16).
//
// From 2026-11-03 to 2026-11-19 the production proxy could reach neither its
// Redis registry nor 11 of its 12 servers (MCP-14). It logged ~33,000
// `redis set: ... connection refused` lines a day and nothing alerted, because
// this package exported no metric at all: the only signal was a log line, and
// no rule watched the logs. These counters are what the alert rules in
// aether-shared/k8s-shared-infrastructure/monitoring.yaml read.
//
// Label cardinality is bounded: server_id is the registered set (12 today),
// kind and op are the fixed enums below.

// Error kinds. Keep this set small and closed; it is an alert label.
const (
	ErrKindConnectionRefused = "connection_refused"
	ErrKindTimeout           = "timeout"
	ErrKindDNS               = "dns"
	ErrKindHTTPStatus        = "http_status"
	ErrKindJSONRPC           = "jsonrpc"
	ErrKindCanceled          = "canceled"
	ErrKindOther             = "other"
)

var (
	// UpstreamRequests counts every call the proxy makes to a federated
	// server, by outcome (ok|error). It exists so an error count can be read
	// as a ratio, and so "zero errors" can be told apart from "zero traffic".
	UpstreamRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tas_mcp",
		Name:      "upstream_requests_total",
		Help:      "Calls from the MCP proxy to a federated server, by outcome.",
	}, []string{"server_id", "outcome"})

	// UpstreamErrors counts failed calls to a federated server, by kind.
	UpstreamErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tas_mcp",
		Name:      "upstream_errors_total",
		Help:      "Failed calls from the MCP proxy to a federated server, by error kind.",
	}, []string{"server_id", "kind"})

	// RegistryErrors counts failed registry operations (Redis in production).
	// ErrServerNotFound is a normal answer, not an error, and is not counted.
	RegistryErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tas_mcp",
		Name:      "registry_errors_total",
		Help:      "Failed MCP server registry operations, by operation and error kind.",
	}, []string{"op", "kind"})
)

func init() {
	prometheus.MustRegister(UpstreamRequests, UpstreamErrors, RegistryErrors)
}

// classifyError maps an error to one of the ErrKind constants.
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return ErrKindCanceled
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return ErrKindConnectionRefused
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrKindDNS
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ETIMEDOUT) {
		return ErrKindTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrKindTimeout
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return ErrKindTimeout
	}
	if errors.Is(err, errUpstreamStatus) {
		return ErrKindHTTPStatus
	}
	if errors.Is(err, errUpstreamJSONRPC) {
		return ErrKindJSONRPC
	}
	// go-redis surfaces some dial failures as plain strings.
	if strings.Contains(err.Error(), "connection refused") {
		return ErrKindConnectionRefused
	}
	return ErrKindOther
}

// Sentinels the HTTP and SSE clients wrap so classifyError can tell a server
// that answered badly from one that could not be reached.
var (
	errUpstreamStatus  = errors.New("upstream returned non-2xx status")
	errUpstreamJSONRPC = errors.New("upstream returned a JSON-RPC error")
)

func recordUpstream(serverID string, err error) {
	if err == nil {
		UpstreamRequests.WithLabelValues(serverID, "ok").Inc()
		return
	}
	UpstreamRequests.WithLabelValues(serverID, "error").Inc()
	UpstreamErrors.WithLabelValues(serverID, classifyError(err)).Inc()
}

func recordRegistry(op string, err error) error {
	if err != nil && !errors.Is(err, ErrServerNotFound) {
		RegistryErrors.WithLabelValues(op, classifyError(err)).Inc()
	}
	return err
}

// instrumentedRegistry counts registry failures without changing behavior.
type instrumentedRegistry struct{ Registry }

func newInstrumentedRegistry(r Registry) Registry {
	if r == nil {
		return nil
	}
	if _, ok := r.(*instrumentedRegistry); ok {
		return r
	}
	return &instrumentedRegistry{Registry: r}
}

func (r *instrumentedRegistry) Put(ctx context.Context, s *MCPServer) error {
	return recordRegistry("put", r.Registry.Put(ctx, s))
}

func (r *instrumentedRegistry) Get(ctx context.Context, id string) (*MCPServer, error) {
	s, err := r.Registry.Get(ctx, id)
	return s, recordRegistry("get", err)
}

func (r *instrumentedRegistry) Delete(ctx context.Context, id string) error {
	return recordRegistry("delete", r.Registry.Delete(ctx, id))
}

func (r *instrumentedRegistry) List(ctx context.Context) ([]*MCPServer, error) {
	s, err := r.Registry.List(ctx)
	return s, recordRegistry("list", err)
}

func (r *instrumentedRegistry) Watch(ctx context.Context) (<-chan RegistryEvent, error) {
	ch, err := r.Registry.Watch(ctx)
	return ch, recordRegistry("watch", err)
}
