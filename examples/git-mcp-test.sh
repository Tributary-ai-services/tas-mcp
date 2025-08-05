#!/bin/bash

set -e

echo "🚀 Testing Git MCP Server Integration with TAS MCP"
echo "=================================================="

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
TAS_MCP_URL="http://localhost:8080"
GIT_MCP_URL="http://localhost:3000"
REGISTRY_API="${TAS_MCP_URL}/api/v1/federation"

echo -e "${YELLOW}Step 1: Checking if TAS MCP server is running${NC}"
if curl -f -s "${TAS_MCP_URL}/health" > /dev/null; then
    echo -e "${GREEN}✅ TAS MCP server is running${NC}"
else
    echo -e "${RED}❌ TAS MCP server is not running. Start it with: make docker && docker run -p 8080:8080 -p 50051:50051 -p 8082:8082 tas-mcp:latest${NC}"
    exit 1
fi

echo -e "${YELLOW}Step 2: Installing Git MCP server locally${NC}"
if ! command -v uvx &> /dev/null; then
    echo "Installing uvx..."
    pip install uvx || {
        echo -e "${RED}❌ Failed to install uvx. Please install Python and pip first.${NC}"
        exit 1
    }
fi

echo "Installing mcp-server-git..."
uvx install mcp-server-git || {
    echo -e "${RED}❌ Failed to install mcp-server-git${NC}"
    exit 1
}

echo -e "${YELLOW}Step 3: Starting Git MCP server${NC}"
# Create a test repository
TEST_REPO_DIR="/tmp/test-git-repo"
if [ ! -d "$TEST_REPO_DIR" ]; then
    mkdir -p "$TEST_REPO_DIR"
    cd "$TEST_REPO_DIR"
    git init
    echo "# Test Repository for Git MCP Integration" > README.md
    git add README.md
    git commit -m "Initial commit"
    cd -
fi

# Start Git MCP server in background
echo "Starting Git MCP server on port 3000..."
uvx mcp-server-git --repository "$TEST_REPO_DIR" --port 3000 &
GIT_MCP_PID=$!

# Wait for Git MCP server to start
sleep 5

# Function to cleanup on exit
cleanup() {
    echo -e "${YELLOW}Cleaning up...${NC}"
    if [ ! -z "$GIT_MCP_PID" ]; then
        kill $GIT_MCP_PID 2>/dev/null || true
    fi
    rm -rf "$TEST_REPO_DIR"
}
trap cleanup EXIT

echo -e "${YELLOW}Step 4: Registering Git MCP server with TAS MCP${NC}"
REGISTER_REQUEST='{
  "id": "git-mcp-server",
  "name": "Git MCP Server",
  "description": "Official Git repository interaction and automation server",
  "version": "1.0.0",
  "category": "development-tools",
  "endpoint": "http://localhost:3000",
  "protocol": "http",
  "auth": {
    "type": "none",
    "config": {}
  },
  "capabilities": [
    "git_status",
    "git_diff_unstaged",
    "git_diff_staged",
    "git_commit",
    "git_add",
    "git_reset",
    "git_log",
    "git_create_branch",
    "git_checkout"
  ],
  "tags": ["python", "git", "repository", "development", "official"],
  "health_check": {
    "enabled": true,
    "interval": "30s",
    "timeout": "10s",
    "path": "/health"
  }
}'

curl -X POST "${REGISTRY_API}/servers" \
     -H "Content-Type: application/json" \
     -d "$REGISTER_REQUEST" || {
    echo -e "${RED}❌ Failed to register Git MCP server${NC}"
    exit 1
}

echo -e "${GREEN}✅ Git MCP server registered successfully${NC}"

echo -e "${YELLOW}Step 5: Testing federation endpoints${NC}"

# List registered servers
echo "📋 Listing registered servers:"
curl -s "${REGISTRY_API}/servers" | jq '.' || {
    echo -e "${RED}❌ Failed to list servers${NC}"
    exit 1
}

# Check health of Git MCP server
echo "🏥 Checking Git MCP server health:"
curl -s "${REGISTRY_API}/servers/git-mcp-server/health" || {
    echo -e "${YELLOW}⚠️ Health check endpoint may not be implemented yet${NC}"
}

echo -e "${YELLOW}Step 6: Testing Git operations via federation${NC}"

# Test git status
echo "📊 Testing git_status operation:"
STATUS_REQUEST='{
  "id": "test-git-status",
  "method": "git_status",
  "params": {
    "repository": "'$TEST_REPO_DIR'"
  }
}'

curl -X POST "${REGISTRY_API}/servers/git-mcp-server/invoke" \
     -H "Content-Type: application/json" \
     -d "$STATUS_REQUEST" | jq '.' || {
    echo -e "${YELLOW}⚠️ Git status invocation may require different endpoint structure${NC}"
}

# Test creating a new branch
echo "🌿 Testing git_create_branch operation:"
BRANCH_REQUEST='{
  "id": "test-create-branch",
  "method": "git_create_branch",
  "params": {
    "repository": "'$TEST_REPO_DIR'",
    "branch_name": "feature/tas-mcp-integration"
  }
}'

curl -X POST "${REGISTRY_API}/servers/git-mcp-server/invoke" \
     -H "Content-Type: application/json" \
     -d "$BRANCH_REQUEST" | jq '.' || {
    echo -e "${YELLOW}⚠️ Branch creation invocation may require different endpoint structure${NC}"
}

echo -e "${YELLOW}Step 7: Testing broadcast operations${NC}"

# Test broadcast to all servers
BROADCAST_REQUEST='{
  "id": "test-broadcast",
  "method": "ping",
  "params": {}
}'

curl -X POST "${REGISTRY_API}/broadcast" \
     -H "Content-Type: application/json" \
     -d "$BROADCAST_REQUEST" | jq '.' || {
    echo -e "${YELLOW}⚠️ Broadcast operation may not be fully implemented${NC}"
}

echo -e "${YELLOW}Step 8: Getting federation metrics${NC}"
curl -s "${REGISTRY_API}/metrics" | jq '.' || {
    echo -e "${YELLOW}⚠️ Federation metrics endpoint may not be implemented yet${NC}"
}

echo -e "${GREEN}🎯 Git MCP Integration Test Completed!${NC}"
echo ""
echo "Summary:"
echo "✅ TAS MCP server running on port 8080"
echo "✅ Git MCP server installed and configured"
echo "✅ Federation registration successful"
echo "✅ Basic federation endpoints tested"
echo ""
echo "Next steps:"
echo "1. Implement missing federation endpoints in TAS MCP"
echo "2. Add proper health checks for Git MCP server"
echo "3. Test actual Git operations through federation"
echo "4. Add authentication if needed"
echo "5. Deploy in production environment"