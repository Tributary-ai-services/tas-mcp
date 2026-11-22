// Package main implements the TAS MCP server executable.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
	"github.com/tributary-ai-services/tas-mcp/internal/federation"
	"github.com/tributary-ai-services/tas-mcp/internal/forwarding"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
	httpserver "github.com/tributary-ai-services/tas-mcp/internal/http"
	"github.com/tributary-ai-services/tas-mcp/internal/identity"
	"github.com/tributary-ai-services/tas-mcp/internal/logger"
	"github.com/tributary-ai-services/tas-mcp/internal/reduction"
	"github.com/tributary-ai-services/tas-mcp/internal/scanning"
	"github.com/tributary-ai-services/tas-mcp/internal/toolevents"
)

// Server configuration constants
const (
	// MaxMessageSize is the maximum gRPC message size (4MB)
	MaxMessageSize = 4 * 1024 * 1024
	// HTTPReadTimeout is the timeout for reading HTTP requests
	HTTPReadTimeout = 30 * time.Second
	// HTTPWriteTimeout is the timeout for writing HTTP responses
	HTTPWriteTimeout = 30 * time.Second
	// HTTPIdleTimeout is the timeout for idle HTTP connections
	HTTPIdleTimeout = 120 * time.Second
	// HealthReadHeaderTimeout is the timeout for reading headers on health check endpoint
	HealthReadHeaderTimeout = 10 * time.Second
	// ShutdownTimeout is the timeout for graceful shutdown
	ShutdownTimeout = 30 * time.Second
)

func main() {
	cfg, zapLogger := initializeApp()
	defer func() { _ = zapLogger.Sync() }()

	zapLogger.Info("Starting TAS MCP Server",
		zap.String("version", cfg.Version),
		zap.Int("http_port", cfg.HTTPPort),
		zap.Int("grpc_port", cfg.GRPCPort),
		zap.String("log_level", cfg.LogLevel))

	forwarder := initializeForwarder(cfg, zapLogger)
	if forwarder != nil {
		defer forwarder.Stop()
	}

	federationManager, reducer := setupFederation(cfg, zapLogger)
	if reducer != nil {
		defer func() { _ = reducer.Close() }()
	}
	pub := installToolCallEvents(federationManager, cfg, zapLogger)
	if pub != nil {
		defer func() { _ = pub.Close() }()
	}
	if stop := installServerProbes(federationManager, cfg, pub, zapLogger); stop != nil {
		defer stop()
	}

	grpcServer, mcpServer := setupGRPCServer(cfg, zapLogger, forwarder)
	httpServerInstance := setupHTTPServer(cfg, zapLogger, mcpServer, forwarder, federationManager)
	healthHTTPServer := setupHealthServer(cfg, zapLogger, mcpServer, forwarder)

	waitForShutdown(zapLogger, httpServerInstance, healthHTTPServer, grpcServer)
}

// setupFederation constructs the federation Manager that this process hosts as
// the MCP gateway, and turns on cache-safe reduce-at-source for federated tool
// results. The Manager is built once and owned by main so its server registry
// can be populated and mutated at runtime — the planned FederatedMCPServer CRD
// controller registers/unregisters downstream servers by calling
// mgr.RegisterServer / mgr.UnregisterServer on this same instance. The registry
// starts empty; federation routes (/api/v1/federation/*) are live immediately
// and return the current (initially empty) set.
//
// reduce-at-source is installed on the concrete *Manager before it is handed to
// the HTTP server as a federation.TASManager (SetResultProcessor lives on the
// concrete type). It no-ops unless REDUCTION_ENABLED. Returns the *Reducer (nil
// when disabled) so main can Close it on shutdown.
func setupFederation(cfg *config.Config, zapLogger *zap.Logger) (*federation.Manager, *reduction.Reducer) {
	registry := buildFederationRegistry(cfg, zapLogger)
	mgr := federation.NewManagerWithRegistry(
		zapLogger, federation.NewDiscovery(zapLogger), federation.NewBridge(zapLogger), registry)

	reducer := reduction.Install(mgr, reduction.FromConfig(cfg.Reduction), zapLogger)

	// Gatekeeper boundary scanning of federated tool results (G2). Independent
	// of reduction: scanning is the security control and runs on every external
	// tool result. No-ops unless SCANNING_ENABLED. The scanner holds no
	// resources, so it isn't threaded through shutdown.
	scanner := scanning.Install(mgr, scanning.FromConfig(cfg.Scanning), zapLogger)

	// Start the manager lifecycle (health monitoring + the registry Watch that
	// converges local services when another replica unregisters a server).
	if err := mgr.Start(context.Background()); err != nil {
		zapLogger.Error("Failed to start federation manager", zap.Error(err))
	}

	zapLogger.Info("Federation gateway enabled",
		zap.Bool("reduce_at_source", reducer != nil),
		zap.Bool("boundary_scanning", scanner != nil))

	return mgr, reducer
}

// buildFederationRegistry selects the registry backend from config. Default is
// the in-process memory registry; FEDERATION_REGISTRY=redis uses a shared Redis
// registry so multiple gateway replicas agree on the federated set. A redis
// init failure falls back to memory rather than blocking startup.
func buildFederationRegistry(cfg *config.Config, zapLogger *zap.Logger) federation.Registry {
	if cfg.Federation != nil && cfg.Federation.Registry == "redis" {
		reg, err := federation.NewRedisRegistryFromURL(cfg.Federation.RedisURL)
		if err != nil {
			zapLogger.Error("federation: redis registry init failed; using in-memory",
				zap.Error(err))
			return federation.NewMemoryRegistry()
		}
		zapLogger.Info("federation registry: redis (shared across replicas)",
			zap.String("redis_url", cfg.Federation.RedisURL))
		return reg
	}
	return federation.NewMemoryRegistry()
}

func initializeApp() (*config.Config, *zap.Logger) {
	var (
		configFile = flag.String("config", "", "Configuration file path")
		version    = flag.Bool("version", false, "Show version information")
	)
	flag.Parse()

	if *version {
		fmt.Printf("TAS MCP Server v%s\n", getVersion())
		os.Exit(0)
	}

	// Load configuration
	var cfg *config.Config
	var err error

	if *configFile != "" {
		cfg, err = config.LoadFromFile(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config from file: %v", err)
		}
	} else {
		cfg = config.Load()
	}

	// Initialize logger
	zapLogger, err := logger.NewLogger(cfg.LogLevel)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	return cfg, zapLogger
}

func initializeForwarder(cfg *config.Config, zapLogger *zap.Logger) *forwarding.EventForwarder {
	if cfg.Forwarding == nil || !cfg.Forwarding.Enabled {
		return nil
	}

	forwarder := forwarding.NewEventForwarder(zapLogger, cfg.Forwarding)
	if err := forwarder.Start(); err != nil {
		zapLogger.Fatal("Failed to start event forwarder", zap.Error(err))
	}

	zapLogger.Info("Event forwarding enabled",
		zap.Int("targets", len(cfg.Forwarding.Targets)),
		zap.Int("workers", cfg.Forwarding.Workers))

	return forwarder
}

func setupGRPCServer(
	cfg *config.Config,
	zapLogger *zap.Logger,
	forwarder *forwarding.EventForwarder,
) (*grpc.Server, *grpcserver.MCPServer) {
	// Create gRPC server
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(MaxMessageSize),
		grpc.MaxSendMsgSize(MaxMessageSize),
	)

	// Create and register MCP service
	mcpServer := grpcserver.NewMCPServer(zapLogger, forwarder)
	mcpv1.RegisterMCPServiceServer(grpcServer, mcpServer)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable gRPC reflection for debugging
	reflection.Register(grpcServer)

	// Start gRPC server
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		zapLogger.Fatal("Failed to listen on gRPC port", zap.Error(err))
	}

	go func() {
		zapLogger.Info("Starting gRPC server", zap.Int("port", cfg.GRPCPort))
		if err := grpcServer.Serve(grpcListener); err != nil {
			zapLogger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	return grpcServer, mcpServer
}

// installToolCallEvents wires the com.tas.aiqg.tool_call.v1 publisher into the
// federation Manager (Plan #22 T.2). Nil when TOOL_CALL_EVENTS_ENABLED is
// unset, or when it is set without brokers (logged at Warn, not fatal: a
// missing event stream must not take the proxy down).
//
// The same publisher carries tool-catalogue events (T.4) when only
// TOOL_CATALOGUE_EVENTS_ENABLED is set; then it is built but not attached as
// the tool-call observer.
func installToolCallEvents(mgr *federation.Manager, cfg *config.Config, zapLogger *zap.Logger) *toolevents.Publisher {
	calls := cfg.ToolCallEvents != nil && cfg.ToolCallEvents.Enabled
	cats := cfg.ToolCatalogueEvents != nil && cfg.ToolCatalogueEvents.Enabled
	if mgr == nil || (!calls && !cats) {
		return nil
	}
	c := cfg.ToolCallEvents
	if !calls {
		c = cfg.ToolCatalogueEvents
	}
	pub, err := toolevents.NewKafkaPublisher(c.Brokers, c.Topic, c.BufferSize, zapLogger)
	if err != nil {
		zapLogger.Warn("tool events: not publishing", zap.Error(err))
		return nil
	}
	if calls {
		mgr.SetToolCallObserver(pub)
	}
	zapLogger.Info("tool events: enabled",
		zap.Bool("tool_calls", calls), zap.Bool("tool_catalogue", cats),
		zap.Strings("brokers", c.Brokers), zap.String("topic", c.Topic), zap.Int("buffer", c.BufferSize))
	return pub
}

// installServerProbes starts the federated-server prober (Plan #22 T.4) and,
// when TOOL_CATALOGUE_EVENTS_ENABLED and a publisher exists, the catalogue
// emitter. Returns a stop func, or nil when SERVER_PROBES_ENABLED is unset.
func installServerProbes(mgr *federation.Manager, cfg *config.Config, pub *toolevents.Publisher, zapLogger *zap.Logger) func() {
	c := cfg.ServerProbes
	if mgr == nil || c == nil || !c.Enabled {
		if cfg.ToolCatalogueEvents != nil && cfg.ToolCatalogueEvents.Enabled {
			zapLogger.Warn("tool catalogue events: enabled but SERVER_PROBES_ENABLED is not; no catalogue will be published")
		}
		return nil
	}
	p := federation.NewProber(mgr, federation.ProbeConfig{
		Interval: c.Interval, Timeout: c.Timeout, Concurrency: c.Concurrency,
	}, zapLogger)
	if pub != nil && cfg.ToolCatalogueEvents != nil && cfg.ToolCatalogueEvents.Enabled {
		p.SetCatalogueObserver(toolevents.NewCatalogueEmitter(pub))
	}
	mgr.SetProber(p)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	zapLogger.Info("server probes: enabled",
		zap.Duration("interval", c.Interval), zap.Duration("timeout", c.Timeout), zap.Int("concurrency", c.Concurrency))
	return func() {
		cancel()
		<-done
	}
}

// buildIdentityResolver returns the caller-identity resolver for the federation
// routes (Plan #22 T.1), or nil when IDENTITY_ENABLED is unset — then the proxy
// reads no TAS-Auth and calls no validator. Enabled without a dashboard URL or
// internal-auth token still resolves headers, but every token is recorded
// validator_unconfigured (unattributed); that is logged at Warn, not fatal.
func buildIdentityResolver(cfg *config.Config, zapLogger *zap.Logger) *identity.Resolver {
	if cfg.Identity == nil || !cfg.Identity.Enabled {
		return nil
	}
	r := &identity.Resolver{Logger: zapLogger}
	v, err := identity.NewDashboardValidator(cfg.Identity.DashboardURL, cfg.Identity.DashboardInternalAuthToken)
	if err != nil {
		zapLogger.Warn("caller identity: validator not configured; every call will be unattributed", zap.Error(err))
		return r
	}
	r.Validator = identity.NewCachingValidator(v, cfg.Identity.CacheTTL)
	zapLogger.Info("caller identity: enabled (observe mode)",
		zap.String("dashboard_url", cfg.Identity.DashboardURL),
		zap.Duration("cache_ttl", cfg.Identity.CacheTTL))
	return r
}

func setupHTTPServer(
	cfg *config.Config,
	zapLogger *zap.Logger,
	mcpServer *grpcserver.MCPServer,
	forwarder *forwarding.EventForwarder,
	federationManager federation.TASManager,
) *http.Server {
	// Create HTTP server with the federation gateway wired in, so tool-call
	// requests reach Manager.InvokeServer (the reduce-at-source choke point).
	httpSrv := httpserver.NewServerWithFederation(zapLogger, mcpServer, forwarder, federationManager)
	httpSrv.SetIdentityResolver(buildIdentityResolver(cfg, zapLogger))
	httpServerInstance := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      httpSrv.Handler(),
		ReadTimeout:  HTTPReadTimeout,
		WriteTimeout: HTTPWriteTimeout,
		IdleTimeout:  HTTPIdleTimeout,
	}

	// Start HTTP server
	go func() {
		zapLogger.Info("Starting HTTP server", zap.Int("port", cfg.HTTPPort))
		if err := httpServerInstance.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zapLogger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	return httpServerInstance
}

func setupHealthServer(
	cfg *config.Config,
	zapLogger *zap.Logger,
	mcpServer *grpcserver.MCPServer,
	forwarder *forwarding.EventForwarder,
) *http.Server {
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		stats := mcpServer.GetStats()
		response := map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC(),
			"uptime":    time.Since(stats.StartTime).Seconds(),
			"stats": map[string]interface{}{
				"total_events":     stats.TotalEvents,
				"stream_events":    stats.StreamEvents,
				"forwarded_events": stats.ForwardedEvents,
				"error_events":     stats.ErrorEvents,
				"active_streams":   stats.ActiveStreams,
			},
		}

		if forwarder != nil {
			forwardingMetrics := forwarder.GetMetrics()
			response["forwarding"] = map[string]interface{}{
				"total_events":     forwardingMetrics.TotalEvents,
				"forwarded_events": forwardingMetrics.ForwardedEvents,
				"failed_events":    forwardingMetrics.FailedEvents,
				"dropped_events":   forwardingMetrics.DroppedEvents,
				"targets":          len(forwarder.GetTargets()),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{
			"status": "%s",
			"timestamp": "%s",
			"uptime": %.2f,
			"stats": %v
		}`,
			response["status"],
			response["timestamp"],
			response["uptime"],
			response["stats"])
	})

	// Prometheus scrape target (MCP-16). The pod annotations have always
	// pointed at :8083/metrics; until now nothing was served there.
	healthMux.Handle("/metrics", promhttp.Handler())

	healthMux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	healthHTTPServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HealthCheckPort),
		Handler:           healthMux,
		ReadHeaderTimeout: HealthReadHeaderTimeout,
	}

	go func() {
		zapLogger.Info("Starting health check server", zap.Int("port", cfg.HealthCheckPort))
		if err := healthHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zapLogger.Error("Health check server failed", zap.Error(err))
		}
	}()

	return healthHTTPServer
}

func waitForShutdown(zapLogger *zap.Logger, httpServer, healthServer *http.Server, grpcServer *grpc.Server) {
	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	zapLogger.Info("Shutting down servers...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(ctx); err != nil {
		zapLogger.Error("HTTP server shutdown error", zap.Error(err))
	}

	// Shutdown health server
	if err := healthServer.Shutdown(ctx); err != nil {
		zapLogger.Error("Health server shutdown error", zap.Error(err))
	}

	// Shutdown gRPC server
	grpcServer.GracefulStop()

	zapLogger.Info("Server shutdown complete")
}

func getVersion() string {
	if version := os.Getenv("VERSION"); version != "" {
		return version
	}
	return "1.0.0-dev"
}
