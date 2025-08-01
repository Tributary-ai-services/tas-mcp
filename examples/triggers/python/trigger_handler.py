#!/usr/bin/env python3
"""
TAS MCP Python Trigger Handler

This module implements trigger handling using the Argo Events paradigm
with Python asyncio and FastAPI for high-performance event processing.
"""

import asyncio
import json
import logging
import time
from datetime import datetime, timezone
from typing import Dict, List, Optional, Any, Union
from dataclasses import dataclass, field
from enum import Enum

import aiohttp
import grpc
from fastapi import FastAPI, HTTPException, BackgroundTasks
from pydantic import BaseModel, Field
import redis.asyncio as redis
from kafka import KafkaProducer

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Pydantic models for API validation
class EventPayload(BaseModel):
    event_id: str = Field(..., description="Unique event identifier")
    event_type: str = Field(..., description="Type of event")
    source: str = Field(..., description="Event source")
    timestamp: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
    data: Dict[str, Any] = Field(default_factory=dict)
    metadata: Dict[str, str] = Field(default_factory=dict)

class ConditionOperator(str, Enum):
    EQ = "eq"
    NE = "ne"
    GT = "gt"
    LT = "lt"
    GTE = "gte"
    LTE = "lte"
    CONTAINS = "contains"
    REGEX = "regex"
    IN = "in"
    NOT_IN = "not_in"

class ActionType(str, Enum):
    HTTP = "http"
    GRPC = "grpc"
    KAFKA = "kafka"
    REDIS = "redis"
    EMAIL = "email"
    WEBHOOK = "webhook"

@dataclass
class Condition:
    field: str
    operator: ConditionOperator
    value: Any
    
@dataclass  
class Action:
    type: ActionType
    target: str
    payload: Dict[str, Any] = field(default_factory=dict)
    timeout: float = 30.0
    retries: int = 3
    headers: Dict[str, str] = field(default_factory=dict)

@dataclass
class TriggerConfig:
    name: str
    conditions: List[Condition] = field(default_factory=list)
    actions: List[Action] = field(default_factory=list)
    enabled: bool = True
    metadata: Dict[str, str] = field(default_factory=dict)
    rate_limit: Optional[int] = None
    cooldown: Optional[float] = None

class TriggerHandler:
    """Main trigger handler implementing Argo Events paradigm."""
    
    def __init__(self):
        self.app = FastAPI(title="TAS MCP Python Trigger Handler")
        self.redis_client = None
        self.kafka_producer = None
        self.session = None
        self.triggers: Dict[str, TriggerConfig] = {}
        self.trigger_stats: Dict[str, Dict[str, int]] = {}
        
        # Initialize default triggers
        self._setup_default_triggers()
        self._setup_routes()
        
    async def initialize(self):
        """Initialize async resources."""
        # Redis connection
        self.redis_client = redis.Redis(
            host='redis-service.redis',
            port=6379,
            decode_responses=True
        )
        
        # Kafka producer
        self.kafka_producer = KafkaProducer(
            bootstrap_servers=['kafka-broker.kafka:9092'],
            value_serializer=lambda x: json.dumps(x).encode('utf-8')
        )
        
        # HTTP session
        self.session = aiohttp.ClientSession()
        
        logger.info("TriggerHandler initialized successfully")

    async def cleanup(self):
        """Cleanup resources."""
        if self.session:
            await self.session.close()
        if self.redis_client:
            await self.redis_client.close()
        if self.kafka_producer:
            self.kafka_producer.close()

    def _setup_routes(self):
        """Setup FastAPI routes."""
        
        @self.app.post("/webhook/github")
        async def github_webhook(payload: EventPayload, background_tasks: BackgroundTasks):
            """Handle GitHub webhook events."""
            logger.info(f"Received GitHub webhook: {payload.event_type}")
            background_tasks.add_task(self.process_event, payload, "github")
            return {"status": "accepted", "event_id": payload.event_id}

        @self.app.post("/webhook/generic")  
        async def generic_webhook(payload: EventPayload, background_tasks: BackgroundTasks):
            """Handle generic webhook events."""
            logger.info(f"Received generic webhook: {payload.event_type}")
            background_tasks.add_task(self.process_event, payload, "generic")
            return {"status": "accepted", "event_id": payload.event_id}

        @self.app.post("/webhook/kafka")
        async def kafka_webhook(payload: EventPayload, background_tasks: BackgroundTasks):
            """Handle Kafka-sourced events."""
            logger.info(f"Received Kafka event: {payload.event_type}")
            background_tasks.add_task(self.process_event, payload, "kafka")
            return {"status": "accepted", "event_id": payload.event_id}

        @self.app.get("/health")
        async def health_check():
            """Health check endpoint."""
            return {"status": "healthy", "timestamp": datetime.now(timezone.utc)}

        @self.app.get("/triggers")
        async def list_triggers():
            """List all configured triggers."""
            return {
                "triggers": list(self.triggers.keys()),
                "stats": self.trigger_stats
            }

        @self.app.post("/triggers/{trigger_name}")
        async def add_trigger(trigger_name: str, config: dict):
            """Add or update a trigger configuration."""
            try:
                trigger_config = self._parse_trigger_config(config)
                trigger_config.name = trigger_name
                self.triggers[trigger_name] = trigger_config
                return {"status": "created", "trigger": trigger_name}
            except Exception as e:
                raise HTTPException(status_code=400, detail=str(e))

        @self.app.delete("/triggers/{trigger_name}")
        async def remove_trigger(trigger_name: str):
            """Remove a trigger configuration."""
            if trigger_name in self.triggers:
                del self.triggers[trigger_name]
                return {"status": "deleted", "trigger": trigger_name}
            raise HTTPException(status_code=404, detail="Trigger not found")

    def _setup_default_triggers(self):
        """Setup default trigger configurations."""
        
        # User creation trigger
        self.triggers["user-welcome"] = TriggerConfig(
            name="user-welcome",
            conditions=[
                Condition("event_type", ConditionOperator.EQ, "user.created"),
                Condition("data.email", ConditionOperator.CONTAINS, "@")
            ],
            actions=[
                Action(
                    type=ActionType.HTTP,
                    target="https://api.example.com/welcome",
                    payload={"template": "welcome", "send_immediately": True},
                    headers={"Authorization": "Bearer ${WELCOME_API_TOKEN}"}
                ),
                Action(
                    type=ActionType.KAFKA,
                    target="user-events",
                    payload={"action": "welcome_sent"}
                )
            ]
        )
        
        # Deployment notification trigger
        self.triggers["deployment-notify"] = TriggerConfig(
            name="deployment-notify", 
            conditions=[
                Condition("event_type", ConditionOperator.EQ, "deployment.completed"),
                Condition("data.environment", ConditionOperator.IN, ["staging", "production"])
            ],
            actions=[
                Action(
                    type=ActionType.HTTP,
                    target="https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK",
                    payload={
                        "text": "🚀 Deployment completed!",
                        "channel": "#deployments"
                    }
                )
            ]
        )
        
        # Critical alert trigger
        self.triggers["critical-alert"] = TriggerConfig(
            name="critical-alert",
            conditions=[
                Condition("event_type", ConditionOperator.EQ, "alert.critical"),
                Condition("data.severity", ConditionOperator.GTE, 8)
            ],
            actions=[
                Action(
                    type=ActionType.HTTP,
                    target="https://api.pagerduty.com/incidents",
                    payload={"urgency": "high"},
                    headers={"Authorization": "Token ${PAGERDUTY_TOKEN}"}
                ),
                Action(
                    type=ActionType.REDIS,
                    target="critical-alerts",
                    payload={"alert_level": "P1"}
                )
            ],
            rate_limit=10,  # Max 10 alerts per minute
            cooldown=300.0  # 5 minute cooldown
        )

        # Data pipeline trigger
        self.triggers["data-pipeline"] = TriggerConfig(
            name="data-pipeline",
            conditions=[
                Condition("event_type", ConditionOperator.EQ, "data.file_uploaded"),
                Condition("data.file_size", ConditionOperator.GT, 1024),
                Condition("data.file_type", ConditionOperator.IN, ["csv", "json", "parquet"])
            ],
            actions=[
                Action(
                    type=ActionType.KAFKA,
                    target="data-processing",
                    payload={"priority": "normal", "auto_process": True}
                ),
                Action(
                    type=ActionType.GRPC,
                    target="data-processor:50051",
                    payload={"method": "ProcessFile"}
                )
            ]
        )

    async def process_event(self, payload: EventPayload, source: str):
        """Process an incoming event against all triggers."""
        logger.info(f"Processing event {payload.event_id} from {source}")
        
        # Find matching triggers
        matching_triggers = []
        for trigger_name, trigger_config in self.triggers.items():
            if self._evaluate_conditions(trigger_config.conditions, payload):
                matching_triggers.append(trigger_config)
        
        logger.info(f"Found {len(matching_triggers)} matching triggers")
        
        # Execute matching triggers
        for trigger in matching_triggers:
            await self._execute_trigger(trigger, payload)
    
    def _evaluate_conditions(self, conditions: List[Condition], payload: EventPayload) -> bool:
        """Evaluate if all conditions are met for the payload."""
        for condition in conditions:
            if not self._evaluate_condition(condition, payload):
                return False
        return True
    
    def _evaluate_condition(self, condition: Condition, payload: EventPayload) -> bool:
        """Evaluate a single condition."""
        # Extract value from payload
        value = self._extract_value(condition.field, payload)
        
        # Handle different operators
        if condition.operator == ConditionOperator.EQ:
            return value == condition.value
        elif condition.operator == ConditionOperator.NE:
            return value != condition.value
        elif condition.operator == ConditionOperator.GT:
            return value > condition.value
        elif condition.operator == ConditionOperator.LT:
            return value < condition.value
        elif condition.operator == ConditionOperator.GTE:
            return value >= condition.value
        elif condition.operator == ConditionOperator.LTE:
            return value <= condition.value
        elif condition.operator == ConditionOperator.CONTAINS:
            return isinstance(value, str) and condition.value in value
        elif condition.operator == ConditionOperator.IN:
            return value in condition.value
        elif condition.operator == ConditionOperator.NOT_IN:
            return value not in condition.value
        else:
            logger.warning(f"Unknown operator: {condition.operator}")
            return False
    
    def _extract_value(self, field: str, payload: EventPayload) -> Any:
        """Extract value from payload based on field path."""
        if field == "event_type":
            return payload.event_type
        elif field == "source":
            return payload.source
        elif field.startswith("data."):
            key = field[5:]  # Remove "data." prefix
            return payload.data.get(key)
        elif field.startswith("metadata."):
            key = field[9:]  # Remove "metadata." prefix  
            return payload.metadata.get(key)
        else:
            return None
    
    async def _execute_trigger(self, trigger: TriggerConfig, payload: EventPayload):
        """Execute all actions for a trigger."""
        if not trigger.enabled:
            logger.debug(f"Trigger {trigger.name} is disabled")
            return
        
        # Check rate limiting and cooldown
        if not await self._check_rate_limit(trigger):
            logger.warning(f"Rate limit exceeded for trigger {trigger.name}")
            return
        
        logger.info(f"Executing trigger: {trigger.name}")
        
        # Update stats
        if trigger.name not in self.trigger_stats:
            self.trigger_stats[trigger.name] = {"executions": 0, "successes": 0, "failures": 0}
        
        self.trigger_stats[trigger.name]["executions"] += 1
        
        # Execute actions concurrently
        tasks = []
        for action in trigger.actions:
            task = asyncio.create_task(self._execute_action(action, payload, trigger.name))
            tasks.append(task)
        
        # Wait for all actions to complete
        results = await asyncio.gather(*tasks, return_exceptions=True)
        
        # Update stats based on results
        successes = sum(1 for r in results if not isinstance(r, Exception))
        failures = len(results) - successes
        
        self.trigger_stats[trigger.name]["successes"] += successes
        self.trigger_stats[trigger.name]["failures"] += failures
        
        logger.info(f"Trigger {trigger.name} completed: {successes} successes, {failures} failures")

    async def _check_rate_limit(self, trigger: TriggerConfig) -> bool:
        """Check if trigger is within rate limits."""
        if not trigger.rate_limit or not self.redis_client:
            return True
        
        key = f"rate_limit:{trigger.name}"
        current = await self.redis_client.get(key)
        
        if current and int(current) >= trigger.rate_limit:
            return False
        
        # Increment counter
        pipe = self.redis_client.pipeline()
        pipe.incr(key)
        pipe.expire(key, 60)  # 1 minute window
        await pipe.execute()
        
        return True

    async def _execute_action(self, action: Action, payload: EventPayload, trigger_name: str):
        """Execute a single action with retries."""
        for attempt in range(action.retries + 1):
            try:
                if action.type == ActionType.HTTP:
                    await self._execute_http_action(action, payload)
                elif action.type == ActionType.KAFKA:
                    await self._execute_kafka_action(action, payload)
                elif action.type == ActionType.REDIS:
                    await self._execute_redis_action(action, payload)
                elif action.type == ActionType.GRPC:
                    await self._execute_grpc_action(action, payload)
                else:
                    logger.warning(f"Unknown action type: {action.type}")
                    return
                
                logger.info(f"Action executed successfully: {action.type} -> {action.target}")
                return
                
            except Exception as e:
                logger.error(f"Action failed (attempt {attempt + 1}): {e}")
                if attempt == action.retries:
                    raise
                await asyncio.sleep(2 ** attempt)  # Exponential backoff

    async def _execute_http_action(self, action: Action, payload: EventPayload):
        """Execute HTTP action."""
        merged_payload = {**action.payload, "event": payload.dict()}
        
        timeout = aiohttp.ClientTimeout(total=action.timeout)
        async with self.session.post(
            action.target,
            json=merged_payload,
            headers=action.headers,
            timeout=timeout
        ) as response:
            response.raise_for_status()
            result = await response.text()
            logger.debug(f"HTTP response: {result}")

    async def _execute_kafka_action(self, action: Action, payload: EventPayload):
        """Execute Kafka action."""
        message = {**action.payload, "event": payload.dict()}
        
        future = self.kafka_producer.send(action.target, message)
        # Wait for the message to be sent
        record_metadata = future.get(timeout=action.timeout)
        logger.debug(f"Kafka message sent to {record_metadata.topic}:{record_metadata.partition}")

    async def _execute_redis_action(self, action: Action, payload: EventPayload):
        """Execute Redis action."""
        message = {**action.payload, "event": payload.dict()}
        
        await self.redis_client.publish(action.target, json.dumps(message))
        logger.debug(f"Redis message published to {action.target}")

    async def _execute_grpc_action(self, action: Action, payload: EventPayload):
        """Execute gRPC action."""
        # This would implement gRPC client calls
        # For now, just log the action
        logger.info(f"gRPC action would be executed: {action.target}")
        
    def _parse_trigger_config(self, config: dict) -> TriggerConfig:
        """Parse trigger configuration from dictionary."""
        conditions = [
            Condition(
                field=c["field"],
                operator=ConditionOperator(c["operator"]),
                value=c["value"]
            )
            for c in config.get("conditions", [])
        ]
        
        actions = [
            Action(
                type=ActionType(a["type"]),
                target=a["target"],
                payload=a.get("payload", {}),
                timeout=a.get("timeout", 30.0),
                retries=a.get("retries", 3),
                headers=a.get("headers", {})
            )
            for a in config.get("actions", [])
        ]
        
        return TriggerConfig(
            name=config["name"],
            conditions=conditions,
            actions=actions,
            enabled=config.get("enabled", True),
            metadata=config.get("metadata", {}),
            rate_limit=config.get("rate_limit"),
            cooldown=config.get("cooldown")
        )

# Global handler instance
handler = TriggerHandler()

# FastAPI app
app = handler.app

@app.on_event("startup")
async def startup_event():
    await handler.initialize()

@app.on_event("shutdown") 
async def shutdown_event():
    await handler.cleanup()

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8080)