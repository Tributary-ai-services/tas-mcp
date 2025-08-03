// Package grpc provides gRPC server functionality for the TAS MCP server.
package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/forwarding"
)

// Constants for the gRPC server
const (
	// EventChannelSize is the size of the event channel buffer
	EventChannelSize = 1000
	// RandomIDModulo is used for generating random IDs
	RandomIDModulo = 100000
	// HealthyStatus indicates healthy service status
	HealthyStatus = "healthy"
	// AcceptedStatus indicates accepted event status
	AcceptedStatus = "accepted"
)

// MCPServer implements the MCP gRPC service
type MCPServer struct {
	mcpv1.UnimplementedMCPServiceServer
	log          *zap.Logger
	forwarder    *forwarding.EventForwarder
	eventChannel chan *mcpv1.Event
	streams      map[string]mcpv1.MCPService_EventStreamServer
	streamsMux   sync.RWMutex
	stats        *ServerStats
}

// ServerStats tracks server statistics
type ServerStats struct {
	TotalEvents     int64     `json:"total_events"`
	StreamEvents    int64     `json:"stream_events"`
	ForwardedEvents int64     `json:"forwarded_events"`
	ErrorEvents     int64     `json:"error_events"`
	ActiveStreams   int       `json:"active_streams"`
	StartTime       time.Time `json:"start_time"`
	mu              sync.RWMutex
}

// NewMCPServer creates a new MCPServer instance
func NewMCPServer(log *zap.Logger, forwarder *forwarding.EventForwarder) *MCPServer {
	return &MCPServer{
		log:          log,
		forwarder:    forwarder,
		eventChannel: make(chan *mcpv1.Event, EventChannelSize),
		streams:      make(map[string]mcpv1.MCPService_EventStreamServer),
		stats: &ServerStats{
			StartTime: time.Now(),
		},
	}
}

// IngestEvent handles single event ingestion
func (s *MCPServer) IngestEvent(ctx context.Context, req *mcpv1.IngestEventRequest) (*mcpv1.IngestEventResponse, error) {
	if req == nil {
		s.stats.mu.Lock()
		s.stats.ErrorEvents++
		s.stats.mu.Unlock()
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	s.log.Debug("Received event ingestion request",
		zap.String("event_id", req.EventId),
		zap.String("event_type", req.EventType),
		zap.String("source", req.Source))

	// Update stats
	s.stats.mu.Lock()
	s.stats.TotalEvents++
	s.stats.mu.Unlock()

	// Validate request
	if err := s.validateIngestRequest(req); err != nil {
		s.stats.mu.Lock()
		s.stats.ErrorEvents++
		s.stats.mu.Unlock()
		return nil, err
	}

	// Create event
	event := &mcpv1.Event{
		EventId:   req.EventId,
		EventType: req.EventType,
		Source:    req.Source,
		Timestamp: req.Timestamp,
		Data:      req.Data,
		Metadata:  req.Metadata,
	}

	// Forward event if forwarder is available
	if s.forwarder != nil {
		if err := s.forwarder.ForwardEvent(ctx, event); err != nil {
			s.log.Error("Failed to forward event",
				zap.String("event_id", event.EventId),
				zap.Error(err))
		} else {
			s.stats.mu.Lock()
			s.stats.ForwardedEvents++
			s.stats.mu.Unlock()
		}
	}

	// Broadcast to connected streams
	s.broadcastEvent(event)

	return &mcpv1.IngestEventResponse{
		EventId: event.EventId,
		Status:  AcceptedStatus,
	}, nil
}

// EventStream handles bidirectional streaming of MCP events
func (s *MCPServer) EventStream(stream mcpv1.MCPService_EventStreamServer) error {
	streamID := generateStreamID()
	s.log.Info("Client connected to event stream", zap.String("stream_id", streamID))

	// Register stream for outbound events
	s.registerStream(streamID, stream)
	defer s.unregisterStream(streamID)

	// Handle incoming events from client
	go s.handleIncomingEvents(stream, streamID)

	// Keep connection alive and handle outbound events
	for {
		select {
		case <-stream.Context().Done():
			s.log.Info("Client disconnected from event stream", zap.String("stream_id", streamID))
			return nil
		case event := <-s.eventChannel:
			// Broadcast to all connected streams
			s.broadcastEvent(event)
		}
	}
}

// GetHealth returns server health status
func (s *MCPServer) GetHealth(_ context.Context, _ *mcpv1.HealthCheckRequest) (*mcpv1.HealthCheckResponse, error) {
	s.stats.mu.RLock()
	uptime := time.Since(s.stats.StartTime)
	activeStreams := s.stats.ActiveStreams
	if activeStreams > math.MaxInt32 {
		activeStreams = math.MaxInt32
	}
	// #nosec G115 -- bounds checked above
	activeStreamsInt32 := int32(activeStreams)
	s.stats.mu.RUnlock()

	status := HealthyStatus

	return &mcpv1.HealthCheckResponse{
		Status: status,
		Uptime: uptime.Milliseconds(),
		Details: map[string]string{
			"active_streams": fmt.Sprintf("%d", activeStreamsInt32),
			"version":        "1.0.0",
		},
	}, nil
}

// GetMetrics returns server metrics
func (s *MCPServer) GetMetrics(_ context.Context, _ *mcpv1.MetricsRequest) (*mcpv1.MetricsResponse, error) {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	activeStreams := s.stats.ActiveStreams
	if activeStreams > math.MaxInt32 {
		activeStreams = math.MaxInt32
	}

	return &mcpv1.MetricsResponse{
		TotalEvents:     s.stats.TotalEvents,
		StreamEvents:    s.stats.StreamEvents,
		ForwardedEvents: s.stats.ForwardedEvents,
		ErrorEvents:     s.stats.ErrorEvents,
		// #nosec G115 -- bounds checked above
		ActiveStreams: int32(activeStreams),
		Uptime:        time.Since(s.stats.StartTime).Milliseconds(),
	}, nil
}

// validateEventData validates common event fields and data
func validateEventData(eventID, eventType, source, data string) error {
	if eventID == "" {
		return status.Error(codes.InvalidArgument, "event ID cannot be empty")
	}
	if eventType == "" {
		return status.Error(codes.InvalidArgument, "event type cannot be empty")
	}
	if source == "" {
		return status.Error(codes.InvalidArgument, "event source cannot be empty")
	}
	if data == "" {
		return status.Error(codes.InvalidArgument, "event data cannot be empty")
	}

	// Validate that data is valid JSON
	var js json.RawMessage
	if err := json.Unmarshal([]byte(data), &js); err != nil {
		return status.Error(codes.InvalidArgument, "event data must be valid JSON")
	}

	return nil
}

// validateIngestRequest validates an ingestion request
func (s *MCPServer) validateIngestRequest(req *mcpv1.IngestEventRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request cannot be nil")
	}
	return validateEventData(req.EventId, req.EventType, req.Source, req.Data)
}

// handleIncomingEvents processes events received from the client
func (s *MCPServer) handleIncomingEvents(stream mcpv1.MCPService_EventStreamServer, streamID string) {
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			s.log.Info("Client closed stream", zap.String("stream_id", streamID))
			return
		}
		if err != nil {
			s.log.Error("Error receiving event from client",
				zap.String("stream_id", streamID),
				zap.Error(err))
			return
		}

		s.log.Debug("Received event from client",
			zap.String("stream_id", streamID),
			zap.String("event_id", event.EventId))

		// Update stats
		s.stats.mu.Lock()
		s.stats.StreamEvents++
		s.stats.TotalEvents++
		s.stats.mu.Unlock()

		// Validate event
		if err := s.validateEvent(event); err != nil {
			s.log.Warn("Invalid event received",
				zap.String("stream_id", streamID),
				zap.String("event_id", event.EventId),
				zap.Error(err))

			s.stats.mu.Lock()
			s.stats.ErrorEvents++
			s.stats.mu.Unlock()
			continue
		}

		// Forward event if forwarder is available
		if s.forwarder != nil {
			if err := s.forwarder.ForwardEvent(stream.Context(), event); err != nil {
				s.log.Error("Failed to forward stream event",
					zap.String("event_id", event.EventId),
					zap.Error(err))
			} else {
				s.stats.mu.Lock()
				s.stats.ForwardedEvents++
				s.stats.mu.Unlock()
			}
		}

		// Forward event to internal processing
		select {
		case s.eventChannel <- event:
			s.log.Debug("Event forwarded to internal channel",
				zap.String("event_id", event.EventId))
		default:
			s.log.Warn("Event channel full, dropping event",
				zap.String("event_id", event.EventId))
		}
	}
}

// registerStream adds a stream to the active streams map
func (s *MCPServer) registerStream(streamID string, stream mcpv1.MCPService_EventStreamServer) {
	s.streamsMux.Lock()
	defer s.streamsMux.Unlock()
	s.streams[streamID] = stream

	s.stats.mu.Lock()
	s.stats.ActiveStreams = len(s.streams)
	s.stats.mu.Unlock()
}

// unregisterStream removes a stream from the active streams map
func (s *MCPServer) unregisterStream(streamID string) {
	s.streamsMux.Lock()
	defer s.streamsMux.Unlock()
	delete(s.streams, streamID)

	s.stats.mu.Lock()
	s.stats.ActiveStreams = len(s.streams)
	s.stats.mu.Unlock()
}

// broadcastEvent sends an event to all connected streams
func (s *MCPServer) broadcastEvent(event *mcpv1.Event) {
	s.streamsMux.RLock()
	defer s.streamsMux.RUnlock()

	for streamID, stream := range s.streams {
		if err := stream.Send(event); err != nil {
			s.log.Error("Failed to send event to stream",
				zap.String("stream_id", streamID),
				zap.String("event_id", event.EventId),
				zap.Error(err))
			// Remove failed stream (will be cleaned up on next broadcast)
		} else {
			s.log.Debug("Event sent to stream",
				zap.String("stream_id", streamID),
				zap.String("event_id", event.EventId))
		}
	}
}

// validateEvent performs basic validation on received events
func (s *MCPServer) validateEvent(event *mcpv1.Event) error {
	if event == nil {
		return status.Error(codes.InvalidArgument, "event cannot be nil")
	}
	return validateEventData(event.EventId, event.EventType, event.Source, event.Data)
}

// GetStats returns server statistics
func (s *MCPServer) GetStats() *ServerStats {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	return &ServerStats{
		TotalEvents:     s.stats.TotalEvents,
		StreamEvents:    s.stats.StreamEvents,
		ForwardedEvents: s.stats.ForwardedEvents,
		ErrorEvents:     s.stats.ErrorEvents,
		ActiveStreams:   s.stats.ActiveStreams,
		StartTime:       s.stats.StartTime,
	}
}

// generateStreamID creates a unique identifier for a stream
func generateStreamID() string {
	return fmt.Sprintf("stream_%d_%d", time.Now().UnixNano(), randomInt())
}

// randomInt generates a random integer
func randomInt() int {
	return int(time.Now().UnixNano() % RandomIDModulo)
}
