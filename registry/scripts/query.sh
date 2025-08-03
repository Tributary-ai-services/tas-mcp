#!/bin/bash

# MCP Registry Query Utilities
# Provides common queries for the MCP server registry

REGISTRY_FILE="$(dirname "$0")/../mcp-servers.json"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo -e "${RED}Error: jq is required but not installed${NC}"
    echo "Install with: sudo apt-get install jq (Ubuntu/Debian) or brew install jq (macOS)"
    exit 1
fi

# Check if registry file exists
if [[ ! -f "$REGISTRY_FILE" ]]; then
    echo -e "${RED}Error: Registry file not found at $REGISTRY_FILE${NC}"
    exit 1
fi

# Function to display help
show_help() {
    echo -e "${BLUE}MCP Registry Query Tool${NC}"
    echo ""
    echo "Usage: $0 [COMMAND] [OPTIONS]"
    echo ""
    echo "Commands:"
    echo "  list-all              List all servers with basic info"
    echo "  list-free             List all free servers"
    echo "  list-category CAT     List servers in category"
    echo "  list-transport TRANS  List servers supporting transport"
    echo "  show-stats            Show registry statistics"
    echo "  health-check          Show server health status"
    echo "  endpoints             List all available endpoints"
    echo "  docker-images         List all Docker images"
    echo "  kubernetes            List Kubernetes-ready servers"
    echo "  search TERM           Search servers by name/description"
    echo "  validate              Validate registry format"
    echo ""
    echo "Categories: ai-model, event-streaming, workflow-orchestration,"
    echo "           knowledge-base, database, monitoring, communication"
    echo ""
    echo "Transports: http, grpc, websocket, tcp, udp"
}

# List all servers
list_all() {
    echo -e "${BLUE}📋 All MCP Servers${NC}"
    jq -r '.servers[] | "\(.name) (\(.id)) - \(.category) - \(.access.type)"' "$REGISTRY_FILE" | \
    while IFS= read -r line; do
        echo "  • $line"
    done
}

# List free servers
list_free() {
    echo -e "${GREEN}🆓 Free MCP Servers${NC}"
    jq -r '.servers[] | select(.access.type == "free") | "  • \(.name) - \(.endpoints.http[0] // .endpoints.grpc[0] // "No public endpoint")"' "$REGISTRY_FILE"
}

# List servers by category
list_category() {
    local category="$1"
    if [[ -z "$category" ]]; then
        echo -e "${RED}Error: Category required${NC}"
        echo "Available categories:"
        jq -r '.servers[].category' "$REGISTRY_FILE" | sort -u | sed 's/^/  /'
        return 1
    fi
    
    echo -e "${BLUE}📂 Servers in category: $category${NC}"
    jq -r --arg cat "$category" '.servers[] | select(.category == $cat) | "  • \(.name) - \(.description)"' "$REGISTRY_FILE"
}

# List servers by transport
list_transport() {
    local transport="$1"
    if [[ -z "$transport" ]]; then
        echo -e "${RED}Error: Transport required${NC}"
        echo "Available transports: http, grpc, websocket, tcp, udp"
        return 1
    fi
    
    echo -e "${BLUE}🔌 Servers supporting $transport${NC}"
    jq -r --arg transport "$transport" '.servers[] | select(.protocols.transport[] == $transport) | "  • \(.name) - \(.endpoints[$transport][0] // "No endpoint listed")"' "$REGISTRY_FILE"
}

# Show registry statistics
show_stats() {
    echo -e "${BLUE}📊 Registry Statistics${NC}"
    echo ""
    
    local total=$(jq '.servers | length' "$REGISTRY_FILE")
    echo -e "${GREEN}Total Servers: $total${NC}"
    echo ""
    
    echo -e "${YELLOW}By Category:${NC}"
    jq -r '.servers | group_by(.category) | .[] | "\(.[0].category): \(length)"' "$REGISTRY_FILE" | sort | sed 's/^/  /'
    echo ""
    
    echo -e "${YELLOW}By Access Type:${NC}"
    jq -r '.servers | group_by(.access.type) | .[] | "\(.[0].access.type): \(length)"' "$REGISTRY_FILE" | sort | sed 's/^/  /'
    echo ""
    
    echo -e "${YELLOW}By Transport:${NC}"
    jq -r '.servers[].protocols.transport[]' "$REGISTRY_FILE" | sort | uniq -c | sort -nr | sed 's/^/  /'
    echo ""
    
    local with_k8s=$(jq '[.servers[] | select(.deployment.kubernetes == true)] | length' "$REGISTRY_FILE")
    local with_docker=$(jq '[.servers[] | select(.deployment.docker)] | length' "$REGISTRY_FILE")
    echo -e "${YELLOW}Deployment Support:${NC}"
    echo "  Kubernetes: $with_k8s"
    echo "  Docker: $with_docker"
}

# Health check
health_check() {
    echo -e "${BLUE}🏥 Server Health Status${NC}"
    jq -r '.servers[] | select(.status) | "\(.name): \(.status.health // "unknown") (\(.status.uptime // "N/A"))"' "$REGISTRY_FILE" | \
    while IFS= read -r line; do
        if [[ $line == *"healthy"* ]]; then
            echo -e "  ${GREEN}✅ $line${NC}"
        elif [[ $line == *"degraded"* ]]; then
            echo -e "  ${YELLOW}⚠️  $line${NC}"
        elif [[ $line == *"offline"* ]]; then
            echo -e "  ${RED}❌ $line${NC}"
        else
            echo "  ❓ $line"
        fi
    done
}

# List endpoints
endpoints() {
    echo -e "${BLUE}🌐 Available Endpoints${NC}"
    echo ""
    echo -e "${YELLOW}HTTP Endpoints:${NC}"
    jq -r '.servers[] | select(.endpoints.http) | "  \(.name): \(.endpoints.http[0])"' "$REGISTRY_FILE"
    echo ""
    echo -e "${YELLOW}gRPC Endpoints:${NC}"
    jq -r '.servers[] | select(.endpoints.grpc) | "  \(.name): \(.endpoints.grpc[0])"' "$REGISTRY_FILE"
    echo ""
    echo -e "${YELLOW}WebSocket Endpoints:${NC}"
    jq -r '.servers[] | select(.endpoints.websocket) | "  \(.name): \(.endpoints.websocket[0])"' "$REGISTRY_FILE"
}

# List Docker images
docker_images() {
    echo -e "${BLUE}🐳 Docker Images${NC}"
    jq -r '.servers[] | select(.deployment.docker) | "  \(.name): \(.deployment.docker)"' "$REGISTRY_FILE"
}

# List Kubernetes-ready servers
kubernetes() {
    echo -e "${BLUE}☸️  Kubernetes-Ready Servers${NC}"
    jq -r '.servers[] | select(.deployment.kubernetes == true) | "  • \(.name) - \(.deployment.helm // "No Helm chart")"' "$REGISTRY_FILE"
}

# Search servers
search() {
    local term="$1"
    if [[ -z "$term" ]]; then
        echo -e "${RED}Error: Search term required${NC}"
        return 1
    fi
    
    echo -e "${BLUE}🔍 Search results for: $term${NC}"
    jq -r --arg term "$term" '.servers[] | select(.name | test($term; "i")) or select(.description | test($term; "i")) | "  • \(.name) - \(.description)"' "$REGISTRY_FILE"
}

# Validate registry
validate() {
    echo -e "${BLUE}✅ Validating Registry${NC}"
    if command -v node &> /dev/null && [[ -f "$(dirname "$0")/validate.js" ]]; then
        node "$(dirname "$0")/validate.js"
    else
        echo -e "${YELLOW}Basic JSON validation:${NC}"
        if jq empty "$REGISTRY_FILE" 2>/dev/null; then
            echo -e "${GREEN}✅ Valid JSON format${NC}"
        else
            echo -e "${RED}❌ Invalid JSON format${NC}"
            return 1
        fi
    fi
}

# Main command dispatcher
case "$1" in
    "list-all"|"all")
        list_all
        ;;
    "list-free"|"free")
        list_free
        ;;
    "list-category"|"category")
        list_category "$2"
        ;;
    "list-transport"|"transport")
        list_transport "$2"
        ;;
    "show-stats"|"stats")
        show_stats
        ;;
    "health-check"|"health")
        health_check
        ;;
    "endpoints")
        endpoints
        ;;
    "docker-images"|"docker")
        docker_images
        ;;
    "kubernetes"|"k8s")
        kubernetes
        ;;
    "search")
        search "$2"
        ;;
    "validate")
        validate
        ;;
    "help"|"--help"|"-h"|"")
        show_help
        ;;
    *)
        echo -e "${RED}Unknown command: $1${NC}"
        echo "Use '$0 help' for usage information"
        exit 1
        ;;
esac