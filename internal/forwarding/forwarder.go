package forwarding

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
	"github.com/tributary-ai-services/tas-mcp/internal/config"
)

// EventForwarder handles forwarding events to multiple targets
type EventForwarder struct {
	logger    *zap.Logger
	config    *config.ForwardingConfig
	targets   map[string]*ForwardingTarget
	mu        sync.RWMutex
	metrics   *ForwardingMetrics
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// ForwardingTarget represents a destination for event forwarding
type ForwardingTarget struct {
	ID          string                    `json:"id"`
	Name        string                    `json:"name"`
	Type        ForwardingType            `json:"type"`
	Endpoint    string                    `json:"endpoint"`
	Config      *TargetConfig             `json:"config"`
	Status      TargetStatus              `json:"status"`
	Client      interface{}               `json:"-"` // gRPC client or HTTP client
	LastHealthy time.Time                 `json:"last_healthy"`
	LastError   string                    `json:"last_error,omitempty"`
	ErrorCount  int                       `json:"error_count"`
	Rules       []*ForwardingRule         `json:"rules"`
	Metrics     *TargetMetrics            `json:"metrics"`
}

// ForwardingType defines the type of forwarding target
type ForwardingType string

const (
	ForwardingTypeGRPC     ForwardingType = "grpc"
	ForwardingTypeHTTP     ForwardingType = "http"
	ForwardingTypeKafka    ForwardingType = "kafka"
	ForwardingTypeWebhook  ForwardingType = "webhook"
	ForwardingTypeArgoEvents ForwardingType = "argo-events"
)

// TargetStatus represents the health status of a target
type TargetStatus string

const (
	TargetStatusHealthy   TargetStatus = "healthy"
	TargetStatusUnhealthy TargetStatus = "unhealthy"
	TargetStatusDisabled  TargetStatus = "disabled"
	TargetStatusUnknown   TargetStatus = "unknown"
)

// TargetConfig holds configuration for a forwarding target
type TargetConfig struct {
	Timeout         time.Duration     `json:"timeout"`
	RetryAttempts   int               `json:"retry_attempts"`
	RetryDelay      time.Duration     `json:"retry_delay"`
	HealthCheckURL  string            `json:"health_check_url,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Authentication  *AuthConfig       `json:"authentication,omitempty"`
	BatchSize       int               `json:"batch_size,omitempty"`
	BatchTimeout    time.Duration     `json:"batch_timeout,omitempty"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Type   string            `json:"type"` // bearer, basic, api-key
	Token  string            `json:"token,omitempty"`
	Header string            `json:"header,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// ForwardingRule defines conditions for event forwarding
type ForwardingRule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Enabled     bool                   `json:"enabled"`
	Priority    int                    `json:"priority"`
	Conditions  []*RuleCondition       `json:"conditions"`
	Transform   *EventTransform        `json:"transform,omitempty"`
	RateLimit   *RateLimit             `json:"rate_limit,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
}

// RuleCondition defines a condition for rule evaluation
type RuleCondition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"` // eq, ne, gt, lt, contains, regex, in
	Value    interface{} `json:"value"`
	Negate   bool        `json:"negate,omitempty"`
}

// EventTransform defines how to transform events before forwarding
type EventTransform struct {
	AddFields    map[string]interface{} `json:"add_fields,omitempty"`
	RemoveFields []string               `json:"remove_fields,omitempty"`
	RenameFields map[string]string      `json:"rename_fields,omitempty"`
	Template     string                 `json:"template,omitempty"`
}

// RateLimit defines rate limiting for forwarding
type RateLimit struct {
	RequestsPerSecond int           `json:"requests_per_second"`
	BurstSize         int           `json:"burst_size"`
	Window            time.Duration `json:"window"`
}

// ForwardingMetrics tracks forwarding statistics
type ForwardingMetrics struct {
	TotalEvents     int64             `json:"total_events"`
	ForwardedEvents int64             `json:"forwarded_events"`
	FailedEvents    int64             `json:"failed_events"`
	DroppedEvents   int64             `json:"dropped_events"`
	TargetMetrics   map[string]*TargetMetrics `json:"target_metrics"`
	LastUpdated     time.Time         `json:"last_updated"`
	mu              sync.RWMutex
}

// TargetMetrics tracks metrics for a specific target
type TargetMetrics struct {
	EventsSent      int64         `json:"events_sent"`
	EventsFailed    int64         `json:"events_failed"`
	ResponseTimes   []time.Duration `json:"-"` // Not serialized, used for calculations
	AvgResponseTime time.Duration `json:"avg_response_time"`
	LastError       string        `json:"last_error,omitempty"`
	LastSuccess     time.Time     `json:"last_success"`
	Uptime          float64       `json:"uptime_percentage"`
}

// EventToForward represents an event ready for forwarding
type EventToForward struct {
	Event     *mcpv1.Event          `json:"event"`
	Targets   []string              `json:"targets"`
	Rules     []string              `json:"rules"`
	Metadata  map[string]string     `json:"metadata"`
	Timestamp time.Time             `json:"timestamp"`
	Attempts  int                   `json:"attempts"`
	MaxAttempts int                 `json:"max_attempts"`
}

// NewEventForwarder creates a new event forwarder
func NewEventForwarder(logger *zap.Logger, config *config.ForwardingConfig) *EventForwarder {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &EventForwarder{
		logger:  logger,
		config:  config,
		targets: make(map[string]*ForwardingTarget),
		metrics: &ForwardingMetrics{
			TargetMetrics: make(map[string]*TargetMetrics),
			LastUpdated:   time.Now(),
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start initializes the forwarder and starts background processes
func (f *EventForwarder) Start() error {
	f.logger.Info("Starting event forwarder")

	// Load initial targets from configuration
	if err := f.loadTargets(); err != nil {
		return fmt.Errorf("failed to load targets: %w", err)
	}

	// Start health check routine
	f.wg.Add(1)
	go f.healthCheckRoutine()

	// Start metrics collection routine
	f.wg.Add(1)
	go f.metricsRoutine()

	f.logger.Info("Event forwarder started successfully", 
		zap.Int("targets", len(f.targets)))
	
	return nil
}

// Stop gracefully shuts down the forwarder
func (f *EventForwarder) Stop() {
	f.logger.Info("Stopping event forwarder")
	f.cancel()
	f.wg.Wait()
	
	// Close connections to targets
	f.mu.Lock()
	defer f.mu.Unlock()
	
	for _, target := range f.targets {
		f.closeTargetConnection(target)
	}
	
	f.logger.Info("Event forwarder stopped")
}

// ForwardEvent forwards an event to matching targets
func (f *EventForwarder) ForwardEvent(ctx context.Context, event *mcpv1.Event) error {
	f.metrics.mu.Lock()
	f.metrics.TotalEvents++
	f.metrics.mu.Unlock()

	// Find matching targets
	matchingTargets := f.findMatchingTargets(event)
	if len(matchingTargets) == 0 {
		f.logger.Debug("No matching targets for event",
			zap.String("event_id", event.EventId),
			zap.String("event_type", event.EventType))
		return nil
	}

	f.logger.Info("Forwarding event",
		zap.String("event_id", event.EventId),
		zap.String("event_type", event.EventType),
		zap.Int("targets", len(matchingTargets)))

	// Create forwarding context
	eventToForward := &EventToForward{
		Event:       event,
		Targets:     make([]string, len(matchingTargets)),
		Timestamp:   time.Now(),
		MaxAttempts: f.config.DefaultRetryAttempts,
	}

	for i, target := range matchingTargets {
		eventToForward.Targets[i] = target.ID
	}

	// Forward to all targets (parallel execution)
	return f.forwardToTargets(ctx, eventToForward, matchingTargets)
}

// AddTarget adds a new forwarding target
func (f *EventForwarder) AddTarget(target *ForwardingTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check for duplicate ID
	if _, exists := f.targets[target.ID]; exists {
		return fmt.Errorf("target with ID %s already exists", target.ID)
	}

	// Validate target configuration
	if err := f.validateTarget(target); err != nil {
		return fmt.Errorf("invalid target configuration: %w", err)
	}

	// Initialize target connection
	if err := f.initializeTarget(target); err != nil {
		return fmt.Errorf("failed to initialize target: %w", err)
	}

	// Add target
	f.targets[target.ID] = target
	f.metrics.TargetMetrics[target.ID] = &TargetMetrics{
		LastSuccess: time.Now(),
		Uptime:      100.0,
	}

	f.logger.Info("Added forwarding target",
		zap.String("target_id", target.ID),
		zap.String("target_name", target.Name),
		zap.String("type", string(target.Type)),
		zap.String("endpoint", target.Endpoint))

	return nil
}

// RemoveTarget removes a forwarding target
func (f *EventForwarder) RemoveTarget(targetID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	target, exists := f.targets[targetID]
	if !exists {
		return fmt.Errorf("target not found: %s", targetID)
	}

	// Close target connection
	f.closeTargetConnection(target)

	// Remove target
	delete(f.targets, targetID)
	delete(f.metrics.TargetMetrics, targetID)

	f.logger.Info("Removed forwarding target", zap.String("target_id", targetID))
	return nil
}

// GetTargets returns all configured targets
func (f *EventForwarder) GetTargets() map[string]*ForwardingTarget {
	f.mu.RLock()
	defer f.mu.RUnlock()

	targets := make(map[string]*ForwardingTarget)
	for id, target := range f.targets {
		targets[id] = target
	}
	return targets
}

// GetMetrics returns forwarding metrics
func (f *EventForwarder) GetMetrics() *ForwardingMetrics {
	f.metrics.mu.RLock()
	defer f.metrics.mu.RUnlock()
	
	// Create a copy to avoid race conditions
	metrics := &ForwardingMetrics{
		TotalEvents:     f.metrics.TotalEvents,
		ForwardedEvents: f.metrics.ForwardedEvents,
		FailedEvents:    f.metrics.FailedEvents,
		DroppedEvents:   f.metrics.DroppedEvents,
		LastUpdated:     f.metrics.LastUpdated,
		TargetMetrics:   make(map[string]*TargetMetrics),
	}

	for id, targetMetrics := range f.metrics.TargetMetrics {
		metrics.TargetMetrics[id] = &TargetMetrics{
			EventsSent:      targetMetrics.EventsSent,
			EventsFailed:    targetMetrics.EventsFailed,
			AvgResponseTime: targetMetrics.AvgResponseTime,
			LastError:       targetMetrics.LastError,
			LastSuccess:     targetMetrics.LastSuccess,
			Uptime:          targetMetrics.Uptime,
		}
	}

	return metrics
}

// loadTargets loads initial targets from configuration
func (f *EventForwarder) loadTargets() error {
	for _, targetConfig := range f.config.Targets {
		// Convert config types to internal types
		var rules []*ForwardingRule
		for _, rule := range targetConfig.Rules {
			var conditions []*RuleCondition
			for _, cond := range rule.Conditions {
				conditions = append(conditions, &RuleCondition{
					Field:    cond.Field,
					Operator: cond.Operator,
					Value:    cond.Value,
					Negate:   cond.Negate,
				})
			}
			
			rules = append(rules, &ForwardingRule{
				ID:         rule.ID,
				Name:       rule.Name,
				Enabled:    rule.Enabled,
				Priority:   rule.Priority,
				Conditions: conditions,
			})
		}

		var targetConfigInternal *TargetConfig
		if targetConfig.Config != nil {
			targetConfigInternal = &TargetConfig{
				Timeout:       targetConfig.Config.Timeout,
				RetryAttempts: targetConfig.Config.RetryAttempts,
				RetryDelay:    targetConfig.Config.RetryDelay,
				Headers:       targetConfig.Config.Headers,
			}
		}

		target := &ForwardingTarget{
			ID:       targetConfig.ID,
			Name:     targetConfig.Name,
			Type:     ForwardingType(targetConfig.Type),
			Endpoint: targetConfig.Endpoint,
			Config:   targetConfigInternal,
			Status:   TargetStatusUnknown,
			Rules:    rules,
			Metrics:  &TargetMetrics{},
		}

		if err := f.AddTarget(target); err != nil {
			f.logger.Error("Failed to load target",
				zap.String("target_id", target.ID),
				zap.Error(err))
			continue
		}
	}

	return nil
}

// findMatchingTargets finds targets that match the event
func (f *EventForwarder) findMatchingTargets(event *mcpv1.Event) []*ForwardingTarget {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var matchingTargets []*ForwardingTarget

	for _, target := range f.targets {
		if target.Status == TargetStatusDisabled {
			continue
		}

		if f.evaluateTargetRules(target, event) {
			matchingTargets = append(matchingTargets, target)
		}
	}

	return matchingTargets
}

// evaluateTargetRules evaluates if an event matches target rules
func (f *EventForwarder) evaluateTargetRules(target *ForwardingTarget, event *mcpv1.Event) bool {
	// If no rules defined, forward all events
	if len(target.Rules) == 0 {
		return true
	}

	// Evaluate each rule (OR logic between rules)
	for _, rule := range target.Rules {
		if !rule.Enabled {
			continue
		}

		if f.evaluateRule(rule, event) {
			return true
		}
	}

	return false
}

// evaluateRule evaluates a single forwarding rule
func (f *EventForwarder) evaluateRule(rule *ForwardingRule, event *mcpv1.Event) bool {
	// All conditions must match (AND logic)
	for _, condition := range rule.Conditions {
		if !f.evaluateCondition(condition, event) {
			return false
		}
	}
	return true
}

// evaluateCondition evaluates a single condition
func (f *EventForwarder) evaluateCondition(condition *RuleCondition, event *mcpv1.Event) bool {
	value := f.extractFieldValue(condition.Field, event)
	result := f.compareValues(value, condition.Operator, condition.Value)
	
	if condition.Negate {
		result = !result
	}
	
	return result
}

// extractFieldValue extracts a field value from an event
func (f *EventForwarder) extractFieldValue(field string, event *mcpv1.Event) interface{} {
	switch field {
	case "event_id":
		return event.EventId
	case "event_type":
		return event.EventType
	case "source":
		return event.Source
	case "timestamp":
		return event.Timestamp
	default:
		// Handle nested data fields
		if len(field) > 5 && field[:5] == "data." {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(event.Data), &data); err == nil {
				fieldName := field[5:]
				return data[fieldName]
			}
		}
		// Handle metadata fields
		if len(field) > 9 && field[:9] == "metadata." {
			fieldName := field[9:]
			return event.Metadata[fieldName]
		}
	}
	return nil
}

// compareValues compares two values using the specified operator
func (f *EventForwarder) compareValues(actual interface{}, operator string, expected interface{}) bool {
	switch operator {
	case "eq":
		return actual == expected
	case "ne":
		return actual != expected
	case "gt":
		return f.compareNumeric(actual, expected, func(a, b float64) bool { return a > b })
	case "lt":
		return f.compareNumeric(actual, expected, func(a, b float64) bool { return a < b })
	case "gte":
		return f.compareNumeric(actual, expected, func(a, b float64) bool { return a >= b })
	case "lte":
		return f.compareNumeric(actual, expected, func(a, b float64) bool { return a <= b })
	case "contains":
		if actualStr, ok := actual.(string); ok {
			if expectedStr, ok := expected.(string); ok {
				return len(expectedStr) > 0 && strings.Contains(actualStr, expectedStr)
			}
		}
		return false
	case "in":
		if expectedSlice, ok := expected.([]interface{}); ok {
			for _, item := range expectedSlice {
				if actual == item {
					return true
				}
			}
		}
		return false
	default:
		f.logger.Warn("Unknown operator", zap.String("operator", operator))
		return false
	}
}

// compareNumeric compares numeric values
func (f *EventForwarder) compareNumeric(a, b interface{}, compareFn func(float64, float64) bool) bool {
	aFloat, aOk := f.toFloat64(a)
	bFloat, bOk := f.toFloat64(b)
	
	if !aOk || !bOk {
		return false
	}
	
	return compareFn(aFloat, bFloat)
}

// toFloat64 converts interface{} to float64
func (f *EventForwarder) toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

// forwardToTargets forwards an event to multiple targets
func (f *EventForwarder) forwardToTargets(ctx context.Context, eventToForward *EventToForward, targets []*ForwardingTarget) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(targets))

	for _, target := range targets {
		wg.Add(1)
		go func(t *ForwardingTarget) {
			defer wg.Done()
			if err := f.forwardToTarget(ctx, eventToForward.Event, t); err != nil {
				errChan <- fmt.Errorf("target %s: %w", t.ID, err)
			}
		}(target)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	// Update metrics
	f.metrics.mu.Lock()
	if len(errors) == 0 {
		f.metrics.ForwardedEvents++
	} else {
		f.metrics.FailedEvents++
	}
	f.metrics.mu.Unlock()

	if len(errors) > 0 {
		return fmt.Errorf("forwarding failed for %d targets: %v", len(errors), errors)
	}

	return nil
}

// forwardToTarget forwards an event to a specific target
func (f *EventForwarder) forwardToTarget(ctx context.Context, event *mcpv1.Event, target *ForwardingTarget) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		f.updateTargetMetrics(target.ID, duration, nil)
	}()

	// Apply timeout from target config
	if target.Config != nil && target.Config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, target.Config.Timeout)
		defer cancel()
	}

	switch target.Type {
	case ForwardingTypeGRPC:
		return f.forwardGRPC(ctx, event, target)
	case ForwardingTypeHTTP, ForwardingTypeWebhook:
		return f.forwardHTTP(ctx, event, target)
	case ForwardingTypeKafka:
		return f.forwardKafka(ctx, event, target)
	case ForwardingTypeArgoEvents:
		return f.forwardArgoEvents(ctx, event, target)
	default:
		return fmt.Errorf("unsupported target type: %s", target.Type)
	}
}

// forwardGRPC forwards an event via gRPC
func (f *EventForwarder) forwardGRPC(ctx context.Context, event *mcpv1.Event, target *ForwardingTarget) error {
	client, ok := target.Client.(mcpv1.MCPServiceClient)
	if !ok {
		return fmt.Errorf("invalid gRPC client for target %s", target.ID)
	}

	req := &mcpv1.IngestEventRequest{
		EventId:   event.EventId,
		EventType: event.EventType,
		Source:    event.Source,
		Timestamp: event.Timestamp,
		Data:      event.Data,
		Metadata:  event.Metadata,
	}

	resp, err := client.IngestEvent(ctx, req)
	if err != nil {
		return fmt.Errorf("gRPC forward failed: %w", err)
	}

	f.logger.Debug("Event forwarded via gRPC",
		zap.String("target_id", target.ID),
		zap.String("event_id", event.EventId),
		zap.String("response_event_id", resp.EventId))

	return nil
}

// forwardHTTP forwards an event via HTTP
func (f *EventForwarder) forwardHTTP(ctx context.Context, event *mcpv1.Event, target *ForwardingTarget) error {
	// Implementation would use HTTP client to POST event
	f.logger.Debug("Event would be forwarded via HTTP",
		zap.String("target_id", target.ID),
		zap.String("endpoint", target.Endpoint),
		zap.String("event_id", event.EventId))
	return nil
}

// forwardKafka forwards an event to Kafka
func (f *EventForwarder) forwardKafka(ctx context.Context, event *mcpv1.Event, target *ForwardingTarget) error {
	// Implementation would use Kafka producer
	f.logger.Debug("Event would be forwarded to Kafka",
		zap.String("target_id", target.ID),
		zap.String("topic", target.Endpoint),
		zap.String("event_id", event.EventId))
	return nil
}

// forwardArgoEvents forwards an event to Argo Events
func (f *EventForwarder) forwardArgoEvents(ctx context.Context, event *mcpv1.Event, target *ForwardingTarget) error {
	// Implementation would send to Argo Events webhook
	f.logger.Debug("Event would be forwarded to Argo Events",
		zap.String("target_id", target.ID),
		zap.String("endpoint", target.Endpoint),
		zap.String("event_id", event.EventId))
	return nil
}

// validateTarget validates a target configuration
func (f *EventForwarder) validateTarget(target *ForwardingTarget) error {
	if target.ID == "" {
		return fmt.Errorf("target ID is required")
	}
	if target.Name == "" {
		return fmt.Errorf("target name is required")
	}
	if target.Endpoint == "" {
		return fmt.Errorf("target endpoint is required")
	}
	return nil
}

// initializeTarget initializes a target's connection
func (f *EventForwarder) initializeTarget(target *ForwardingTarget) error {
	switch target.Type {
	case ForwardingTypeGRPC:
		return f.initializeGRPCTarget(target)
	case ForwardingTypeHTTP, ForwardingTypeWebhook:
		return f.initializeHTTPTarget(target)
	case ForwardingTypeKafka:
		return f.initializeKafkaTarget(target)
	case ForwardingTypeArgoEvents:
		return f.initializeArgoEventsTarget(target)
	default:
		return fmt.Errorf("unsupported target type: %s", target.Type)
	}
}

// initializeGRPCTarget initializes a gRPC target
func (f *EventForwarder) initializeGRPCTarget(target *ForwardingTarget) error {
	conn, err := grpc.Dial(target.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
	}

	client := mcpv1.NewMCPServiceClient(conn)
	target.Client = client
	target.Status = TargetStatusHealthy
	target.LastHealthy = time.Now()

	return nil
}

// initializeHTTPTarget initializes an HTTP target
func (f *EventForwarder) initializeHTTPTarget(target *ForwardingTarget) error {
	// HTTP client initialization would go here
	target.Status = TargetStatusHealthy
	target.LastHealthy = time.Now()
	return nil
}

// initializeKafkaTarget initializes a Kafka target
func (f *EventForwarder) initializeKafkaTarget(target *ForwardingTarget) error {
	// Kafka producer initialization would go here
	target.Status = TargetStatusHealthy
	target.LastHealthy = time.Now()
	return nil
}

// initializeArgoEventsTarget initializes an Argo Events target
func (f *EventForwarder) initializeArgoEventsTarget(target *ForwardingTarget) error {
	// Argo Events webhook client initialization would go here
	target.Status = TargetStatusHealthy
	target.LastHealthy = time.Now()
	return nil
}

// closeTargetConnection closes a target's connection
func (f *EventForwarder) closeTargetConnection(target *ForwardingTarget) {
	switch target.Type {
	case ForwardingTypeGRPC:
		if conn, ok := target.Client.(*grpc.ClientConn); ok {
			conn.Close()
		}
	}
}

// updateTargetMetrics updates metrics for a target
func (f *EventForwarder) updateTargetMetrics(targetID string, duration time.Duration, err error) {
	f.metrics.mu.Lock()
	defer f.metrics.mu.Unlock()

	metrics, exists := f.metrics.TargetMetrics[targetID]
	if !exists {
		metrics = &TargetMetrics{}
		f.metrics.TargetMetrics[targetID] = metrics
	}

	if err == nil {
		metrics.EventsSent++
		metrics.LastSuccess = time.Now()
		metrics.ResponseTimes = append(metrics.ResponseTimes, duration)
		
		// Calculate average response time (keep last 100 measurements)
		if len(metrics.ResponseTimes) > 100 {
			metrics.ResponseTimes = metrics.ResponseTimes[1:]
		}
		
		var total time.Duration
		for _, rt := range metrics.ResponseTimes {
			total += rt
		}
		metrics.AvgResponseTime = total / time.Duration(len(metrics.ResponseTimes))
	} else {
		metrics.EventsFailed++
		metrics.LastError = err.Error()
	}

	f.metrics.LastUpdated = time.Now()
}

// healthCheckRoutine performs periodic health checks on targets
func (f *EventForwarder) healthCheckRoutine() {
	defer f.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-f.ctx.Done():
			return
		case <-ticker.C:
			f.performHealthChecks()
		}
	}
}

// performHealthChecks checks the health of all targets
func (f *EventForwarder) performHealthChecks() {
	f.mu.RLock()
	targets := make([]*ForwardingTarget, 0, len(f.targets))
	for _, target := range f.targets {
		targets = append(targets, target)
	}
	f.mu.RUnlock()

	for _, target := range targets {
		f.checkTargetHealth(target)
	}
}

// checkTargetHealth checks the health of a specific target
func (f *EventForwarder) checkTargetHealth(target *ForwardingTarget) {
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()

	healthy := false
	switch target.Type {
	case ForwardingTypeGRPC:
		healthy = f.checkGRPCHealth(ctx, target)
	case ForwardingTypeHTTP, ForwardingTypeWebhook:
		healthy = f.checkHTTPHealth(ctx, target)
	default:
		healthy = true // Assume healthy for unsupported types
	}

	f.mu.Lock()
	if healthy {
		target.Status = TargetStatusHealthy
		target.LastHealthy = time.Now()
		target.ErrorCount = 0
	} else {
		target.ErrorCount++
		if target.ErrorCount >= 3 {
			target.Status = TargetStatusUnhealthy
		}
	}
	f.mu.Unlock()
}

// checkGRPCHealth checks gRPC target health
func (f *EventForwarder) checkGRPCHealth(ctx context.Context, target *ForwardingTarget) bool {
	client, ok := target.Client.(mcpv1.MCPServiceClient)
	if !ok {
		return false
	}

	_, err := client.GetHealth(ctx, &mcpv1.HealthCheckRequest{})
	return err == nil
}

// checkHTTPHealth checks HTTP target health
func (f *EventForwarder) checkHTTPHealth(ctx context.Context, target *ForwardingTarget) bool {
	// HTTP health check implementation would go here
	return true
}

// metricsRoutine periodically updates metrics
func (f *EventForwarder) metricsRoutine() {
	defer f.wg.Done()
	
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-f.ctx.Done():
			return
		case <-ticker.C:
			f.updateMetrics()
		}
	}
}

// updateMetrics updates aggregated metrics
func (f *EventForwarder) updateMetrics() {
	f.metrics.mu.Lock()
	defer f.metrics.mu.Unlock()

	// Update uptime percentages for targets
	for _, metrics := range f.metrics.TargetMetrics {
		total := metrics.EventsSent + metrics.EventsFailed
		if total > 0 {
			metrics.Uptime = float64(metrics.EventsSent) / float64(total) * 100
		}
	}

	f.metrics.LastUpdated = time.Now()
}
// EvaluateCondition is a public method to evaluate a condition (for testing)
func (f *EventForwarder) EvaluateCondition(event *mcpv1.Event, condition *config.RuleCondition) bool {
	ruleCondition := &RuleCondition{
		Field:    condition.Field,
		Operator: condition.Operator,
		Value:    condition.Value,
		Negate:   condition.Negate,
	}
	return f.evaluateCondition(ruleCondition, event)
}
