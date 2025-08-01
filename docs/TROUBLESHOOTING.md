# 🔧 TAS MCP Server Troubleshooting Guide

This guide helps you diagnose and resolve common issues with the TAS MCP Server.

## Table of Contents

- [Common Issues](#common-issues)
- [Debugging Techniques](#debugging-techniques)
- [Performance Issues](#performance-issues)
- [Integration Problems](#integration-problems)
- [Deployment Issues](#deployment-issues)
- [Getting Help](#getting-help)

## Common Issues

### Server Won't Start

#### Symptom
```
Error: failed to start server: listen tcp :8080: bind: address already in use
```

#### Solution
1. Check if port is in use:
   ```bash
   lsof -i :8080
   # or
   netstat -tulpn | grep :8080
   ```

2. Kill the process or use a different port:
   ```bash
   # Kill process
   kill -9 <PID>
   
   # Or use different port
   HTTP_PORT=8081 ./tas-mcp
   ```

### Connection Refused

#### Symptom
```
Error: connection refused
curl: (7) Failed to connect to localhost port 8080: Connection refused
```

#### Solution
1. Verify server is running:
   ```bash
   ps aux | grep tas-mcp
   ```

2. Check server logs:
   ```bash
   docker logs tas-mcp-server
   # or
   journalctl -u tas-mcp -f
   ```

3. Verify correct port:
   ```bash
   # Check listening ports
   ss -tlnp | grep tas-mcp
   ```

### Invalid Configuration

#### Symptom
```
Error: failed to load config: invalid character '}' looking for beginning of object key string
```

#### Solution
1. Validate JSON syntax:
   ```bash
   jq . config.json
   ```

2. Check for common issues:
   - Missing commas
   - Trailing commas
   - Unmatched brackets
   - Invalid quotes

3. Use example configuration:
   ```bash
   cp configs/forwarding-example.json config.json
   vim config.json  # Edit as needed
   ```

### Event Rejection

#### Symptom
```
{"error":{"code":"INVALID_EVENT","message":"Event validation failed"}}
```

#### Solution
1. Check required fields:
   ```json
   {
     "event_id": "required",
     "event_type": "required", 
     "source": "required",
     "data": "required JSON string"
   }
   ```

2. Validate JSON in data field:
   ```bash
   # Test with curl
   curl -X POST http://localhost:8080/api/v1/events \
     -H "Content-Type: application/json" \
     -d '{
       "event_id": "test-1",
       "event_type": "test.event",
       "source": "test",
       "data": "{\"valid\": \"json\"}"
     }'
   ```

### Memory Issues

#### Symptom
```
runtime: out of memory
```

#### Solution
1. Check memory usage:
   ```bash
   docker stats tas-mcp-server
   # or
   top -p $(pgrep tas-mcp)
   ```

2. Increase memory limits:
   ```yaml
   # docker-compose.yml
   services:
     tas-mcp-server:
       mem_limit: 1g
       memswap_limit: 2g
   ```

3. Tune buffer sizes:
   ```bash
   BUFFER_SIZE=500 FORWARDING_BUFFER_SIZE=500 ./tas-mcp
   ```

## Debugging Techniques

### Enable Debug Logging

```bash
# Environment variable
LOG_LEVEL=debug ./tas-mcp

# Or in config
{
  "LogLevel": "debug"
}

# Docker
docker run -e LOG_LEVEL=debug tas-mcp
```

### Analyze Logs

#### Structured Log Queries
```bash
# Filter by level
docker logs tas-mcp-server 2>&1 | jq 'select(.level=="error")'

# Filter by component
docker logs tas-mcp-server 2>&1 | jq 'select(.component=="forwarder")'

# Search for event ID
docker logs tas-mcp-server 2>&1 | grep "evt-123"
```

### Enable Profiling

1. Start with profiling enabled:
   ```go
   // In main.go
   import _ "net/http/pprof"
   
   go func() {
       log.Println(http.ListenAndServe("localhost:6060", nil))
   }()
   ```

2. Capture profiles:
   ```bash
   # CPU profile
   go tool pprof -http=:8081 http://localhost:6060/debug/pprof/profile

   # Memory profile
   go tool pprof -http=:8081 http://localhost:6060/debug/pprof/heap

   # Goroutine profile
   go tool pprof -http=:8081 http://localhost:6060/debug/pprof/goroutine
   ```

### Trace Requests

```bash
# Enable request tracing
curl -X POST http://localhost:8080/api/v1/events \
  -H "X-Request-ID: trace-123" \
  -H "X-Debug: true" \
  -v \
  -d '{"event_id": "test-1", ...}'
```

## Performance Issues

### High CPU Usage

#### Diagnosis
```bash
# Check CPU usage
top -H -p $(pgrep tas-mcp)

# Profile CPU
wget http://localhost:6060/debug/pprof/profile?seconds=30 -O cpu.prof
go tool pprof -http=:8081 cpu.prof
```

#### Solutions
1. **Reduce worker count**:
   ```bash
   FORWARDING_WORKERS=3 ./tas-mcp
   ```

2. **Optimize rules**:
   - Use specific conditions
   - Avoid regex when possible
   - Order rules by frequency

3. **Enable batching**:
   ```json
   {
     "config": {
       "batch_size": 100,
       "batch_timeout": "1s"
     }
   }
   ```

### High Memory Usage

#### Diagnosis
```bash
# Memory profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof -http=:8081 heap.prof

# Check for leaks
go tool pprof -diff_base=heap1.prof heap2.prof
```

#### Solutions
1. **Reduce buffer sizes**:
   ```json
   {
     "BufferSize": 500,
     "forwarding": {
       "buffer_size": 500
     }
   }
   ```

2. **Set connection limits**:
   ```json
   {
     "MaxConnections": 50
   }
   ```

3. **Enable GC tuning**:
   ```bash
   GOGC=50 ./tas-mcp  # More aggressive GC
   ```

### Slow Event Processing

#### Diagnosis
```bash
# Check metrics
curl http://localhost:8080/api/v1/metrics | grep latency

# Monitor forwarding
curl http://localhost:8080/api/v1/forwarding/metrics
```

#### Solutions
1. **Optimize forwarding targets**:
   - Use connection pooling
   - Implement circuit breakers
   - Add caching

2. **Tune timeouts**:
   ```json
   {
     "ForwardTimeout": "10s",
     "forwarding": {
       "default_timeout": "5s"
     }
   }
   ```

## Integration Problems

### Argo Events Integration

#### Webhook Not Receiving Events
1. Verify endpoint:
   ```bash
   kubectl get svc -n argo-events
   ```

2. Test connectivity:
   ```bash
   kubectl exec -it deployment/tas-mcp -- wget -O- http://eventbus-webhook-eventsource-svc:12000/webhook
   ```

3. Check event source logs:
   ```bash
   kubectl logs -n argo-events deployment/webhook-eventsource
   ```

### Kafka Integration

#### Connection Failed
1. Verify Kafka is running:
   ```bash
   kafka-topics.sh --bootstrap-server localhost:9092 --list
   ```

2. Test connectivity:
   ```bash
   kafkacat -b localhost:9092 -L
   ```

3. Check configuration:
   ```json
   {
     "type": "kafka",
     "endpoint": "kafka:9092",  // Use service name in K8s
     "config": {
       "topic": "mcp-events",
       "client_id": "tas-mcp"
     }
   }
   ```

### gRPC Client Issues

#### Connection Errors
```go
// Add retry logic
opts := []grpc.DialOption{
    grpc.WithInsecure(),
    grpc.WithBlock(),
    grpc.WithTimeout(5 * time.Second),
    grpc.WithDefaultCallOptions(
        grpc.WaitForReady(true),
    ),
}

conn, err := grpc.Dial("localhost:50051", opts...)
```

#### Stream Errors
```go
// Handle stream reconnection
for {
    stream, err := client.EventStream(ctx)
    if err != nil {
        log.Printf("Stream error: %v, retrying...", err)
        time.Sleep(5 * time.Second)
        continue
    }
    
    // Use stream...
}
```

## Deployment Issues

### Kubernetes Deployment

#### Pod Crashing
```bash
# Check pod status
kubectl describe pod tas-mcp-xxx

# Check logs
kubectl logs tas-mcp-xxx --previous

# Common issues:
# - OOMKilled: Increase memory limits
# - CrashLoopBackOff: Check configuration
# - ImagePullBackOff: Verify image name/tag
```

#### Service Discovery
```bash
# Verify service
kubectl get svc tas-mcp

# Test from another pod
kubectl run test --rm -it --image=busybox -- wget -O- http://tas-mcp:8080/health
```

### Docker Issues

#### Container Won't Start
```bash
# Check logs
docker logs tas-mcp-server

# Debug with shell
docker run -it --entrypoint sh tas-mcp

# Common issues:
# - Permission denied: Check file permissions
# - Exec format error: Wrong architecture
```

#### Networking Issues
```bash
# Verify network
docker network ls
docker network inspect bridge

# Test connectivity
docker run --rm --network container:tas-mcp-server nicolaka/netshoot curl localhost:8080/health
```

## Getting Help

### Collect Diagnostic Information

```bash
# Create diagnostic bundle
mkdir tas-mcp-diag
cd tas-mcp-diag

# System info
uname -a > system.txt
go version >> system.txt

# Container info (if using Docker)
docker version > docker.txt
docker info >> docker.txt
docker logs tas-mcp-server > logs.txt 2>&1

# Configuration (remove sensitive data)
cat config.json | jq 'del(.authentication)' > config.json

# Metrics
curl http://localhost:8080/api/v1/metrics > metrics.txt
curl http://localhost:8080/api/v1/stats > stats.txt

# Create archive
tar -czf tas-mcp-diag.tar.gz .
```

### Where to Get Help

1. **Documentation**:
   - [README.md](../README.md)
   - [DEVELOPER.md](../DEVELOPER.md)
   - [API.md](API.md)

2. **Community**:
   - GitHub Issues: Report bugs
   - GitHub Discussions: Ask questions
   - Discord: Real-time help

3. **Commercial Support**:
   - Email: support@tributary-ai-services.com
   - Enterprise support plans available

### Reporting Issues

When reporting issues, include:
1. **Version information**
2. **Configuration** (sanitized)
3. **Steps to reproduce**
4. **Error messages/logs**
5. **Expected vs actual behavior**
6. **Diagnostic bundle** (if requested)

---

Remember: Most issues have simple solutions. Check logs, verify configuration, and test connectivity first!