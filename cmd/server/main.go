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

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
	"github.com/tributary-ai-services/tas-mcp/internal/forwarding"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
	httpserver "github.com/tributary-ai-services/tas-mcp/internal/http"
	"github.com/tributary-ai-services/tas-mcp/internal/logger"
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

	grpcServer, mcpServer := setupGRPCServer(cfg, zapLogger, forwarder)
	httpServerInstance := setupHTTPServer(cfg, zapLogger, mcpServer, forwarder)
	healthHTTPServer := setupHealthServer(cfg, zapLogger, mcpServer, forwarder)

	waitForShutdown(zapLogger, httpServerInstance, healthHTTPServer, grpcServer)
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

func setupHTTPServer(
	cfg *config.Config,
	zapLogger *zap.Logger,
	mcpServer *grpcserver.MCPServer,
	forwarder *forwarding.EventForwarder,
) *http.Server {
	// Create HTTP server
	httpSrv := httpserver.NewServer(zapLogger, mcpServer, forwarder)
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
