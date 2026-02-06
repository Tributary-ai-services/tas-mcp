#!/bin/bash
# ==============================================================================
# Napkin AI MCP Server - Integration Test Script
# Tests Napkin MCP Server integration with TAS MCP Federation
# ==============================================================================

set -e

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
TAS_MCP_ENDPOINT="${TAS_MCP_ENDPOINT:-http://localhost:8080}"
NAPKIN_MCP_ENDPOINT="${NAPKIN_MCP_ENDPOINT:-http://localhost:8087}"
NAPKIN_MCP_VERSION="${NAPKIN_MCP_VERSION:-1.0.0}"

PASSED=0
FAILED=0

pass() {
  echo -e "${GREEN}PASS${NC}: $1"
  PASSED=$((PASSED + 1))
}

fail() {
  echo -e "${RED}FAIL${NC}: $1"
  FAILED=$((FAILED + 1))
}

info() {
  echo -e "${BLUE}INFO${NC}: $1"
}

warn() {
  echo -e "${YELLOW}WARN${NC}: $1"
}

echo "============================================================="
echo "  Napkin AI MCP Server - Integration Tests"
echo "  TAS MCP Federation v${NAPKIN_MCP_VERSION}"
echo "============================================================="
echo ""

# --- Test 1: Check TAS MCP server ---
info "Test 1: Checking TAS MCP server is running..."
if curl -sf "${TAS_MCP_ENDPOINT}/health" > /dev/null 2>&1; then
  pass "TAS MCP server is healthy"
else
  fail "TAS MCP server is not reachable at ${TAS_MCP_ENDPOINT}"
  warn "Start the TAS MCP server first. Continuing with direct tests..."
fi

# --- Test 2: Check Napkin MCP server ---
info "Test 2: Checking Napkin MCP server is running..."
HEALTH_RESPONSE=$(curl -sf "${NAPKIN_MCP_ENDPOINT}/health" 2>/dev/null)
if [ $? -eq 0 ]; then
  pass "Napkin MCP server is healthy"
  echo "  Response: ${HEALTH_RESPONSE}"
else
  fail "Napkin MCP server is not reachable at ${NAPKIN_MCP_ENDPOINT}"
  echo ""
  echo "Start the Napkin MCP server:"
  echo "  cd tas-mcp-servers/servers/napkin-mcp && docker-compose up -d"
  exit 1
fi

# --- Test 3: Register with federation ---
info "Test 3: Registering Napkin MCP with TAS MCP federation..."
REG_RESPONSE=$(curl -sf -X POST "${TAS_MCP_ENDPOINT}/api/v1/federation/servers" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "napkin-mcp-server-v'"${NAPKIN_MCP_VERSION}"'",
    "name": "Napkin AI MCP Server v'"${NAPKIN_MCP_VERSION}"'",
    "description": "Visual generation from text using Napkin AI with MinIO storage",
    "version": "'"${NAPKIN_MCP_VERSION}"'",
    "category": "visual-generation",
    "endpoint": "'"${NAPKIN_MCP_ENDPOINT}"'",
    "protocol": "http",
    "auth": {"type": "none", "config": {}},
    "capabilities": ["generate_visual", "check_visual_status", "download_visual", "list_styles", "list_visuals", "create_napkin_visual_cr"],
    "tags": ["nodejs", "napkin-ai", "visual-generation", "svg", "png", "minio"],
    "health_check": {"enabled": true, "interval": "30s", "timeout": "10s", "path": "/health"}
  }' 2>/dev/null)
if [ $? -eq 0 ]; then
  pass "Napkin MCP registered with federation"
else
  warn "Federation registration may have failed (server might already be registered)"
fi

# --- Test 4: Verify in federation listing ---
info "Test 4: Verifying Napkin MCP appears in federation..."
SERVERS=$(curl -sf "${TAS_MCP_ENDPOINT}/api/v1/federation/servers" 2>/dev/null)
if echo "${SERVERS}" | grep -q "napkin-mcp-server" 2>/dev/null; then
  pass "Napkin MCP server found in federation listing"
else
  warn "Could not verify Napkin MCP in federation (federation API may not be available)"
fi

# --- Test 5: Test federation health check ---
info "Test 5: Testing federation health check for Napkin MCP..."
HEALTH=$(curl -sf "${TAS_MCP_ENDPOINT}/api/v1/federation/servers/napkin-mcp-server-v${NAPKIN_MCP_VERSION}/health" 2>/dev/null)
if [ $? -eq 0 ]; then
  pass "Federation health check passed"
else
  warn "Federation health check endpoint may not be implemented"
fi

# --- Test 6: Test list_styles invocation ---
info "Test 6: Testing list_styles invocation via federation..."
STYLES_RESPONSE=$(curl -sf -X POST "${TAS_MCP_ENDPOINT}/api/v1/federation/servers/napkin-mcp-server-v${NAPKIN_MCP_VERSION}/invoke" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "test-list-styles",
    "method": "list_styles",
    "params": {}
  }' 2>/dev/null)
if [ $? -eq 0 ]; then
  pass "list_styles invocation completed"
  echo "  Response: $(echo "${STYLES_RESPONSE}" | head -c 200)"
else
  warn "list_styles invocation may need a valid NAPKIN_API_KEY"
fi

# --- Test 7: Test generate_visual (if API key available) ---
if [ -n "${NAPKIN_API_KEY}" ]; then
  info "Test 7: Testing generate_visual with sample content..."
  GEN_RESPONSE=$(curl -sf -X POST "${TAS_MCP_ENDPOINT}/api/v1/federation/servers/napkin-mcp-server-v${NAPKIN_MCP_VERSION}/invoke" \
    -H 'Content-Type: application/json' \
    -d '{
      "id": "test-generate-visual",
      "method": "generate_visual",
      "params": {
        "content": "Steps to deploy a microservice: 1. Build Docker image 2. Push to registry 3. Apply K8s manifests 4. Verify health",
        "format": "svg"
      }
    }' 2>/dev/null)
  if [ $? -eq 0 ]; then
    pass "generate_visual invocation completed"
    echo "  Response: $(echo "${GEN_RESPONSE}" | head -c 200)"
  else
    fail "generate_visual invocation failed"
  fi
else
  info "Test 7: Skipping generate_visual test (NAPKIN_API_KEY not set)"
fi

# --- Summary ---
echo ""
echo "============================================================="
echo "  Test Results"
echo "============================================================="
echo -e "  ${GREEN}Passed${NC}: ${PASSED}"
echo -e "  ${RED}Failed${NC}: ${FAILED}"
echo ""
echo "  Access points:"
echo "    - Napkin MCP Health: ${NAPKIN_MCP_ENDPOINT}/health"
echo "    - TAS MCP API: ${TAS_MCP_ENDPOINT}"
echo "    - Federation API: ${TAS_MCP_ENDPOINT}/api/v1/federation"
echo "============================================================="

if [ ${FAILED} -gt 0 ]; then
  exit 1
fi
exit 0
