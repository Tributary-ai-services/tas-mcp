# TAS MCP Kubernetes Deployments

This directory contains Kubernetes deployment manifests using Kustomize for the TAS MCP server across different environments.

## 📁 Structure

```
k8s/
├── base/                    # Base Kustomize resources
│   ├── kustomization.yaml   # Base kustomization
│   ├── namespace.yaml       # Namespace definition
│   ├── deployment.yaml      # Main deployment + ServiceAccount
│   ├── service.yaml         # HTTP, gRPC, and health services
│   ├── configmap.yaml       # Default configuration
│   ├── servicemonitor.yaml  # Prometheus ServiceMonitor
│   ├── networkpolicy.yaml   # Network security policies
│   └── poddisruptionbudget.yaml # PDB for high availability
└── overlays/
    ├── dev/                 # Development environment
    ├── staging/             # Staging environment
    └── prod/                # Production environment
```

## 🚀 Quick Start

### Prerequisites

- Kubernetes cluster (1.24+)
- `kubectl` configured
- `kustomize` installed (or use `kubectl -k`)

### Deploy to Development

```bash
# Apply development configuration
kubectl apply -k k8s/overlays/dev

# Check deployment status
kubectl get pods -n tas-mcp-dev
kubectl logs -f deployment/dev-tas-mcp -n tas-mcp-dev
```

### Deploy to Staging

```bash
# Apply staging configuration
kubectl apply -k k8s/overlays/staging

# Check deployment status
kubectl get pods -n tas-mcp-staging
kubectl get hpa -n tas-mcp-staging
```

### Deploy to Production

```bash
# Apply production configuration
kubectl apply -k k8s/overlays/prod

# Check deployment status
kubectl get pods -n tas-mcp-prod
kubectl get hpa,vpa -n tas-mcp-prod
```

## ⚙️ Environment Configurations

### Development
- **Replicas:** 1
- **Resources:** 50m CPU, 64Mi RAM
- **Logging:** Debug level
- **Features:** Local ingress, minimal resources

### Staging
- **Replicas:** 2 (with HPA 2-10)
- **Resources:** 200m CPU, 256Mi RAM
- **Logging:** Info level
- **Features:** TLS, rate limiting, auto-scaling

### Production
- **Replicas:** 5 (with HPA 5-50)
- **Resources:** 500m CPU, 512Mi RAM
- **Logging:** Warn level
- **Features:** Full TLS, VPA, pod anti-affinity, advanced scaling

## 🛡️ Security Features

### Base Security
- Non-root container execution
- Read-only root filesystem
- Dropped capabilities
- Security contexts
- Network policies

### Additional Production Security
- Pod anti-affinity rules
- Resource quotas and limits
- TLS termination
- Rate limiting

## 📊 Monitoring & Observability

### Prometheus Integration
- ServiceMonitor for metrics scraping
- Health check endpoints
- Custom metrics for autoscaling

### Health Checks
- **Liveness:** `/health` endpoint
- **Readiness:** `/ready` endpoint
- Configurable timeouts and thresholds

## 🔧 Customization

### Environment Variables
Override via ConfigMap patches:
```yaml
data:
  LOG_LEVEL: "debug"
  BUFFER_SIZE: "2000"
  MAX_CONNECTIONS: "200"
  FORWARD_TO: "http://downstream1:8080,http://downstream2:8080"
```

### Resource Scaling
Adjust HPA metrics and thresholds:
```yaml
metrics:
- type: Resource
  resource:
    name: cpu
    target:
      type: Utilization
      averageUtilization: 70
```

## 🔄 CI/CD Integration

### GitOps Workflow
```bash
# Validate configurations
kustomize build k8s/overlays/prod | kubectl --dry-run=client -f -

# Apply with ArgoCD/Flux
kubectl apply -k k8s/overlays/prod
```

### Image Updates
```bash
# Update image tag
cd k8s/overlays/prod
kustomize edit set image tas-mcp:v1.2.0
```

## 🧪 Testing Deployments

### Connectivity Tests
```bash
# Test HTTP endpoint
kubectl port-forward -n tas-mcp-dev svc/dev-tas-mcp-http 8080:8080
curl http://localhost:8080/health

# Test gRPC endpoint  
kubectl port-forward -n tas-mcp-dev svc/dev-tas-mcp-grpc 50051:50051
grpcurl -plaintext localhost:50051 list
```

### Load Testing
```bash
# HTTP load test
kubectl run load-test --rm -i --tty --image=curlimages/curl -- \
  sh -c 'while true; do curl -X POST http://dev-tas-mcp-http.tas-mcp-dev:8080/mcp \
  -H "Content-Type: application/json" \
  -d "{\"id\":\"test-$(date +%s)\",\"data\":\"{\\\"test\\\":true}\"}"; sleep 0.1; done'
```

## 🗑️ Cleanup

```bash
# Remove specific environment
kubectl delete -k k8s/overlays/dev

# Remove all environments
kubectl delete -k k8s/overlays/dev
kubectl delete -k k8s/overlays/staging  
kubectl delete -k k8s/overlays/prod
```

## 📚 Additional Resources

- [Kustomize Documentation](https://kustomize.io/)
- [Kubernetes Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
- [HPA Configuration](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
- [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)