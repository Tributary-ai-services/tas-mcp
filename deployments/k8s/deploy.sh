#!/bin/bash

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Version configuration
export TAS_MCP_VERSION=${TAS_MCP_VERSION:-1.1.0}
export GIT_MCP_VERSION=${GIT_MCP_VERSION:-1.0.0}
export BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
export VCS_REF=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo -e "${GREEN}🚀 TAS MCP + Git MCP Kubernetes Deployment${NC}"
echo "==========================================="
echo -e "${YELLOW}TAS MCP Version: ${TAS_MCP_VERSION}${NC}"
echo -e "${YELLOW}Git MCP Version: ${GIT_MCP_VERSION}${NC}"
echo -e "${YELLOW}Build Date: ${BUILD_DATE}${NC}"
echo -e "${YELLOW}VCS Ref: ${VCS_REF}${NC}"
echo ""

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl is not installed. Please install kubectl first.${NC}"
    exit 1
fi

# Check if kustomize is installed
if ! command -v kustomize &> /dev/null; then
    echo -e "${YELLOW}⚠️ kustomize is not installed. Installing...${NC}"
    # Install kustomize
    curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | bash
    sudo mv kustomize /usr/local/bin/
fi

# Check if cluster is accessible
if ! kubectl cluster-info &>/dev/null; then
    echo -e "${RED}❌ Cannot connect to Kubernetes cluster. Please check your kubeconfig.${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Kubernetes cluster is accessible${NC}"

# Function to show usage
show_usage() {
    echo "Usage: $0 [COMMAND]"
    echo ""
    echo "Commands:"
    echo "  deploy      Deploy all resources (default)"
    echo "  delete      Delete all resources"
    echo "  status      Show deployment status"
    echo "  logs        Show logs from pods"
    echo "  test        Run integration tests"
    echo "  build       Build and load Docker images"
    echo "  help        Show this help message"
}

# Parse command line arguments
COMMAND=${1:-deploy}

case $COMMAND in
    "deploy")
        echo -e "${YELLOW}🔨 Building Docker images with version info...${NC}"
        
        # Build TAS MCP image
        echo "Building TAS MCP server v${TAS_MCP_VERSION}..."
        docker build -t tas-mcp/server:${TAS_MCP_VERSION} \
          --build-arg VERSION=${TAS_MCP_VERSION} \
          --build-arg BUILD_DATE=${BUILD_DATE} \
          --build-arg VCS_REF=${VCS_REF} \
          ../../
        
        # Build Git MCP image
        echo "Building Git MCP server v${GIT_MCP_VERSION}..."
        docker build -t tas-mcp/git-mcp-server:${GIT_MCP_VERSION} \
          --build-arg VERSION=${GIT_MCP_VERSION} \
          --build-arg BUILD_DATE=${BUILD_DATE} \
          --build-arg VCS_REF=${VCS_REF} \
          -f ../git-mcp/Dockerfile ../../
        
        # Load images into kind cluster if using kind
        if kubectl config current-context | grep -q "kind"; then
            echo -e "${YELLOW}📦 Loading images into kind cluster...${NC}"
            kind load docker-image tas-mcp/server:${TAS_MCP_VERSION}
            kind load docker-image tas-mcp/git-mcp-server:${GIT_MCP_VERSION}
        fi
        
        echo -e "${YELLOW}🚀 Deploying to Kubernetes...${NC}"
        
        # Apply manifests using kustomize
        kustomize build . | kubectl apply -f -
        
        echo -e "${YELLOW}⏳ Waiting for deployments to be ready...${NC}"
        
        # Wait for deployments
        kubectl wait --for=condition=available --timeout=300s deployment/git-mcp-server -n tas-mcp
        kubectl wait --for=condition=available --timeout=300s deployment/tas-mcp-server -n tas-mcp
        
        # Wait for federation init job
        kubectl wait --for=condition=complete --timeout=300s job/federation-init -n tas-mcp
        
        echo -e "${GREEN}✅ Deployment completed successfully!${NC}"
        echo ""
        echo "🔗 Access Points:"
        echo "  - TAS MCP HTTP API: http://localhost:30080"
        echo "  - TAS MCP gRPC API: localhost:30051"
        echo "  - TAS MCP Health: http://localhost:30082/health"
        echo "  - Git MCP API: http://localhost:30300"
        echo "  - Git MCP Health: http://localhost:30301/health"
        echo ""
        echo "📝 Next steps:"
        echo "  - Check status: $0 status"
        echo "  - View logs: $0 logs"
        echo "  - Run tests: $0 test"
        ;;
        
    "delete")
        echo -e "${YELLOW}🗑️ Deleting all resources...${NC}"
        kustomize build . | kubectl delete -f - --ignore-not-found=true
        echo -e "${GREEN}✅ All resources deleted${NC}"
        ;;
        
    "status")
        echo -e "${YELLOW}📊 Deployment Status:${NC}"
        echo ""
        echo "Namespace:"
        kubectl get namespace tas-mcp 2>/dev/null || echo "  tas-mcp namespace not found"
        echo ""
        echo "Deployments:"
        kubectl get deployments -n tas-mcp 2>/dev/null || echo "  No deployments found"
        echo ""
        echo "Pods:"
        kubectl get pods -n tas-mcp 2>/dev/null || echo "  No pods found"
        echo ""
        echo "Services:"
        kubectl get services -n tas-mcp 2>/dev/null || echo "  No services found"
        echo ""
        echo "Jobs:"
        kubectl get jobs -n tas-mcp 2>/dev/null || echo "  No jobs found"
        echo ""
        echo "PVCs:"
        kubectl get pvc -n tas-mcp 2>/dev/null || echo "  No PVCs found"
        ;;
        
    "logs")
        echo -e "${YELLOW}📜 Showing logs...${NC}"
        echo ""
        echo "=== TAS MCP Server Logs ==="
        kubectl logs -n tas-mcp deployment/tas-mcp-server --tail=50 2>/dev/null || echo "TAS MCP logs not available"
        echo ""
        echo "=== Git MCP Server Logs ==="
        kubectl logs -n tas-mcp deployment/git-mcp-server --tail=50 2>/dev/null || echo "Git MCP logs not available"
        echo ""
        echo "=== Federation Init Job Logs ==="
        kubectl logs -n tas-mcp job/federation-init 2>/dev/null || echo "Federation init logs not available"
        ;;
        
    "test")
        echo -e "${YELLOW}🧪 Running integration tests...${NC}"
        
        # Check if services are accessible
        echo "1️⃣ Testing service accessibility..."
        
        # Port forward in background for testing
        kubectl port-forward -n tas-mcp service/tas-mcp-service 8080:8080 &
        TAS_PF_PID=$!
        kubectl port-forward -n tas-mcp service/git-mcp-service 3001:3001 &
        GIT_PF_PID=$!
        
        # Wait for port forwards
        sleep 5
        
        # Test TAS MCP health
        echo -n "TAS MCP Health: "
        if curl -f -s http://localhost:8080/health > /dev/null; then
            echo -e "${GREEN}✅ Healthy${NC}"
        else
            echo -e "${RED}❌ Unhealthy${NC}"
        fi
        
        # Test Git MCP health
        echo -n "Git MCP Health: "
        if curl -f -s http://localhost:3001/health > /dev/null; then
            echo -e "${GREEN}✅ Healthy${NC}"
        else
            echo -e "${RED}❌ Unhealthy${NC}"
        fi
        
        # Test federation
        echo "2️⃣ Testing federation..."
        curl -s http://localhost:8080/api/v1/federation/servers | grep -q "git-mcp-server" && echo -e "${GREEN}✅ Git MCP server found in federation${NC}" || echo -e "${RED}❌ Git MCP server not found in federation${NC}"
        
        # Cleanup port forwards
        kill $TAS_PF_PID $GIT_PF_PID 2>/dev/null || true
        
        echo -e "${GREEN}🎯 Integration tests completed${NC}"
        ;;
        
    "build")
        echo -e "${YELLOW}🔨 Building Docker images with version info...${NC}"
        
        # Build TAS MCP image
        echo "Building TAS MCP server v${TAS_MCP_VERSION}..."
        docker build -t tas-mcp/server:${TAS_MCP_VERSION} \
          --build-arg VERSION=${TAS_MCP_VERSION} \
          --build-arg BUILD_DATE=${BUILD_DATE} \
          --build-arg VCS_REF=${VCS_REF} \
          ../../
        
        # Build Git MCP image
        echo "Building Git MCP server v${GIT_MCP_VERSION}..."
        docker build -t tas-mcp/git-mcp-server:${GIT_MCP_VERSION} \
          --build-arg VERSION=${GIT_MCP_VERSION} \
          --build-arg BUILD_DATE=${BUILD_DATE} \
          --build-arg VCS_REF=${VCS_REF} \
          -f ../git-mcp/Dockerfile ../../
        
        # Load into kind if using kind
        if kubectl config current-context | grep -q "kind"; then
            echo -e "${YELLOW}📦 Loading images into kind cluster...${NC}"
            kind load docker-image tas-mcp/server:${TAS_MCP_VERSION}
            kind load docker-image tas-mcp/git-mcp-server:${GIT_MCP_VERSION}
        fi
        
        echo -e "${GREEN}✅ Images built and loaded with versions${NC}"
        echo "  - tas-mcp/server:${TAS_MCP_VERSION}"
        echo "  - tas-mcp/git-mcp-server:${GIT_MCP_VERSION}"
        ;;
        
    "help")
        show_usage
        ;;
        
    *)
        echo -e "${RED}❌ Unknown command: $COMMAND${NC}"
        echo ""
        show_usage
        exit 1
        ;;
esac