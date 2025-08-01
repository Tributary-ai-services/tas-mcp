package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/tributary-ai-services/tas-mcp/internal/config"
	grpcserver "github.com/tributary-ai-services/tas-mcp/internal/grpc"
	httpserver "github.com/tributary-ai-services/tas-mcp/internal/http"
	"github.com/tributary-ai-services/tas-mcp/internal/logger"
	pb "github.com/tributary-ai-services/tas-mcp/proto/gen/go/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var version = "dev" // Will be set by build process

func main() {
	// Load configuration
	cfg := config.Load()
	if cfg.Version == "dev" {
		cfg.Version = version
	}

	// Set up logger
	log := logger.New(cfg.LogLevel)
	defer log.Sync()

	log.Info("Starting TAS MCP Server",
		zap.String("version", cfg.Version),
		zap.Int("http_port", cfg.HTTPPort),
		zap.Int("grpc_port", cfg.GRPCPort),
		zap.Int("health_port", cfg.HealthCheckPort),
		zap.String("log_level", cfg.LogLevel),
		zap.Strings("forward_to", cfg.ForwardTo))

	// Create event channel
	eventChannel := make(chan *pb.MCPEvent, cfg.BufferSize)
	
	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startTime := time.Now()
	var wg sync.WaitGroup

	// Start HTTP server
	httpSrv := httpserver.NewServer(log, eventChannel, cfg.Version)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpSrv.Start(ctx, cfg.HTTPPort); err != nil {
			log.Error("HTTP server failed", zap.Error(err))
		}
	}()

	// Start gRPC server
	grpcSrv := grpcserver.NewServer(log, eventChannel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
		if err != nil {
			log.Fatal("Failed to listen on gRPC port", zap.Error(err))
		}

		s := grpc.NewServer()
		pb.RegisterEventingServer(s, grpcSrv)

		log.Info("gRPC server listening", zap.Int("port", cfg.GRPCPort))
		if err := s.Serve(lis); err != nil {
			log.Error("gRPC server failed", zap.Error(err))
		}
	}()

	// Start health check server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpserver.StartHealthServer(ctx, cfg.HealthCheckPort, log, cfg.Version, startTime); err != nil {
			log.Error("Health server failed", zap.Error(err))
		}
	}()

	// TODO: Start forwarder service if configured
	if len(cfg.ForwardTo) > 0 {
		log.Info("Event forwarding configured", zap.Strings("targets", cfg.ForwardTo))
		// Forwarder implementation will be added later
	}

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		log.Info("Received shutdown signal", zap.String("signal", sig.String()))
	case <-ctx.Done():
		log.Info("Context cancelled")
	}

	log.Info("Initiating graceful shutdown...")
	cancel()

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info("All servers shut down gracefully")
	case <-time.After(30 * time.Second):
		log.Warn("Shutdown timeout exceeded, forcing exit")
	}

	log.Info("TAS MCP Server stopped")
}