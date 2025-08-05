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

echo -e "${GREEN}🚀 TAS MCP + Git MCP Docker Compose Deployment${NC}"
echo "=============================================="
echo -e "${YELLOW}TAS MCP Version: ${TAS_MCP_VERSION}${NC}"
echo -e "${YELLOW}Git MCP Version: ${GIT_MCP_VERSION}${NC}"
echo -e "${YELLOW}Build Date: ${BUILD_DATE}${NC}"
echo -e "${YELLOW}VCS Ref: ${VCS_REF}${NC}"
echo ""

# Check if Docker and Docker Compose are installed
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed. Please install Docker first.${NC}"
    exit 1
fi

if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo -e "${RED}❌ Docker Compose is not installed. Please install Docker Compose first.${NC}"
    exit 1
fi

# Use docker compose if available, otherwise docker-compose
COMPOSE_CMD="docker-compose"
if docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
fi

echo -e "${YELLOW}Using: $COMPOSE_CMD${NC}"

# Create necessary directories
echo -e "${YELLOW}📁 Creating necessary directories...${NC}"
mkdir -p examples/repositories
mkdir -p config
mkdir -p logs
mkdir -p deployments/federation
mkdir -p deployments/test

# Create example repository if it doesn't exist
if [ ! -d "examples/repositories/.git" ]; then
    echo -e "${YELLOW}📝 Creating example Git repository...${NC}"
    cd examples/repositories
    git init
    git config user.name "Docker Test User"
    git config user.email "test@tributary-ai.services"
    
    echo "# Test Repository for Git MCP" > README.md
    echo "This repository is used for testing Git MCP operations." >> README.md
    echo "" >> README.md
    echo "## Files" >> README.md
    echo "- README.md: This file" >> README.md
    echo "- example.py: Python example" >> README.md
    echo "- example.go: Go example" >> README.md
    
    echo "print('Hello from Git MCP!')" > example.py
    echo "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from Git MCP!\")\n}" > example.go
    
    git add .
    git commit -m "Initial commit: Add example files"
    
    # Create a feature branch
    git checkout -b feature/testing
    echo "# Testing Branch" > testing.md
    echo "This file is for testing branch operations." >> testing.md
    git add testing.md
    git commit -m "Add testing branch file"
    git checkout main
    
    cd ../../
    echo -e "${GREEN}✅ Example repository created${NC}"
fi

# Function to show usage
show_usage() {
    echo "Usage: $0 [COMMAND]"
    echo ""
    echo "Commands:"
    echo "  up        Start all services (default)"
    echo "  down      Stop all services"
    echo "  test      Run integration tests"
    echo "  logs      Show logs from all services"
    echo "  status    Show status of all services"
    echo "  clean     Stop services and remove volumes"
    echo "  build     Build images without starting services"
    echo "  help      Show this help message"
}

# Parse command line arguments
COMMAND=${1:-up}

case $COMMAND in
    "up")
        echo -e "${YELLOW}🔄 Starting TAS MCP and Git MCP services...${NC}"
        echo -e "${YELLOW}ℹ️  Note: Consider using the new modular approach in deployments/docker-compose/run.sh${NC}"
        $COMPOSE_CMD -f docker-compose.git-mcp.yml up -d
        
        echo -e "${YELLOW}⏳ Waiting for services to be ready...${NC}"
        sleep 10
        
        echo -e "${GREEN}✅ Services started successfully!${NC}"
        echo ""
        echo "🔗 Access Points:"
        echo "  - TAS MCP API: http://localhost:8080"
        echo "  - TAS MCP Health: http://localhost:8082/health"
        echo "  - Git MCP Server: http://localhost:3000"
        echo "  - Git MCP Health: http://localhost:3001/health"
        echo "  - Federation API: http://localhost:8080/api/v1/federation"
        echo ""
        echo "📝 Next steps:"
        echo "  - Run tests: $0 test"
        echo "  - View logs: $0 logs"
        echo "  - Check status: $0 status"
        ;;
        
    "down")
        echo -e "${YELLOW}🛑 Stopping all services...${NC}"
        $COMPOSE_CMD -f docker-compose.git-mcp.yml down
        echo -e "${GREEN}✅ Services stopped${NC}"
        ;;
        
    "test")
        echo -e "${YELLOW}🧪 Running integration tests...${NC}"
        $COMPOSE_CMD -f docker-compose.git-mcp.yml --profile test up test-client
        ;;
        
    "logs")
        echo -e "${YELLOW}📜 Showing logs from all services...${NC}"
        $COMPOSE_CMD -f docker-compose.git-mcp.yml logs -f
        ;;
        
    "status")
        echo -e "${YELLOW}📊 Service status:${NC}"
        $COMPOSE_CMD -f docker-compose.git-mcp.yml ps
        echo ""
        echo -e "${YELLOW}🏥 Health checks:${NC}"
        echo -n "TAS MCP: "
        curl -f -s http://localhost:8082/health > /dev/null && echo -e "${GREEN}✅ Healthy${NC}" || echo -e "${RED}❌ Unhealthy${NC}"
        echo -n "Git MCP: "
        curl -f -s http://localhost:3001/health > /dev/null && echo -e "${GREEN}✅ Healthy${NC}" || echo -e "${RED}❌ Unhealthy${NC}"
        ;;
        
    "clean")
        echo -e "${YELLOW}🧹 Cleaning up services and volumes...${NC}"
        $COMPOSE_CMD -f docker-compose.git-mcp.yml down -v
        docker system prune -f
        echo -e "${GREEN}✅ Cleanup completed${NC}"
        ;;
        
    "build")
        echo -e "${YELLOW}🔨 Building Docker images with version info...${NC}"
        echo "Building TAS MCP Server v${TAS_MCP_VERSION}..."
        echo "Building Git MCP Server v${GIT_MCP_VERSION}..."
        
        # Build with version arguments
        TAS_MCP_VERSION=$TAS_MCP_VERSION \
        GIT_MCP_VERSION=$GIT_MCP_VERSION \
        BUILD_DATE=$BUILD_DATE \
        VCS_REF=$VCS_REF \
        $COMPOSE_CMD -f docker-compose.git-mcp.yml build \
          --build-arg VERSION=${TAS_MCP_VERSION} \
          --build-arg BUILD_DATE=${BUILD_DATE} \
          --build-arg VCS_REF=${VCS_REF}
          
        echo -e "${GREEN}✅ Images built successfully with versions${NC}"
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