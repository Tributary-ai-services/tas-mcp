#!/bin/sh

set -e

echo "🕷️ Starting Apify MCP Server v${APIFY_MCP_VERSION:-1.0.0}"
echo "🔑 API Token: ${APIFY_API_TOKEN:+[SET]}${APIFY_API_TOKEN:-[NOT SET - LIMITED FUNCTIONALITY]}"
echo "💾 Default Memory: ${DEFAULT_MEMORY_MBYTES:-512}MB"
echo "⏱️ Default Timeout: ${DEFAULT_TIMEOUT_SECS:-300}s"

# Health check endpoint setup (for Docker health checks)
if [ "$HEALTH_CHECK_ENABLED" = "true" ]; then
    echo "🏥 Health check enabled on port ${HEALTH_PORT:-3403}"
    
    # Create simple health check server
    cat > health-server.js << 'HEALTH_EOF'
import http from 'http';

const server = http.createServer((req, res) => {
  if (req.url === '/health' && req.method === 'GET') {
    const hasApiToken = !!process.env.APIFY_API_TOKEN;
    
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      status: 'healthy',
      service: 'apify-mcp-server',
      version: process.env.APIFY_MCP_VERSION || '1.0.0',
      timestamp: new Date().toISOString(),
      apify_integration: {
        api_token_configured: hasApiToken,
        default_memory_mbytes: parseInt(process.env.DEFAULT_MEMORY_MBYTES || '512'),
        default_timeout_secs: parseInt(process.env.DEFAULT_TIMEOUT_SECS || '300')
      },
      capabilities: {
        run_actors: hasApiToken,
        search_actors: true,
        get_actor_info: true,
        web_scraping: hasApiToken,
        data_extraction: hasApiToken
      },
      limitations: hasApiToken ? [] : ['API token required for full functionality']
    }));
  } else if (req.url === '/metrics' && req.method === 'GET') {
    const hasApiToken = !!process.env.APIFY_API_TOKEN;
    
    res.writeHead(200, { 'Content-Type': 'text/plain' });
    res.end(`# HELP apify_mcp_server_uptime_seconds Server uptime in seconds
# TYPE apify_mcp_server_uptime_seconds counter
apify_mcp_server_uptime_seconds ${process.uptime()}

# HELP apify_mcp_server_memory_usage_bytes Memory usage in bytes
# TYPE apify_mcp_server_memory_usage_bytes gauge
apify_mcp_server_memory_usage_bytes{type="rss"} ${process.memoryUsage().rss}
apify_mcp_server_memory_usage_bytes{type="heap_used"} ${process.memoryUsage().heapUsed}
apify_mcp_server_memory_usage_bytes{type="heap_total"} ${process.memoryUsage().heapTotal}

# HELP apify_mcp_server_api_token_configured API token configuration status
# TYPE apify_mcp_server_api_token_configured gauge
apify_mcp_server_api_token_configured ${hasApiToken ? 1 : 0}

# HELP apify_mcp_server_info Server information
# TYPE apify_mcp_server_info gauge
apify_mcp_server_info{version="${process.env.APIFY_MCP_VERSION || '1.0.0'}",node_version="${process.version}"} 1
`);
  } else {
    res.writeHead(404);
    res.end('Not Found');
  }
});

const port = process.env.HEALTH_PORT || 3403;
server.listen(port, '0.0.0.0', () => {
  console.log(`Health check server running on port ${port}`);
});
HEALTH_EOF

    # Start health server in background
    node health-server.js &
fi

# Wait a moment for health server to start
sleep 2

# Validate API token if provided
if [ -n "$APIFY_API_TOKEN" ]; then
    echo "✅ Apify API token configured - full functionality available"
else
    echo "⚠️ No Apify API token configured - some features will be limited"
    echo "   Set APIFY_API_TOKEN environment variable for full functionality"
fi

# Start the main Apify MCP server
echo "🚀 Starting Apify MCP server..."
exec node server.js "$@"