#!/bin/bash

# MCP Server Registry Endpoint Validation Script
# Validates that all servers in the registry have endpoint information

REGISTRY_FILE="$(dirname "$0")/../mcp-servers-expanded.json"
ENDPOINTS_FILE="$(dirname "$0")/../endpoints-discovered.json"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 MCP Registry Endpoint Validation${NC}"
echo "====================================="
echo ""

# Check if required files exist
if [[ ! -f "$REGISTRY_FILE" ]]; then
    echo -e "${RED}Error: Registry file not found: $REGISTRY_FILE${NC}"
    exit 1
fi

if [[ ! -f "$ENDPOINTS_FILE" ]]; then
    echo -e "${RED}Error: Endpoints file not found: $ENDPOINTS_FILE${NC}"
    exit 1
fi

# Count total servers
total_servers=$(jq '.servers | length' "$REGISTRY_FILE")
echo -e "${YELLOW}Total servers in registry: $total_servers${NC}"

# Count servers with endpoints
servers_with_endpoints=$(jq '[.servers[] | select(has("endpoints"))] | length' "$REGISTRY_FILE")
echo -e "${GREEN}Servers with endpoints: $servers_with_endpoints${NC}"

# Count servers without endpoints
servers_without_endpoints=$((total_servers - servers_with_endpoints))
if [[ $servers_without_endpoints -gt 0 ]]; then
    echo -e "${RED}Servers missing endpoints: $servers_without_endpoints${NC}"
    echo ""
    echo -e "${YELLOW}Servers missing endpoint information:${NC}"
    jq -r '.servers[] | select(has("endpoints") | not) | "  - " + .name + " (" + .id + ")"' "$REGISTRY_FILE"
else
    echo -e "${GREEN}✅ All servers have endpoint information${NC}"
fi

echo ""

# Validate endpoint types
echo -e "${BLUE}📊 Endpoint Statistics:${NC}"
http_endpoints=$(jq '[.servers[].endpoints.http[]?] | length' "$REGISTRY_FILE")
grpc_endpoints=$(jq '[.servers[].endpoints.grpc[]?] | length' "$REGISTRY_FILE")

echo "  HTTP endpoints: $http_endpoints"
echo "  gRPC endpoints: $grpc_endpoints"

# Count live vs localhost endpoints
live_endpoints=$(jq '[.servers[].endpoints.http[]? | select(startswith("https://"))] | length' "$REGISTRY_FILE")
localhost_endpoints=$(jq '[.servers[].endpoints.http[]? | select(startswith("http://localhost"))] | length' "$REGISTRY_FILE")

echo "  Live HTTP endpoints: $live_endpoints"
echo "  Localhost endpoints: $localhost_endpoints"

echo ""

# Show live endpoints summary
echo -e "${BLUE}🌐 Live Production Endpoints:${NC}"
jq -r '.servers[] | select(.endpoints.http[]? | startswith("https://")) | "  • " + .name + ": " + (.endpoints.http[]? | select(startswith("https://")))' "$REGISTRY_FILE"

echo ""

# Cross-reference with discovered endpoints
echo -e "${BLUE}🔗 Cross-Reference with Discovery Results:${NC}"
discovered_live=$(jq '.endpoints.live | length' "$ENDPOINTS_FILE")
echo "  Discovered live endpoints: $discovered_live"

# Validate that discovered endpoints are in registry
echo ""
echo -e "${YELLOW}Validating discovered endpoints are in registry:${NC}"

# Extract URLs from discovered endpoints and check if they exist in registry
jq -r '.endpoints.live[].url' "$ENDPOINTS_FILE" | while read -r url; do
    if jq -e --arg url "$url" '.servers[].endpoints.http[]? | select(. == $url)' "$REGISTRY_FILE" > /dev/null; then
        echo -e "  ${GREEN}✅${NC} $url (found in registry)"
    else
        echo -e "  ${RED}❌${NC} $url (missing from registry)"
    fi
done

echo ""
echo -e "${GREEN}✅ Endpoint validation complete!${NC}"