package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	
	// TAS MCP proto definitions
	mcpv1 "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
)

// TriggerHandler handles incoming events and executes triggers
type TriggerHandler struct {
	logger *zap.Logger
	client mcpv1.MCPServiceClient
}

// EventPayload represents the structure of incoming events
type EventPayload struct {
	EventID     string                 `json:"eventId"`
	EventType   string                 `json:"eventType"`
	Source      string                 `json:"source"`
	Timestamp   time.Time              `json:"timestamp"`
	Data        map[string]interface{} `json:"data"`
	Metadata    map[string]string      `json:"metadata"`
}

// TriggerConfig defines trigger conditions and actions
type TriggerConfig struct {
	Name        string            `json:"name"`
	Conditions  []Condition       `json:"conditions"`
	Actions     []Action          `json:"actions"`
	Enabled     bool              `json:"enabled"`
	Metadata    map[string]string `json:"metadata"`
}

// Condition defines when a trigger should fire
type Condition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"` // eq, ne, gt, lt, contains, regex
	Value    interface{} `json:"value"`
}

// Action defines what should happen when trigger fires
type Action struct {
	Type       string                 `json:"type"`       // http, grpc, kafka, email
	Target     string                 `json:"target"`     // URL, topic, address
	Payload    map[string]interface{} `json:"payload"`
	Timeout    time.Duration          `json:"timeout"`
	Retries    int                    `json:"retries"`
}

// NewTriggerHandler creates a new trigger handler
func NewTriggerHandler(logger *zap.Logger, client mcpv1.MCPServiceClient) *TriggerHandler {
	return &TriggerHandler{
		logger: logger,
		client: client,
	}
}

// HandleWebhookTrigger processes webhook events
func (h *TriggerHandler) HandleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	var payload EventPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to decode webhook payload", zap.Error(err))
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	h.logger.Info("Received webhook event",
		zap.String("eventId", payload.EventID),
		zap.String("eventType", payload.EventType),
		zap.String("source", payload.Source),
	)

	// Process trigger based on event type
	switch payload.EventType {
	case "user.created":
		h.handleUserCreatedTrigger(payload)
	case "deployment.succeeded":
		h.handleDeploymentSucceededTrigger(payload)
	case "alert.critical":
		h.handleCriticalAlertTrigger(payload)
	default:
		h.handleGenericTrigger(payload)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}

// handleUserCreatedTrigger processes user creation events
func (h *TriggerHandler) handleUserCreatedTrigger(payload EventPayload) {
	trigger := TriggerConfig{
		Name: "user-welcome-sequence",
		Conditions: []Condition{
			{Field: "eventType", Operator: "eq", Value: "user.created"},
			{Field: "data.email", Operator: "contains", Value: "@"},
		},
		Actions: []Action{
			{
				Type:    "http",
				Target:  "https://api.example.com/welcome-email",
				Payload: map[string]interface{}{
					"userId": payload.Data["userId"],
					"email":  payload.Data["email"],
					"name":   payload.Data["name"],
				},
				Timeout: 30 * time.Second,
				Retries: 3,
			},
			{
				Type:   "grpc",
				Target: "user-service:50051",
				Payload: map[string]interface{}{
					"action": "setup_defaults",
					"userId": payload.Data["userId"],
				},
				Timeout: 10 * time.Second,
				Retries: 2,
			},
		},
		Enabled: true,
	}

	h.executeTrigger(trigger, payload)
}

// handleDeploymentSucceededTrigger processes deployment success events
func (h *TriggerHandler) handleDeploymentSucceededTrigger(payload EventPayload) {
	trigger := TriggerConfig{
		Name: "deployment-notification",
		Conditions: []Condition{
			{Field: "eventType", Operator: "eq", Value: "deployment.succeeded"},
			{Field: "data.environment", Operator: "eq", Value: "production"},
		},
		Actions: []Action{
			{
				Type:   "http",
				Target: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
				Payload: map[string]interface{}{
					"text": "🚀 Production deployment successful!",
					"attachments": []map[string]interface{}{
						{
							"color": "good",
							"fields": []map[string]interface{}{
								{"title": "Service", "value": payload.Data["service"], "short": true},
								{"title": "Version", "value": payload.Data["version"], "short": true},
								{"title": "Environment", "value": payload.Data["environment"], "short": true},
							},
						},
					},
				},
				Timeout: 15 * time.Second,
				Retries: 2,
			},
		},
		Enabled: true,
	}

	h.executeTrigger(trigger, payload)
}

// handleCriticalAlertTrigger processes critical alerts
func (h *TriggerHandler) handleCriticalAlertTrigger(payload EventPayload) {
	trigger := TriggerConfig{
		Name: "critical-alert-escalation",
		Conditions: []Condition{
			{Field: "eventType", Operator: "eq", Value: "alert.critical"},
			{Field: "data.severity", Operator: "gte", Value: 8},
		},
		Actions: []Action{
			{
				Type:   "http",
				Target: "https://api.pagerduty.com/incidents",
				Payload: map[string]interface{}{
					"incident": map[string]interface{}{
						"type":  "incident",
						"title": payload.Data["title"],
						"service": map[string]interface{}{
							"id":   payload.Data["serviceId"],
							"type": "service_reference",
						},
						"urgency": "high",
						"body": map[string]interface{}{
							"type":    "incident_body",
							"details": payload.Data["description"],
						},
					},
				},
				Timeout: 20 * time.Second,
				Retries: 3,
			},
			{
				Type:   "kafka",
				Target: "critical-alerts",
				Payload: map[string]interface{}{
					"alertId":     payload.EventID,
					"timestamp":   payload.Timestamp,
					"severity":    payload.Data["severity"],
					"service":     payload.Data["service"],
					"description": payload.Data["description"],
				},
				Timeout: 5 * time.Second,
				Retries: 2,
			},
		},
		Enabled: true,
	}

	h.executeTrigger(trigger, payload)
}

// handleGenericTrigger processes any other events
func (h *TriggerHandler) handleGenericTrigger(payload EventPayload) {
	h.logger.Info("Processing generic trigger",
		zap.String("eventType", payload.EventType),
		zap.String("source", payload.Source),
	)

	// Forward to TAS MCP service via gRPC
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Convert payload to gRPC message
	eventData, _ := json.Marshal(payload.Data)
	
	req := &mcpv1.IngestEventRequest{
		EventId:   payload.EventID,
		EventType: payload.EventType,
		Source:    payload.Source,
		Timestamp: payload.Timestamp.Unix(),
		Data:      string(eventData),
		Metadata:  payload.Metadata,
	}

	resp, err := h.client.IngestEvent(ctx, req)
	if err != nil {
		h.logger.Error("Failed to forward event to MCP service", zap.Error(err))
		return
	}

	h.logger.Info("Event forwarded successfully",
		zap.String("eventId", payload.EventID),
		zap.String("mcpEventId", resp.EventId),
	)
}

// executeTrigger executes all actions for a trigger
func (h *TriggerHandler) executeTrigger(trigger TriggerConfig, payload EventPayload) {
	if !trigger.Enabled {
		h.logger.Debug("Trigger disabled, skipping", zap.String("trigger", trigger.Name))
		return
	}

	// Check conditions
	if !h.evaluateConditions(trigger.Conditions, payload) {
		h.logger.Debug("Trigger conditions not met", zap.String("trigger", trigger.Name))
		return
	}

	h.logger.Info("Executing trigger", zap.String("trigger", trigger.Name))

	// Execute actions
	for _, action := range trigger.Actions {
		go h.executeAction(action, payload)
	}
}

// evaluateConditions checks if all conditions are met
func (h *TriggerHandler) evaluateConditions(conditions []Condition, payload EventPayload) bool {
	for _, condition := range conditions {
		if !h.evaluateCondition(condition, payload) {
			return false
		}
	}
	return true
}

// evaluateCondition evaluates a single condition
func (h *TriggerHandler) evaluateCondition(condition Condition, payload EventPayload) bool {
	var value interface{}
	
	// Extract value from payload based on field path
	switch condition.Field {
	case "eventType":
		value = payload.EventType
	case "source":
		value = payload.Source
	default:
		// Handle nested data fields (e.g., "data.userId")
		if len(condition.Field) > 5 && condition.Field[:5] == "data." {
			field := condition.Field[5:]
			value = payload.Data[field]
		}
	}

	// Evaluate condition based on operator
	switch condition.Operator {
	case "eq":
		return value == condition.Value
	case "ne":
		return value != condition.Value
	case "contains":
		if str, ok := value.(string); ok {
			if substr, ok := condition.Value.(string); ok {
				return len(str) > 0 && len(substr) > 0 && 
					   len(str) >= len(substr) && 
					   str != substr // avoid self-contains
			}
		}
		return false
	// Add more operators as needed
	default:
		return false
	}
}

// executeAction executes a single action
func (h *TriggerHandler) executeAction(action Action, payload EventPayload) {
	ctx, cancel := context.WithTimeout(context.Background(), action.Timeout)
	defer cancel()

	h.logger.Info("Executing action",
		zap.String("type", action.Type),
		zap.String("target", action.Target),
	)

	switch action.Type {
	case "http":
		h.executeHTTPAction(ctx, action, payload)
	case "grpc":
		h.executeGRPCAction(ctx, action, payload)
	case "kafka":
		h.executeKafkaAction(ctx, action, payload)
	default:
		h.logger.Warn("Unknown action type", zap.String("type", action.Type))
	}
}

// executeHTTPAction sends HTTP request
func (h *TriggerHandler) executeHTTPAction(ctx context.Context, action Action, payload EventPayload) {
	// Implement HTTP action execution with retries
	h.logger.Info("Executing HTTP action", zap.String("target", action.Target))
	// Implementation details...
}

// executeGRPCAction sends gRPC request  
func (h *TriggerHandler) executeGRPCAction(ctx context.Context, action Action, payload EventPayload) {
	// Implement gRPC action execution
	h.logger.Info("Executing gRPC action", zap.String("target", action.Target))
	// Implementation details...
}

// executeKafkaAction publishes to Kafka
func (h *TriggerHandler) executeKafkaAction(ctx context.Context, action Action, payload EventPayload) {
	// Implement Kafka publishing
	h.logger.Info("Executing Kafka action", zap.String("target", action.Target))
	// Implementation details...
}

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Initialize MCP client (placeholder)
	var mcpClient mcpv1.MCPServiceClient

	handler := NewTriggerHandler(logger, mcpClient)

	r := mux.NewRouter()
	r.HandleFunc("/webhook/github", handler.HandleWebhookTrigger).Methods("POST")
	r.HandleFunc("/webhook/generic", handler.HandleWebhookTrigger).Methods("POST")
	r.HandleFunc("/webhook/tas", handler.HandleWebhookTrigger).Methods("POST")

	logger.Info("Starting TAS MCP trigger server on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}