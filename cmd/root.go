package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
	grpcimpl "github.com/tributary-ai-services/tas-mcp/internal/grpc"
	"github.com/tributary-ai-services/tas-mcp/internal/http"
	"github.com/tributary-ai-services/tas-mcp/internal/logger"
	"google.golang.org/grpc"

	"go.uber.org/zap"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tas-mcp",
		Short: "Start the TAS Model Context Protocol (MCP) server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			log := logger.New(cfg.LogLevel)
			eventCh := make(chan *http.MCPEvent, 100)

			// Context-aware shutdown
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Signal handler
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

			// Start HTTP server
			go func() {
				if err := http.Start(ctx, cfg.MCPPort, eventCh, log); err != nil {
					log.Fatal("failed to start HTTP server", zap.Error(err))
				}
			}()

			// Start gRPC server
			go func() {
				lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
				if err != nil {
					log.Fatal("failed to listen for gRPC", zap.Error(err))
				}

				s := grpc.NewServer()
				pb := grpcimpl.NewServer(log, eventCh)
				grpcimpl.RegisterEventingServer(s, pb)

				log.Info("gRPC server listening", zap.Int("port", cfg.GRPCPort))
				if err := s.Serve(lis); err != nil {
					log.Fatal("failed to start gRPC server", zap.Error(err))
				}
			}()

			// Wait for shutdown
			<-stop
			log.Info("shutting down MCP server")
			cancel()

			// Give goroutines time to exit
			time.Sleep(1 * time.Second)
			return nil
		},
	}

	return cmd
}
