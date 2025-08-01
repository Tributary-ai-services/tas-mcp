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

func main() {
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
	defer zapLogger.Sync()

	zapLogger.Info("Starting TAS MCP Server",
		logger.String("version", cfg.Version),
		logger.Int("http_port", cfg.HTTPPort),
		logger.Int("grpc_port", cfg.GRPCPort),
		logger.String("log_level", cfg.LogLevel))

	// Initialize event forwarder if enabled
	var forwarder *forwarding.EventForwarder
	if cfg.Forwarding != nil && cfg.Forwarding.Enabled {
		forwarder = forwarding.NewEventForwarder(zapLogger, cfg.Forwarding)
		if err := forwarder.Start(); err != nil {
			zapLogger.Fatal("Failed to start event forwarder", logger.Error(err))
		}
		defer forwarder.Stop()
		
		zapLogger.Info("Event forwarding enabled",
			logger.Int("targets", len(cfg.Forwarding.Targets)),
			logger.Int("workers", cfg.Forwarding.Workers))
	}

	// Create gRPC server
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(4*1024*1024), // 4MB
		grpc.MaxSendMsgSize(4*1024*1024), // 4MB
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
		zapLogger.Fatal("Failed to listen on gRPC port", logger.Error(err))
	}

	go func() {
		zapLogger.Info("Starting gRPC server", logger.Int("port", cfg.GRPCPort))
		if err := grpcServer.Serve(grpcListener); err != nil {
			zapLogger.Fatal("gRPC server failed", logger.Error(err))
		}
	}()

	// Create HTTP server
	httpSrv := httpserver.NewServer(zapLogger, mcpServer, forwarder)
	httpServerInstance := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      httpSrv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start HTTP server
	go func() {
		zapLogger.Info("Starting HTTP server", logger.Int("port", cfg.HTTPPort))
		if err := httpServerInstance.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zapLogger.Fatal("HTTP server failed", logger.Error(err))
		}
	}()

	// Create health check server
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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
		fmt.Fprintf(w, `{
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

	healthMux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	healthServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HealthCheckPort),
		Handler: healthMux,
	}

	go func() {
		zapLogger.Info("Starting health check server", logger.Int("port", cfg.HealthCheckPort))
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zapLogger.Error("Health check server failed", logger.Error(err))
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	zapLogger.Info("Shutting down servers...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := httpServerInstance.Shutdown(ctx); err != nil {
		zapLogger.Error("HTTP server shutdown error", logger.Error(err))
	}

	// Shutdown health server
	if err := healthServer.Shutdown(ctx); err != nil {
		zapLogger.Error("Health server shutdown error", logger.Error(err))
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