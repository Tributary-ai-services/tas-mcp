#!/bin/sh

set -e

echo "🦆 Starting DuckDuckGo MCP Server v${DUCKDUCKGO_MCP_VERSION:-1.0.0}"
echo "🔍 Search region: ${SEARCH_REGION:-us-en}"
echo "🔒 Safe search: ${SAFE_SEARCH:-moderate}"
echo "📊 Max results: ${MAX_RESULTS:-10}"

# Health check endpoint setup (for Docker health checks)
if [ "$HEALTH_CHECK_ENABLED" = "true" ]; then
    echo "🏥 Health check enabled on port ${HEALTH_PORT:-3402}"
    
    # Create simple health check server
    cat > health-server.js << 'HEALTH_EOF'
import http from 'http';

const server = http.createServer((req, res) => {
  if (req.url === '/health' && req.method === 'GET') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      status: 'healthy',
      service: 'duckduckgo-mcp-server',
      version: process.env.DUCKDUCKGO_MCP_VERSION || '1.0.0',
      timestamp: new Date().toISOString(),
      search_capabilities: {
        web_search: true,
        news_search: true,
        image_search: true,
        content_fetch: true
      },
      privacy_features: {
        no_tracking: true,
        no_stored_history: true,
        anonymous_search: true
      }
    }));
  } else if (req.url === '/metrics' && req.method === 'GET') {
    res.writeHead(200, { 'Content-Type': 'text/plain' });
    res.end(`# HELP duckduckgo_mcp_server_uptime_seconds Server uptime in seconds
# TYPE duckduckgo_mcp_server_uptime_seconds counter
duckduckgo_mcp_server_uptime_seconds ${process.uptime()}

# HELP duckduckgo_mcp_server_memory_usage_bytes Memory usage in bytes
# TYPE duckduckgo_mcp_server_memory_usage_bytes gauge
duckduckgo_mcp_server_memory_usage_bytes{type="rss"} ${process.memoryUsage().rss}
duckduckgo_mcp_server_memory_usage_bytes{type="heap_used"} ${process.memoryUsage().heapUsed}
duckduckgo_mcp_server_memory_usage_bytes{type="heap_total"} ${process.memoryUsage().heapTotal}

# HELP duckduckgo_mcp_server_info Server information
# TYPE duckduckgo_mcp_server_info gauge
duckduckgo_mcp_server_info{version="${process.env.DUCKDUCKGO_MCP_VERSION || '1.0.0'}",node_version="${process.version}"} 1
`);
  } else {
    res.writeHead(404);
    res.end('Not Found');
  }
});

const port = process.env.HEALTH_PORT || 3402;
server.listen(port, '0.0.0.0', () => {
  console.log(`Health check server running on port ${port}`);
});
HEALTH_EOF

    # Start health server in background
    node health-server.js &
fi

# Wait a moment for health server to start
sleep 2

# Start the main DuckDuckGo MCP server
echo "🚀 Starting DuckDuckGo MCP server..."
exec node server.js "$@"