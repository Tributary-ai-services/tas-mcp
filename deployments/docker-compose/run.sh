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

echo -e "${GREEN}🚀 TAS MCP Docker Compose Orchestration${NC}"
echo "======================================="
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
mkdir -p ../../examples/repositories
mkdir -p ../../config
mkdir -p ../../logs
mkdir -p ../federation
mkdir -p ../test

# Create example repository if it doesn't exist
if [ ! -d "../../examples/repositories/.git" ]; then
    echo -e "${YELLOW}📝 Creating example Git repository...${NC}"
    cd ../../examples/repositories
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
    
    cd ../../deployments/docker-compose
    echo -e "${GREEN}✅ Example repository created${NC}"
fi

# Function to show usage
show_usage() {
    echo "Usage: $0 [COMMAND] [SERVICE]"
    echo ""
    echo "Commands:"
    echo "  up [SERVICE]      Start service(s) (default: full-stack)"
    echo "  down [SERVICE]    Stop service(s)"
    echo "  test              Run integration tests (full-stack only)"
    echo "  build [SERVICE]   Build service(s)"
    echo "  logs [SERVICE]    Show logs from service(s)"
    echo "  status [SERVICE]  Show status of service(s)"
    echo "  clean [SERVICE]   Stop service(s) and remove volumes"
    echo "  help              Show this help message"
    echo ""
    echo "Services:"
    echo "  full-stack        Complete TAS MCP + Git MCP stack (default)"
    echo "  tas-mcp           TAS MCP federation server only"
    echo "  git-mcp           Git MCP server only"
    echo ""
    echo "Examples:"
    echo "  $0 up                    # Start full stack"
    echo "  $0 up tas-mcp           # Start only TAS MCP server"
    echo "  $0 build git-mcp        # Build only Git MCP server"
    echo "  $0 logs full-stack      # Show all logs"
}

# Parse command line arguments
COMMAND=${1:-up}
SERVICE=${2:-full-stack}

# Determine compose file based on service
case $SERVICE in
    "full-stack")
        COMPOSE_FILE="full-stack.yml"
        ;;
    "tas-mcp")
        COMPOSE_FILE="docker-compose.yml"
        ;;
    "git-mcp")
        COMPOSE_FILE="git-mcp/docker-compose.yml"
        ;;
    *)
        echo -e "${RED}❌ Unknown service: $SERVICE${NC}"
        echo ""
        show_usage
        exit 1
        ;;
esac

case $COMMAND in
    "up")
        echo -e "${YELLOW}🔄 Starting $SERVICE services...${NC}"
        
        # Create network if it doesn't exist
        docker network create mcp-network 2>/dev/null || true
        
        TAS_MCP_VERSION=$TAS_MCP_VERSION \
        GIT_MCP_VERSION=$GIT_MCP_VERSION \
        BUILD_DATE=$BUILD_DATE \
        VCS_REF=$VCS_REF \
        $COMPOSE_CMD -f $COMPOSE_FILE up -d
        
        echo -e "${YELLOW}⏳ Waiting for services to be ready...${NC}"
        sleep 10
        
        echo -e "${GREEN}✅ Services started successfully!${NC}"
        echo ""
        echo "🔗 Access Points:"
        if [[ "$SERVICE" == "full-stack" || "$SERVICE" == "tas-mcp" ]]; then
            echo "  - TAS MCP API: http://localhost:8080"
            echo "  - TAS MCP Health: http://localhost:8082/health"
            echo "  - Federation API: http://localhost:8080/api/v1/federation"
        fi
        if [[ "$SERVICE" == "full-stack" || "$SERVICE" == "git-mcp" ]]; then
            echo "  - Git MCP Server: http://localhost:3000"
            echo "  - Git MCP Health: http://localhost:3001/health"
        fi
        echo ""
        echo "📝 Next steps:"
        echo "  - Run tests: $0 test"
        echo "  - View logs: $0 logs $SERVICE"
        echo "  - Check status: $0 status $SERVICE"
        ;;
        
    "down")
        echo -e "${YELLOW}🛑 Stopping $SERVICE services...${NC}"
        $COMPOSE_CMD -f $COMPOSE_FILE down
        echo -e "${GREEN}✅ Services stopped${NC}"
        ;;
        
    "test")
        if [[ "$SERVICE" != "full-stack" ]]; then
            echo -e "${RED}❌ Tests can only be run on full-stack deployment${NC}"
            exit 1
        fi
        echo -e "${YELLOW}🧪 Running integration tests...${NC}"
        $COMPOSE_CMD -f $COMPOSE_FILE --profile test up test-client
        ;;
        
    "build")
        echo -e "${YELLOW}🔨 Building $SERVICE images with version info...${NC}"
        
        TAS_MCP_VERSION=$TAS_MCP_VERSION \
        GIT_MCP_VERSION=$GIT_MCP_VERSION \
        BUILD_DATE=$BUILD_DATE \
        VCS_REF=$VCS_REF \
        $COMPOSE_CMD -f $COMPOSE_FILE build \
          --build-arg VERSION=${TAS_MCP_VERSION} \
          --build-arg BUILD_DATE=${BUILD_DATE} \
          --build-arg VCS_REF=${VCS_REF}
          
        echo -e "${GREEN}✅ Images built successfully${NC}"
        ;;
        
    "logs")
        echo -e "${YELLOW}📜 Showing logs from $SERVICE services...${NC}"
        $COMPOSE_CMD -f $COMPOSE_FILE logs -f
        ;;
        
    "status")
        echo -e "${YELLOW}📊 $SERVICE service status:${NC}"
        $COMPOSE_CMD -f $COMPOSE_FILE ps
        echo ""
        echo -e "${YELLOW}🏥 Health checks:${NC}"
        if [[ "$SERVICE" == "full-stack" || "$SERVICE" == "tas-mcp" ]]; then
            echo -n "TAS MCP: "
            curl -f -s http://localhost:8082/health > /dev/null && echo -e "${GREEN}✅ Healthy${NC}" || echo -e "${RED}❌ Unhealthy${NC}"
        fi
        if [[ "$SERVICE" == "full-stack" || "$SERVICE" == "git-mcp" ]]; then
            echo -n "Git MCP: "
            curl -f -s http://localhost:3001/health > /dev/null && echo -e "${GREEN}✅ Healthy${NC}" || echo -e "${RED}❌ Unhealthy${NC}"
        fi
        ;;
        
    "clean")
        echo -e "${YELLOW}🧹 Cleaning up $SERVICE services and volumes...${NC}"
        $COMPOSE_CMD -f $COMPOSE_FILE down -v
        if [[ "$SERVICE" == "full-stack" ]]; then
            docker system prune -f
        fi
        echo -e "${GREEN}✅ Cleanup completed${NC}"
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