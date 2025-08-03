#!/usr/bin/env node

/**
 * TAS MCP Node.js Trigger Handler
 * 
 * This module implements trigger handling using the Argo Events paradigm
 * with Node.js, Express, and event-driven architecture.
 */

const express = require('express');
const { EventEmitter } = require('events');
const axios = require('axios');
const Redis = require('ioredis');
const { Kafka } = require('kafkajs');
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');
const winston = require('winston');
const rateLimit = require('express-rate-limit');
const helmet = require('helmet');
const cors = require('cors');

// Configure logging
const logger = winston.createLogger({
  level: process.env.LOG_LEVEL || 'info',
  format: winston.format.combine(
    winston.format.timestamp(),
    winston.format.errors({ stack: true }),
    winston.format.json()
  ),
  transports: [
    new winston.transports.Console(),
    new winston.transports.File({ filename: 'triggers.log' })
  ]
});

// Enums for configuration
const ConditionOperator = {
  EQ: 'eq',
  NE: 'ne', 
  GT: 'gt',
  LT: 'lt',
  GTE: 'gte',
  LTE: 'lte',
  CONTAINS: 'contains',
  REGEX: 'regex',
  IN: 'in',
  NOT_IN: 'not_in'
};

const ActionType = {
  HTTP: 'http',
  GRPC: 'grpc', 
  KAFKA: 'kafka',
  REDIS: 'redis',
  EMAIL: 'email',
  WEBHOOK: 'webhook',
  FUNCTION: 'function'
};

class TriggerHandler extends EventEmitter {
  constructor() {
    super();
    this.app = express();
    this.redis = null;
    this.kafka = null;
    this.grpcClient = null;
    this.triggers = new Map();
    this.triggerStats = new Map();
    this.rateLimiters = new Map();
    
    this.setupMiddleware();
    this.setupDefaultTriggers();
    this.setupRoutes();
  }

  async initialize() {
    try {
      // Initialize Redis
      this.redis = new Redis({
        host: process.env.REDIS_HOST || 'redis-service.redis',
        port: process.env.REDIS_PORT || 6379,
        retryDelayOnFailover: 100,
        maxRetriesPerRequest: 3
      });

      // Initialize Kafka
      this.kafka = new Kafka({
        clientId: 'tas-mcp-triggers',
        brokers: [process.env.KAFKA_BROKERS || 'kafka-broker.kafka:9092']
      });
      this.kafkaProducer = this.kafka.producer();
      await this.kafkaProducer.connect();

      // Initialize gRPC client (placeholder)
      // const packageDefinition = protoLoader.loadSync('path/to/mcp.proto');
      // const mcpProto = grpc.loadPackageDefinition(packageDefinition);
      
      logger.info('TriggerHandler initialized successfully');
    } catch (error) {
      logger.error('Failed to initialize TriggerHandler', error);
      throw error;
    }
  }

  setupMiddleware() {
    this.app.use(helmet());
    this.app.use(cors());
    this.app.use(express.json({ limit: '10mb' }));
    this.app.use(express.urlencoded({ extended: true }));
    
    // Global rate limiting
    const globalLimiter = rateLimit({
      windowMs: 60 * 1000, // 1 minute
      max: 1000, // limit each IP to 1000 requests per windowMs
      message: 'Too many requests from this IP'
    });
    this.app.use(globalLimiter);

    // Request logging
    this.app.use((req, res, next) => {
      logger.info('Request received', {
        method: req.method,
        url: req.url,
        ip: req.ip,
        userAgent: req.get('User-Agent')
      });
      next();
    });
  }

  setupDefaultTriggers() {
    // User registration trigger
    this.triggers.set('user-registration', {
      name: 'user-registration',
      conditions: [
        { field: 'event_type', operator: ConditionOperator.EQ, value: 'user.registered' },
        { field: 'data.email', operator: ConditionOperator.CONTAINS, value: '@' }
      ],
      actions: [
        {
          type: ActionType.HTTP,
          target: 'https://api.sendgrid.com/v3/mail/send',
          payload: {
            personalizations: [{
              to: [{ email: '{{data.email}}' }],
              subject: 'Welcome to TAS MCP!'
            }],
            from: { email: 'noreply@tas-mcp.com' },
            content: [{
              type: 'text/html',
              value: '<h1>Welcome!</h1><p>Thank you for registering.</p>'
            }]
          },
          headers: {
            'Authorization': 'Bearer {{SENDGRID_API_KEY}}',
            'Content-Type': 'application/json'
          },
          timeout: 10000,
          retries: 3
        },
        {
          type: ActionType.KAFKA,
          target: 'user-events',
          payload: {
            action: 'welcome_email_sent',
            user_id: '{{data.user_id}}',
            timestamp: '{{timestamp}}'
          }
        }
      ],
      enabled: true,
      rateLimit: 100, // Max 100 per minute
      cooldown: 5000 // 5 second cooldown
    });

    // CI/CD Pipeline trigger
    this.triggers.set('cicd-pipeline', {
      name: 'cicd-pipeline',
      conditions: [
        { field: 'event_type', operator: ConditionOperator.EQ, value: 'git.push' },
        { field: 'data.branch', operator: ConditionOperator.IN, value: ['main', 'master', 'develop'] },
        { field: 'data.repository', operator: ConditionOperator.CONTAINS, value: 'tas-mcp' }
      ],
      actions: [
        {
          type: ActionType.HTTP,
          target: 'https://api.github.com/repos/{{data.repository}}/actions/workflows/deploy.yml/dispatches',
          payload: {
            ref: '{{data.branch}}',
            inputs: {
              environment: '{{data.branch === "main" ? "production" : "staging"}}',
              triggered_by: 'argo-events'
            }
          },
          headers: {
            'Authorization': 'token {{GITHUB_TOKEN}}',
            'Accept': 'application/vnd.github.v3+json'
          },
          timeout: 15000,
          retries: 2
        },
        {
          type: ActionType.FUNCTION,
          target: 'notifyTeam',
          payload: {
            message: 'Deployment triggered for {{data.repository}}:{{data.branch}}',
            channel: '#deployments'
          }
        }
      ],
      enabled: true
    });

    // Monitoring alert trigger
    this.triggers.set('monitoring-alert', {
      name: 'monitoring-alert',
      conditions: [
        { field: 'event_type', operator: ConditionOperator.EQ, value: 'alert.triggered' },
        { field: 'data.severity', operator: ConditionOperator.IN, value: ['critical', 'high'] }
      ],
      actions: [
        {
          type: ActionType.HTTP,
          target: 'https://hooks.slack.com/services/{{SLACK_WEBHOOK_PATH}}',
          payload: {
            text: '🚨 Alert: {{data.title}}',
            attachments: [{
              color: '{{data.severity === "critical" ? "danger" : "warning"}}',
              fields: [
                { title: 'Severity', value: '{{data.severity}}', short: true },
                { title: 'Service', value: '{{data.service}}', short: true },
                { title: 'Environment', value: '{{data.environment}}', short: true },
                { title: 'Description', value: '{{data.description}}', short: false }
              ],
              actions: [{
                type: 'button',
                text: 'View Dashboard',
                url: '{{data.dashboard_url}}'
              }]
            }]
          },
          timeout: 10000,
          retries: 3
        },
        {
          type: ActionType.REDIS,
          target: 'alerts',
          payload: {
            alert_id: '{{event_id}}',
            severity: '{{data.severity}}',
            timestamp: '{{timestamp}}',
            acknowledged: false
          }
        }
      ],
      enabled: true,
      rateLimit: 50
    });

    // Data processing trigger
    this.triggers.set('data-processing', {
      name: 'data-processing',
      conditions: [
        { field: 'event_type', operator: ConditionOperator.EQ, value: 'file.uploaded' },
        { field: 'data.file_type', operator: ConditionOperator.IN, value: ['csv', 'json', 'parquet'] },
        { field: 'data.size', operator: ConditionOperator.GT, value: 1024 }
      ],
      actions: [
        {
          type: ActionType.GRPC,
          target: 'data-processor:50051',
          payload: {
            file_path: '{{data.file_path}}',
            file_type: '{{data.file_type}}',
            processing_options: {
              validate: true,
              transform: '{{data.auto_transform || false}}',
              notify_completion: true
            }
          },
          timeout: 30000,
          retries: 2
        },
        {
          type: ActionType.KAFKA,
          target: 'data-pipeline',
          payload: {
            event: 'processing_started',
            file_id: '{{data.file_id}}',
            estimated_duration: '{{data.size > 10000000 ? 300 : 60}}'
          }
        }
      ],
      enabled: true
    });

    // Security incident trigger
    this.triggers.set('security-incident', {
      name: 'security-incident',
      conditions: [
        { field: 'event_type', operator: ConditionOperator.EQ, value: 'security.incident' },
        { field: 'data.threat_level', operator: ConditionOperator.GTE, value: 7 }
      ],
      actions: [
        {
          type: ActionType.HTTP,
          target: 'https://api.opsgenie.com/v2/alerts',
          payload: {
            message: 'Security Incident: {{data.incident_type}}',
            description: '{{data.description}}',
            priority: 'P1',
            tags: ['security', 'incident', '{{data.incident_type}}'],
            details: {
              source_ip: '{{data.source_ip}}',
              target: '{{data.target}}',
              threat_level: '{{data.threat_level}}'
            }
          },
          headers: {
            'Authorization': 'GenieKey {{OPSGENIE_API_KEY}}',
            'Content-Type': 'application/json'
          },
          timeout: 15000,
          retries: 3
        },
        {
          type: ActionType.FUNCTION,
          target: 'quarantineResource',
          payload: {
            resource_id: '{{data.target}}',
            reason: 'Security incident detected',
            auto_restore: false
          }
        }
      ],
      enabled: true,
      rateLimit: 10, // Very restrictive for security
      cooldown: 60000 // 1 minute cooldown
    });

    logger.info(`Loaded ${this.triggers.size} default triggers`);
  }

  setupRoutes() {
    // Health check
    this.app.get('/health', (req, res) => {
      res.json({
        status: 'healthy',
        timestamp: new Date().toISOString(),
        triggers: this.triggers.size,
        uptime: process.uptime()
      });
    });

    // Webhook endpoints
    this.app.post('/webhook/github', this.createWebhookHandler('github'));
    this.app.post('/webhook/generic', this.createWebhookHandler('generic'));  
    this.app.post('/webhook/kafka', this.createWebhookHandler('kafka'));
    this.app.post('/webhook/monitoring', this.createWebhookHandler('monitoring'));

    // Trigger management
    this.app.get('/triggers', (req, res) => {
      const triggerList = Array.from(this.triggers.values()).map(trigger => ({
        name: trigger.name,
        enabled: trigger.enabled,
        conditions: trigger.conditions.length,
        actions: trigger.actions.length,
        stats: this.triggerStats.get(trigger.name) || { executions: 0, successes: 0, failures: 0 }
      }));
      
      res.json({ triggers: triggerList });
    });

    this.app.post('/triggers/:name', async (req, res) => {
      try {
        const triggerName = req.params.name;
        const triggerConfig = this.validateTriggerConfig(req.body);
        triggerConfig.name = triggerName;
        
        this.triggers.set(triggerName, triggerConfig);
        logger.info(`Trigger ${triggerName} added/updated`);
        
        res.json({ status: 'success', trigger: triggerName });
      } catch (error) {
        logger.error('Failed to add trigger', error);
        res.status(400).json({ error: error.message });
      }
    });

    this.app.delete('/triggers/:name', (req, res) => {
      const triggerName = req.params.name;
      if (this.triggers.has(triggerName)) {
        this.triggers.delete(triggerName);
        this.triggerStats.delete(triggerName);
        logger.info(`Trigger ${triggerName} deleted`);
        res.json({ status: 'deleted', trigger: triggerName });
      } else {
        res.status(404).json({ error: 'Trigger not found' });
      }
    });

    // Statistics endpoint
    this.app.get('/stats', (req, res) => {
      const stats = {
        total_triggers: this.triggers.size,
        active_triggers: Array.from(this.triggers.values()).filter(t => t.enabled).length,
        trigger_stats: Object.fromEntries(this.triggerStats),
        uptime: process.uptime(),
        memory_usage: process.memoryUsage()
      };
      res.json(stats);
    });
  }

  createWebhookHandler(source) {
    return async (req, res) => {
      try {
        const payload = this.normalizePayload(req.body, source);
        
        logger.info('Webhook received', {
          source,
          eventType: payload.event_type,
          eventId: payload.event_id
        });

        // Process asynchronously
        setImmediate(() => this.processEvent(payload, source));
        
        res.json({ 
          status: 'accepted', 
          event_id: payload.event_id,
          source 
        });
      } catch (error) {
        logger.error('Webhook processing error', error);
        res.status(400).json({ error: error.message });
      }
    };
  }

  normalizePayload(body, source) {
    // Normalize different payload formats
    return {
      event_id: body.event_id || body.id || this.generateEventId(),
      event_type: body.event_type || body.type || 'generic.event',
      source: body.source || source,
      timestamp: body.timestamp || new Date().toISOString(),
      data: body.data || body,
      metadata: body.metadata || {}
    };
  }

  async processEvent(payload, source) {
    logger.info(`Processing event ${payload.event_id} from ${source}`);
    
    const matchingTriggers = [];
    
    // Find matching triggers
    for (const [name, trigger] of this.triggers) {
      if (trigger.enabled && this.evaluateConditions(trigger.conditions, payload)) {
        matchingTriggers.push(trigger);
      }
    }

    logger.info(`Found ${matchingTriggers.length} matching triggers`);

    // Execute triggers
    const promises = matchingTriggers.map(trigger => 
      this.executeTrigger(trigger, payload).catch(error => {
        logger.error(`Trigger ${trigger.name} failed`, error);
        return { trigger: trigger.name, error: error.message };
      })
    );

    await Promise.allSettled(promises);
  }

  evaluateConditions(conditions, payload) {
    return conditions.every(condition => this.evaluateCondition(condition, payload));
  }

  evaluateCondition(condition, payload) {
    const value = this.extractValue(condition.field, payload);
    
    switch (condition.operator) {
      case ConditionOperator.EQ:
        return value === condition.value;
      case ConditionOperator.NE:
        return value !== condition.value;
      case ConditionOperator.GT:
        return value > condition.value;
      case ConditionOperator.LT:
        return value < condition.value;
      case ConditionOperator.GTE:
        return value >= condition.value;
      case ConditionOperator.LTE:
        return value <= condition.value;
      case ConditionOperator.CONTAINS:
        return typeof value === 'string' && value.includes(condition.value);
      case ConditionOperator.IN:
        return Array.isArray(condition.value) && condition.value.includes(value);
      case ConditionOperator.NOT_IN:
        return Array.isArray(condition.value) && !condition.value.includes(value);
      case ConditionOperator.REGEX:
        return new RegExp(condition.value).test(String(value));
      default:
        logger.warn(`Unknown operator: ${condition.operator}`);
        return false;
    }
  }

  extractValue(field, payload) {
    const parts = field.split('.');
    let value = payload;
    
    for (const part of parts) {
      if (value && typeof value === 'object') {
        value = value[part];
      } else {
        return undefined;
      }
    }
    
    return value;
  }

  async executeTrigger(trigger, payload) {
    // Check rate limiting
    if (!(await this.checkRateLimit(trigger))) {
      logger.warn(`Rate limit exceeded for trigger ${trigger.name}`);
      return;
    }

    logger.info(`Executing trigger: ${trigger.name}`);

    // Initialize stats
    if (!this.triggerStats.has(trigger.name)) {
      this.triggerStats.set(trigger.name, { executions: 0, successes: 0, failures: 0 });
    }
    
    const stats = this.triggerStats.get(trigger.name);
    stats.executions++;

    // Execute actions
    const actionPromises = trigger.actions.map(action => 
      this.executeAction(action, payload, trigger.name)
    );

    const results = await Promise.allSettled(actionPromises);
    
    // Update stats
    const successes = results.filter(r => r.status === 'fulfilled').length;
    const failures = results.length - successes;
    
    stats.successes += successes;
    stats.failures += failures;

    logger.info(`Trigger ${trigger.name} completed: ${successes} successes, ${failures} failures`);
  }

  async checkRateLimit(trigger) {
    if (!trigger.rateLimit || !this.redis) return true;
    
    const key = `rate_limit:${trigger.name}`;
    const current = await this.redis.get(key);
    
    if (current && parseInt(current) >= trigger.rateLimit) {
      return false;
    }
    
    await this.redis.multi()
      .incr(key)
      .expire(key, 60)
      .exec();
    
    return true;
  }

  async executeAction(action, payload, triggerName) {
    const maxRetries = action.retries || 3;
    
    for (let attempt = 0; attempt <= maxRetries; attempt++) {
      try {
        await this.performAction(action, payload);
        logger.info(`Action executed successfully: ${action.type} -> ${action.target}`);
        return;
      } catch (error) {
        logger.error(`Action failed (attempt ${attempt + 1}/${maxRetries + 1})`, error);
        
        if (attempt === maxRetries) {
          throw error;
        }
        
        // Exponential backoff
        await this.sleep(Math.pow(2, attempt) * 1000);
      }
    }
  }

  async performAction(action, payload) {
    const processedPayload = this.processTemplate(action.payload, payload);
    const processedHeaders = this.processTemplate(action.headers || {}, payload);
    
    switch (action.type) {
      case ActionType.HTTP:
        await this.executeHttpAction(action.target, processedPayload, processedHeaders, action.timeout);
        break;
      case ActionType.KAFKA:
        await this.executeKafkaAction(action.target, processedPayload);
        break;
      case ActionType.REDIS:
        await this.executeRedisAction(action.target, processedPayload);
        break;
      case ActionType.GRPC:
        await this.executeGrpcAction(action.target, processedPayload);
        break;
      case ActionType.FUNCTION:
        await this.executeFunctionAction(action.target, processedPayload);
        break;
      default:
        throw new Error(`Unknown action type: ${action.type}`);
    }
  }

  async executeHttpAction(target, payload, headers, timeout = 30000) {
    const response = await axios.post(target, payload, {
      headers,
      timeout,
      validateStatus: status => status < 400
    });
    
    logger.debug(`HTTP response: ${response.status}`);
  }

  async executeKafkaAction(topic, payload) {
    await this.kafkaProducer.send({
      topic,
      messages: [{
        value: JSON.stringify(payload),
        timestamp: Date.now().toString()
      }]
    });
    
    logger.debug(`Kafka message sent to topic: ${topic}`);
  }

  async executeRedisAction(channel, payload) {
    await this.redis.publish(channel, JSON.stringify(payload));
    logger.debug(`Redis message published to channel: ${channel}`);
  }

  async executeGrpcAction(target, payload) {
    // Placeholder for gRPC implementation
    logger.info(`gRPC action would be executed: ${target}`);
  }

  async executeFunctionAction(functionName, payload) {
    // Execute built-in functions
    switch (functionName) {
      case 'notifyTeam':
        await this.notifyTeam(payload);
        break;
      case 'quarantineResource':
        await this.quarantineResource(payload);
        break;
      default:
        logger.warn(`Unknown function: ${functionName}`);
    }
  }

  async notifyTeam(payload) {
    // Implementation for team notification
    logger.info('Team notification sent', payload);
  }

  async quarantineResource(payload) {
    // Implementation for resource quarantine
    logger.info('Resource quarantined', payload);
  }

  processTemplate(template, payload) {
    if (typeof template === 'string') {
      return template.replace(/\{\{([^}]+)\}\}/g, (match, expression) => {
        try {
          // Simple template processing - in production, use a proper template engine
          return this.evaluateExpression(expression.trim(), payload);
        } catch (error) {
          logger.warn(`Template evaluation failed: ${expression}`, error);
          return match;
        }
      });
    } else if (Array.isArray(template)) {
      return template.map(item => this.processTemplate(item, payload));
    } else if (template && typeof template === 'object') {
      const result = {};
      for (const [key, value] of Object.entries(template)) {
        result[key] = this.processTemplate(value, payload);
      }
      return result;
    }
    
    return template;
  }

  evaluateExpression(expression, payload) {
    // Simple expression evaluation - extend as needed
    if (expression.includes('.')) {
      return this.extractValue(expression, payload);
    }
    
    // Handle environment variables
    if (expression.startsWith('SENDGRID_API_KEY') || expression.startsWith('GITHUB_TOKEN')) {
      return process.env[expression] || '';
    }
    
    return expression;
  }

  validateTriggerConfig(config) {
    if (!config.name) throw new Error('Trigger name is required');
    if (!Array.isArray(config.conditions)) throw new Error('Conditions must be an array');
    if (!Array.isArray(config.actions)) throw new Error('Actions must be an array');
    
    return {
      name: config.name,
      conditions: config.conditions,
      actions: config.actions,
      enabled: config.enabled !== false,
      rateLimit: config.rateLimit,
      cooldown: config.cooldown
    };
  }

  generateEventId() {
    return `evt_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
  }

  sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
  }

  async start(port = 8080) {
    await this.initialize();
    
    this.app.listen(port, () => {
      logger.info(`TAS MCP Trigger Handler listening on port ${port}`);
    });

    // Graceful shutdown
    process.on('SIGTERM', async () => {
      logger.info('Shutting down gracefully...');
      if (this.kafkaProducer) await this.kafkaProducer.disconnect();
      if (this.redis) this.redis.disconnect();
      process.exit(0);
    });
  }
}

// Export for use as module
module.exports = TriggerHandler;

// Run if called directly
if (require.main === module) {
  const handler = new TriggerHandler();
  handler.start().catch(error => {
    logger.error('Failed to start trigger handler', error);
    process.exit(1);
  });
}