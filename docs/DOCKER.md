# Docker Deployment Guide

This guide covers how to build, run, and deploy the TAS MCP Server using Docker.

## Quick Start

### Single Container

```bash
# Build the Docker image
make docker

# Run the container
make docker-run
```

### Full Stack with Docker Compose

```bash
# Start all services
make docker-compose

# Stop all services
make docker-compose-down
```

## Docker Image

The TAS MCP Server uses a multi-stage Docker build:

1. **Builder Stage**: Uses `golang:1.22-alpine` to compile the Go binary
2. **Runtime Stage**: Uses `alpine:3.18` for a minimal runtime environment

### Image Features

- **Minimal size**: Alpine-based runtime image
- **Security**: Runs as non-root user (UID 65534)
- **Health checks**: Built-in health monitoring
- **Configuration**: Environment variable and file-based config
- **Observability**: Includes CA certificates for HTTPS calls

### Exposed Ports

- `8080`: HTTP API server
- `50051`: gRPC server  
- `8082`: Health check endpoint

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | Logging level (debug, info, warn, error) |
| `HTTP_PORT` | `8080` | HTTP server port |
| `GRPC_PORT` | `50051` | gRPC server port |
| `HEALTH_CHECK_PORT` | `8082` | Health check port |
| `FORWARDING_ENABLED` | `false` | Enable event forwarding |
| `FORWARDING_WORKERS` | `5` | Number of forwarding workers |
| `FORWARDING_BUFFER_SIZE` | `1000` | Event buffer size |

## Docker Compose Services

### Core Services

- **tas-mcp-server**: Main application server
- **redis**: Caching and rate limiting (optional)

### Optional Services

- **kafka + zookeeper**: Event streaming platform
- **prometheus**: Metrics collection
- **grafana**: Metrics visualization

### Service Profiles

Use Docker Compose profiles to control which services run:

```bash
# Run only core services (default)
docker-compose up

# Run with monitoring stack
docker-compose --profile monitoring up

# Run full stack with Kafka
docker-compose --profile full up
```

## Development

### Development Override

The `docker-compose.override.yml` file provides development-specific configurations:

- Hot reload with file watching
- Debug logging enabled
- Debugger port exposed (40000)
- Minimal service dependencies

### Development Workflow

```bash
# Start development environment
docker-compose up

# View logs
docker-compose logs -f tas-mcp-server

# Execute commands in container
docker-compose exec tas-mcp-server sh

# Restart specific service
docker-compose restart tas-mcp-server
```

## Production Deployment

### Building for Production

```bash
# Build with version tag
VERSION=1.0.0 make docker

# Push to registry
make docker-push
```

### Production Configuration

1. **Use external configuration files**:
   ```bash
   docker run -v /path/to/config.json:/configs/config.json tas-mcp:latest -config /configs/config.json
   ```

2. **Set production environment variables**:
   ```bash
   docker run -e LOG_LEVEL=warn -e FORWARDING_ENABLED=true tas-mcp:latest
   ```

3. **Use secrets management**:
   ```bash
   docker run --env-file production.env tas-mcp:latest
   ```

### Health Monitoring

The container includes comprehensive health checks:

```bash
# Check container health
docker ps

# View health check logs
docker inspect --format='{{json .State.Health}}' <container-id>

# Manual health check
curl http://localhost:8082/health
```

## Troubleshooting

### Common Issues

1. **Port conflicts**:
   ```bash
   # Check port usage
   netstat -tulpn | grep :8080
   
   # Use different ports
   docker run -p 8081:8080 tas-mcp:latest
   ```

2. **Permission issues**:
   ```bash
   # Check file permissions
   ls -la configs/
   
   # Fix ownership
   sudo chown -R 65534:65534 configs/
   ```

3. **Memory issues**:
   ```bash
   # Set memory limits
   docker run --memory=512m tas-mcp:latest
   
   # Monitor memory usage
   docker stats
   ```

### Debugging

1. **Enable debug logging**:
   ```bash
   docker run -e LOG_LEVEL=debug tas-mcp:latest
   ```

2. **Access container shell**:
   ```bash
   docker run -it tas-mcp:latest sh
   ```

3. **Override entrypoint**:
   ```bash
   docker run -it --entrypoint=sh tas-mcp:latest
   ```

## Security Considerations

1. **Non-root execution**: Container runs as user ID 65534
2. **Minimal attack surface**: Alpine-based image with minimal packages
3. **No sensitive data**: Configuration should use secrets management
4. **Network security**: Use Docker networks for service isolation

## Integration Examples

### With Kubernetes

See [k8s/](../k8s/) directory for Kubernetes manifests.

### With CI/CD

```yaml
# Example GitHub Actions workflow
- name: Build Docker image
  run: make docker

- name: Push to registry
  run: make docker-push
  env:
    VERSION: ${{ github.sha }}
```

### With External Services

```yaml
# Example integration with external Kafka
services:
  tas-mcp-server:
    environment:
      - FORWARDING_ENABLED=true
      - FORWARDING_TARGETS='[{"id":"kafka","type":"kafka","endpoint":"external-kafka:9092"}]'
```