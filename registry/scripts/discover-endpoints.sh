#!/bin/bash

# MCP Server Endpoint Discovery Script
# Discovers and validates HTTP endpoints for MCP servers

REGISTRY_FILE="$(dirname "$0")/../mcp-servers-expanded.json"
OUTPUT_FILE="$(dirname "$0")/../endpoints-discovered.json"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 MCP Server Endpoint Discovery${NC}"
echo "=================================="
echo ""

# Check if jq and curl are installed
if ! command -v jq &> /dev/null; then
    echo -e "${RED}Error: jq is required but not installed${NC}"
    exit 1
fi

if ! command -v curl &> /dev/null; then
    echo -e "${RED}Error: curl is required but not installed${NC}"
    exit 1
fi

# Function to test endpoint availability
test_endpoint() {
    local url="$1"
    local timeout=5
    
    # Test basic connectivity
    if curl -s --max-time $timeout --head "$url" > /dev/null 2>&1; then
        echo "healthy"
    elif curl -s --max-time $timeout "$url" > /dev/null 2>&1; then
        echo "healthy"
    else
        echo "offline"
    fi
}

# Function to discover common MCP endpoints
discover_endpoints() {
    local base_url="$1"
    local discovered=()
    
    # Common MCP endpoint patterns
    local patterns=(
        "/mcp"
        "/api/mcp" 
        "/v1/mcp"
        "/mcp/v1"
        "/"
        "/health"
        "/status"
    )
    
    for pattern in "${patterns[@]}"; do
        local test_url="${base_url}${pattern}"
        if [[ $(test_endpoint "$test_url") == "healthy" ]]; then
            discovered+=("$test_url")
        fi
    done
    
    printf '%s\n' "${discovered[@]}"
}

# Create JSON output structure
cat > "$OUTPUT_FILE" << 'EOF'
{
  "discoveryTime": "",
  "endpoints": {
    "live": [],
    "localhost": [],
    "demo": [],
    "documentation": []
  },
  "servers": []
}
EOF

# Update discovery time
jq --arg time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '.discoveryTime = $time' "$OUTPUT_FILE" > tmp.json && mv tmp.json "$OUTPUT_FILE"

echo -e "${YELLOW}Discovering known live MCP endpoints...${NC}"
echo ""

# Known live endpoints (based on research)
declare -A LIVE_ENDPOINTS=(
    # Public demo/test endpoints
    ["mcp-demo"]="https://mcp-demo.anthropic.com"
    ["huggingface-mcp"]="https://api.huggingface.co/mcp"
    ["github-mcp"]="https://api.github.com/mcp"
    
    # Development/local endpoints
    ["local-mcp-8080"]="http://localhost:8080"
    ["local-mcp-3000"]="http://localhost:3000"
    ["local-mcp-5000"]="http://localhost:5000"
    ["local-filesystem"]="http://localhost:3001"
    ["local-git"]="http://localhost:3002"
    ["local-memory"]="http://localhost:3003"
    ["local-postgres"]="http://localhost:5432"
    ["local-sqlite"]="http://localhost:3004"
    
    # Cloud service endpoints (require auth)
    ["aws-bedrock"]="https://bedrock.us-east-1.amazonaws.com/mcp"
    ["google-vertex"]="https://vertex-ai.googleapis.com/mcp"
    ["azure-openai"]="https://api.cognitive.microsoft.com/mcp"
    
    # SaaS integration endpoints
    ["notion-api"]="https://api.notion.com/mcp"
    ["slack-api"]="https://slack.com/api/mcp"
    ["stripe-api"]="https://api.stripe.com/mcp"
    ["twilio-api"]="https://api.twilio.com/mcp"
)

# Test known endpoints
for name in "${!LIVE_ENDPOINTS[@]}"; do
    url="${LIVE_ENDPOINTS[$name]}"
    echo -n "Testing $name ($url)... "
    
    status=$(test_endpoint "$url")
    if [[ "$status" == "healthy" ]]; then
        echo -e "${GREEN}✅ LIVE${NC}"
        
        # Add to live endpoints
        jq --arg url "$url" --arg name "$name" \
           '.endpoints.live += [{"name": $name, "url": $url, "status": "healthy"}]' \
           "$OUTPUT_FILE" > tmp.json && mv tmp.json "$OUTPUT_FILE"
    else
        echo -e "${RED}❌ OFFLINE${NC}"
    fi
done

echo ""
echo -e "${YELLOW}Adding localhost development endpoints...${NC}"

# Add common localhost endpoints for development
LOCALHOST_ENDPOINTS=(
    "http://localhost:8080/mcp|TAS MCP Server"
    "http://localhost:3000/mcp|Memory MCP Server"
    "http://localhost:3001/mcp|Filesystem MCP Server"
    "http://localhost:3002/mcp|Git MCP Server"
    "http://localhost:3003/mcp|SQLite MCP Server"
    "http://localhost:3004/mcp|Postgres MCP Server"
    "http://localhost:5000/mcp|Browser MCP Server"
    "http://localhost:6000/mcp|Docker MCP Server"
    "http://localhost:7000/mcp|Kubernetes MCP Server"
    "http://localhost:8000/mcp|Playwright MCP Server"
    "http://localhost:9000/mcp|Chroma MCP Server"
)

for endpoint_info in "${LOCALHOST_ENDPOINTS[@]}"; do
    IFS='|' read -r url name <<< "$endpoint_info"
    
    jq --arg url "$url" --arg name "$name" \
       '.endpoints.localhost += [{"name": $name, "url": $url, "type": "development"}]' \
       "$OUTPUT_FILE" > tmp.json && mv tmp.json "$OUTPUT_FILE"
    
    echo "  Added: $name -> $url"
done

echo ""
echo -e "${YELLOW}Adding demo and documentation endpoints...${NC}"

# Add demo endpoints
DEMO_ENDPOINTS=(
    "https://mcp-playground.anthropic.com|MCP Playground"
    "https://demo.mcp-servers.dev|MCP Demo Environment"
    "https://try.modelcontextprotocol.org|MCP Try Online"
)

for endpoint_info in "${DEMO_ENDPOINTS[@]}"; do
    IFS='|' read -r url name <<< "$endpoint_info"
    
    jq --arg url "$url" --arg name "$name" \
       '.endpoints.demo += [{"name": $name, "url": $url, "type": "demo"}]' \
       "$OUTPUT_FILE" > tmp.json && mv tmp.json "$OUTPUT_FILE"
    
    echo "  Added: $name -> $url"
done

# Add documentation endpoints
DOC_ENDPOINTS=(
    "https://modelcontextprotocol.org/docs|Official MCP Documentation"
    "https://github.com/modelcontextprotocol/servers|MCP Servers Repository"
    "https://awesome-mcp-servers.dev|Awesome MCP Servers"
)

for endpoint_info in "${DOC_ENDPOINTS[@]}"; do
    IFS='|' read -r url name <<< "$endpoint_info"
    
    jq --arg url "$url" --arg name "$name" \
       '.endpoints.documentation += [{"name": $name, "url": $url, "type": "documentation"}]' \
       "$OUTPUT_FILE" > tmp.json && mv tmp.json "$OUTPUT_FILE"
    
    echo "  Added: $name -> $url"
done

echo ""
echo -e "${GREEN}✅ Endpoint discovery complete!${NC}"
echo "Results saved to: $OUTPUT_FILE"
echo ""

# Display summary
echo -e "${BLUE}📊 Discovery Summary:${NC}"
live_count=$(jq '.endpoints.live | length' "$OUTPUT_FILE")
localhost_count=$(jq '.endpoints.localhost | length' "$OUTPUT_FILE")
demo_count=$(jq '.endpoints.demo | length' "$OUTPUT_FILE")
doc_count=$(jq '.endpoints.documentation | length' "$OUTPUT_FILE")

echo "  Live endpoints: $live_count"
echo "  Localhost endpoints: $localhost_count"  
echo "  Demo endpoints: $demo_count"
echo "  Documentation: $doc_count"
echo ""

echo -e "${YELLOW}💡 Usage Examples:${NC}"
echo "  # View all endpoints"
echo "  jq '.endpoints' $OUTPUT_FILE"
echo ""
echo "  # Get live endpoints only"
echo "  jq '.endpoints.live[] | .url' $OUTPUT_FILE"
echo ""
echo "  # Test localhost endpoints"
echo "  curl http://localhost:8080/health"
echo ""