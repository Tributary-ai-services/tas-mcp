# 🚀 TAS MCP Server Deployment Guide

This guide covers various deployment options for the TAS MCP Server, from local development to production Kubernetes clusters.

## Table of Contents

- [Quick Start](#quick-start)
- [Local Development](#local-development)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Cloud Deployments](#cloud-deployments)
- [Production Considerations](#production-considerations)
- [Monitoring & Observability](#monitoring--observability)

## Quick Start

### Single Command Deployment

```bash
# Docker
docker run -p 8080:8080 -p 50051:50051 ghcr.io/tributary-ai-services/tas-mcp:latest

# Kubernetes (with kubectl)
kubectl apply -k https://github.com/tributary-ai-services/tas-mcp/k8s/overlays/dev

# Docker Compose
curl -sSL https://raw.githubusercontent.com/tributary-ai-services/tas-mcp/main/docker-compose.yml | docker-compose -f - up
```

## Local Development

### Prerequisites

- Go 1.22+
- Make
- Docker (optional)

### Native Development

```bash
# Clone repository
git clone https://github.com/tributary-ai-services/tas-mcp.git
cd tas-mcp

# Install dependencies
make init

# Run with default configuration
make run

# Run with custom configuration
./bin/tas-mcp -config configs/dev.json

# Run with environment variables
LOG_LEVEL=debug HTTP_PORT=8081 ./bin/tas-mcp
```

### Using Docker Compose

```bash
# Start development stack
make docker-compose

# View logs
docker-compose logs -f tas-mcp-server

# Scale services
docker-compose up --scale tas-mcp-server=3

# Stop services
make docker-compose-down
```

### Hot Reload Development

```bash
# Install air (if not already installed)
go install github.com/cosmtrek/air@latest

# Run with hot reload
make dev
```

## Docker Deployment

### Single Container

```bash
# Build locally
make docker

# Run with basic configuration
docker run \
  -p 8080:8080 \
  -p 50051:50051 \
  -p 8082:8082 \
  -e LOG_LEVEL=info \
  -e FORWARDING_ENABLED=true \
  tas-mcp:latest

# Run with custom configuration
docker run \
  -p 8080:8080 \
  -p 50051:50051 \
  -v $(pwd)/config.json:/config.json \
  tas-mcp:latest -config /config.json
```

### Docker Compose Production

```yaml
# docker-compose.prod.yml
version: '3.8'

services:
  tas-mcp-server:
    image: ghcr.io/tributary-ai-services/tas-mcp:v1.0.0
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "50051:50051"
    environment:
      - LOG_LEVEL=warn
      - FORWARDING_ENABLED=true
      - FORWARDING_WORKERS=10
      - MAX_CONNECTIONS=1000
    volumes:
      - ./production-config.json:/config.json:ro
      - tas-mcp-data:/data
    command: ["-config", "/config.json"]
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8082/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: '1.0'
        reservations:
          memory: 256M
          cpus: '0.5'

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    volumes:
      - redis-data:/data
    command: redis-server --appendonly yes

  prometheus:
    image: prom/prometheus:latest
    restart: unless-stopped
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus

volumes:
  tas-mcp-data:
  redis-data:
  prometheus-data:
```

### Multi-Stage Builds

```dockerfile
# Dockerfile.prod
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tas-mcp ./cmd/server

FROM alpine:3.18
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/
COPY --from=builder /app/tas-mcp .
COPY --from=builder /app/configs ./configs
EXPOSE 8080 50051 8082
CMD ["./tas-mcp"]
```

## Kubernetes Deployment

### Using Kustomize (Recommended)

```bash
# Development deployment
kubectl apply -k k8s/overlays/dev

# Staging deployment
kubectl apply -k k8s/overlays/staging

# Production deployment
kubectl apply -k k8s/overlays/prod

# Check deployment status
kubectl get pods,svc,ingress -n tas-mcp-prod
```

### Manual Deployment

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: tas-mcp
---
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tas-mcp-config
  namespace: tas-mcp
data:
  config.json: |
    {
      "HTTPPort": 8080,
      "GRPCPort": 50051,
      "LogLevel": "info",
      "forwarding": {
        "enabled": true,
        "workers": 5
      }
    }
---
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tas-mcp
  namespace: tas-mcp
spec:
  replicas: 3
  selector:
    matchLabels:
      app: tas-mcp
  template:
    metadata:
      labels:
        app: tas-mcp
    spec:
      containers:
      - name: tas-mcp
        image: ghcr.io/tributary-ai-services/tas-mcp:v1.0.0
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 50051
          name: grpc
        - containerPort: 8082
          name: health
        env:
        - name: LOG_LEVEL
          value: "info"
        - name: FORWARDING_ENABLED
          value: "true"
        volumeMounts:
        - name: config
          mountPath: /config.json
          subPath: config.json
        livenessProbe:
          httpGet:
            path: /health
            port: 8082
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8082
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
      volumes:
      - name: config
        configMap:
          name: tas-mcp-config
---
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: tas-mcp
  namespace: tas-mcp
spec:
  selector:
    app: tas-mcp
  ports:
  - name: http
    port: 8080
    targetPort: 8080
  - name: grpc
    port: 50051
    targetPort: 50051
  type: ClusterIP
---
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: tas-mcp
  namespace: tas-mcp
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
  - hosts:
    - mcp.yourdomain.com
    secretName: tas-mcp-tls
  rules:
  - host: mcp.yourdomain.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: tas-mcp
            port:
              number: 8080
```

### Horizontal Pod Autoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: tas-mcp-hpa
  namespace: tas-mcp
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: tas-mcp
  minReplicas: 2
  maxReplicas: 20
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
  - type: Pods
    pods:
      metric:
        name: mcp_events_per_second
      target:
        type: AverageValue
        averageValue: "1000"
```

### Pod Disruption Budget

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: tas-mcp-pdb
  namespace: tas-mcp
spec:
  minAvailable: 50%
  selector:
    matchLabels:
      app: tas-mcp
```

## Cloud Deployments

### AWS EKS

```bash
# Install eksctl
curl --silent --location "https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_$(uname -s)_amd64.tar.gz" | tar xz -C /tmp
sudo mv /tmp/eksctl /usr/local/bin

# Create EKS cluster
eksctl create cluster \
  --name tas-mcp-cluster \
  --region us-west-2 \
  --nodegroup-name linux-nodes \
  --node-type m5.large \
  --nodes 3 \
  --nodes-min 1 \
  --nodes-max 10 \
  --managed

# Deploy application
kubectl apply -k k8s/overlays/prod

# Install AWS Load Balancer Controller
kubectl apply -k "github.com/aws/eks-charts/stable/aws-load-balancer-controller//crds"
helm install aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set clusterName=tas-mcp-cluster
```

### Google GKE

```bash
# Create GKE cluster
gcloud container clusters create tas-mcp-cluster \
  --zone us-central1-a \
  --num-nodes 3 \
  --enable-autoscaling \
  --min-nodes 1 \
  --max-nodes 10

# Get credentials
gcloud container clusters get-credentials tas-mcp-cluster --zone us-central1-a

# Deploy application
kubectl apply -k k8s/overlays/prod
```

### Azure AKS

```bash
# Create resource group
az group create --name tas-mcp-rg --location eastus

# Create AKS cluster
az aks create \
  --resource-group tas-mcp-rg \
  --name tas-mcp-cluster \
  --node-count 3 \
  --enable-addons monitoring \
  --generate-ssh-keys

# Get credentials
az aks get-credentials --resource-group tas-mcp-rg --name tas-mcp-cluster

# Deploy application
kubectl apply -k k8s/overlays/prod
```

### Terraform Infrastructure

```hcl
# main.tf
terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.0"
    }
  }
}

resource "kubernetes_namespace" "tas_mcp" {
  metadata {
    name = "tas-mcp"
  }
}

resource "helm_release" "tas_mcp" {
  name       = "tas-mcp"
  repository = "https://tributary-ai-services.github.io/helm-charts"
  chart      = "tas-mcp-server"
  namespace  = kubernetes_namespace.tas_mcp.metadata[0].name

  values = [
    file("${path.module}/values.yaml")
  ]
}
```

## Production Considerations

### Security

1. **Network Policies**
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: tas-mcp-netpol
spec:
  podSelector:
    matchLabels:
      app: tas-mcp
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
    ports:
    - protocol: TCP
      port: 8080
```

2. **Pod Security Standards**
```yaml
apiVersion: v1
kind: Pod
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 65534
    fsGroup: 65534
  containers:
  - name: tas-mcp
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop:
        - ALL
```

3. **Secrets Management**
```bash
# Create secret for API keys
kubectl create secret generic tas-mcp-secrets \
  --from-literal=api-key=your-secret-key \
  --from-literal=jwt-secret=your-jwt-secret
```

### High Availability

1. **Multi-Zone Deployment**
```yaml
spec:
  replicas: 6
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - tas-mcp
              topologyKey: kubernetes.io/zone
```

2. **External Dependencies**
```yaml
# Use managed services
# - AWS RDS for database
# - AWS ElastiCache for Redis
# - AWS MSK for Kafka
```

### Resource Management

```yaml
# VPA (Vertical Pod Autoscaler)
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: tas-mcp-vpa
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: tas-mcp
  updatePolicy:
    updateMode: "Auto"
```

### Backup and Recovery

```bash
# Velero backup
velero backup create tas-mcp-backup \
  --include-namespaces tas-mcp \
  --storage-location default

# Restore from backup
velero restore create --from-backup tas-mcp-backup
```

## Monitoring & Observability

### Prometheus Configuration

```yaml
# prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
- job_name: 'tas-mcp'
  kubernetes_sd_configs:
  - role: pod
  relabel_configs:
  - source_labels: [__meta_kubernetes_pod_label_app]
    action: keep
    regex: tas-mcp
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
    action: keep
    regex: true
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
    action: replace
    target_label: __metrics_path__
    regex: (.+)
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "TAS MCP Server",
    "panels": [
      {
        "title": "Event Throughput",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(mcp_events_total[5m])",
            "legendFormat": "Events/sec"
          }
        ]
      },
      {
        "title": "Forwarding Latency",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, rate(mcp_forwarding_duration_seconds_bucket[5m]))",
            "legendFormat": "95th percentile"
          }
        ]
      }
    ]
  }
}
```

### Log Aggregation

```yaml
# fluentd-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluentd-config
data:
  fluent.conf: |
    <source>
      @type tail
      path /var/log/containers/*tas-mcp*.log
      pos_file /var/log/fluentd-tas-mcp.log.pos
      tag kubernetes.tas-mcp
      format json
    </source>

    <match kubernetes.tas-mcp>
      @type elasticsearch
      host elasticsearch.logging.svc.cluster.local
      port 9200
      index_name tas-mcp
    </match>
```

### Alerting Rules

```yaml
# alerts.yml
groups:
- name: tas-mcp
  rules:
  - alert: HighEventLatency
    expr: histogram_quantile(0.95, rate(mcp_event_processing_duration_seconds_bucket[5m])) > 0.1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High event processing latency"
  
  - alert: ForwardingFailures
    expr: rate(mcp_forwarding_errors_total[5m]) > 0.1
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "High forwarding failure rate"
```

---

This deployment guide provides comprehensive coverage for getting TAS MCP Server running in various environments. Choose the deployment method that best fits your infrastructure and requirements.