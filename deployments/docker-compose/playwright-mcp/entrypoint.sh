#!/bin/bash

set -e

echo "Starting Playwright MCP Server v${PLAYWRIGHT_MCP_VERSION:-1.0.0}"
echo "Headless mode: ${HEADLESS:-true}"
echo "Viewport: ${VIEWPORT_WIDTH:-1280}x${VIEWPORT_HEIGHT:-720}"
echo "Default timeout: ${DEFAULT_TIMEOUT:-30000}ms"

# Create combined health server and MCP HTTP wrapper
cat > http-server.js << 'HTTP_EOF'
import http from 'http';
import { spawn } from 'child_process';
import { chromium } from 'playwright';

// Browser instance for direct HTTP API
let browser = null;
let context = null;
let page = null;

const headless = process.env.HEADLESS !== 'false';
const viewportWidth = parseInt(process.env.VIEWPORT_WIDTH) || 1280;
const viewportHeight = parseInt(process.env.VIEWPORT_HEIGHT) || 720;
const timeout = parseInt(process.env.DEFAULT_TIMEOUT) || 30000;

async function ensureBrowser() {
  if (!browser) {
    browser = await chromium.launch({
      headless,
      args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage', '--disable-gpu'],
    });
  }
  if (!context) {
    context = await browser.newContext({
      viewport: { width: viewportWidth, height: viewportHeight },
    });
  }
  if (!page) {
    page = await context.newPage();
    page.setDefaultTimeout(timeout);
  }
  return page;
}

async function closeBrowser() {
  if (page) { await page.close().catch(() => {}); page = null; }
  if (context) { await context.close().catch(() => {}); context = null; }
  if (browser) { await browser.close().catch(() => {}); browser = null; }
}

// Track operations for metrics
let operationCount = 0;
let errorCount = 0;

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`);

  // Health endpoint
  if (url.pathname === '/health' && req.method === 'GET') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      status: 'healthy',
      service: 'playwright-mcp-server',
      version: process.env.PLAYWRIGHT_MCP_VERSION || '1.0.0',
      timestamp: new Date().toISOString(),
      browser_capabilities: {
        chromium: true,
        headless,
        viewport: { width: viewportWidth, height: viewportHeight }
      },
      features: {
        navigation: true,
        screenshots: true,
        form_interaction: true,
        javascript_evaluation: true,
        element_interaction: true,
        keyboard_input: true,
        mouse_events: true
      },
      stats: {
        operations: operationCount,
        errors: errorCount,
        browser_connected: !!browser
      }
    }));
    return;
  }

  // Metrics endpoint
  if (url.pathname === '/metrics' && req.method === 'GET') {
    res.writeHead(200, { 'Content-Type': 'text/plain' });
    res.end(`# HELP playwright_mcp_server_uptime_seconds Server uptime in seconds
# TYPE playwright_mcp_server_uptime_seconds counter
playwright_mcp_server_uptime_seconds ${process.uptime()}

# HELP playwright_mcp_server_operations_total Total operations performed
# TYPE playwright_mcp_server_operations_total counter
playwright_mcp_server_operations_total ${operationCount}

# HELP playwright_mcp_server_errors_total Total errors encountered
# TYPE playwright_mcp_server_errors_total counter
playwright_mcp_server_errors_total ${errorCount}

# HELP playwright_mcp_server_memory_usage_bytes Memory usage in bytes
# TYPE playwright_mcp_server_memory_usage_bytes gauge
playwright_mcp_server_memory_usage_bytes{type="rss"} ${process.memoryUsage().rss}
playwright_mcp_server_memory_usage_bytes{type="heap_used"} ${process.memoryUsage().heapUsed}
playwright_mcp_server_memory_usage_bytes{type="heap_total"} ${process.memoryUsage().heapTotal}

# HELP playwright_mcp_server_browser_connected Browser connection status
# TYPE playwright_mcp_server_browser_connected gauge
playwright_mcp_server_browser_connected ${browser ? 1 : 0}

# HELP playwright_mcp_server_info Server information
# TYPE playwright_mcp_server_info gauge
playwright_mcp_server_info{version="${process.env.PLAYWRIGHT_MCP_VERSION || '1.0.0'}",node_version="${process.version}",headless="${headless}"} 1
`);
    return;
  }

  // API endpoints for browser automation
  if (url.pathname === '/api/navigate' && req.method === 'POST') {
    try {
      const body = await getRequestBody(req);
      const { url: targetUrl, wait_until = 'load' } = JSON.parse(body);
      const p = await ensureBrowser();
      await p.goto(targetUrl, { waitUntil: wait_until });
      operationCount++;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({
        success: true,
        url: p.url(),
        title: await p.title()
      }));
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  if (url.pathname === '/api/screenshot' && req.method === 'POST') {
    try {
      const body = await getRequestBody(req);
      const { selector, full_page = false, type = 'png' } = JSON.parse(body || '{}');
      const p = await ensureBrowser();

      let buffer;
      if (selector) {
        buffer = await p.locator(selector).screenshot({ type, fullPage: full_page });
      } else {
        buffer = await p.screenshot({ type, fullPage: full_page });
      }
      operationCount++;
      res.writeHead(200, { 'Content-Type': type === 'jpeg' ? 'image/jpeg' : 'image/png' });
      res.end(buffer);
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  if (url.pathname === '/api/click' && req.method === 'POST') {
    try {
      const body = await getRequestBody(req);
      const { selector } = JSON.parse(body);
      const p = await ensureBrowser();
      await p.click(selector);
      operationCount++;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ success: true, action: 'click', selector }));
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  if (url.pathname === '/api/fill' && req.method === 'POST') {
    try {
      const body = await getRequestBody(req);
      const { selector, value } = JSON.parse(body);
      const p = await ensureBrowser();
      await p.fill(selector, value);
      operationCount++;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ success: true, action: 'fill', selector }));
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  if (url.pathname === '/api/evaluate' && req.method === 'POST') {
    try {
      const body = await getRequestBody(req);
      const { script } = JSON.parse(body);
      const p = await ensureBrowser();
      const result = await p.evaluate(script);
      operationCount++;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ success: true, result }));
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  if (url.pathname === '/api/content' && req.method === 'GET') {
    try {
      const p = await ensureBrowser();
      const content = await p.content();
      operationCount++;
      res.writeHead(200, { 'Content-Type': 'text/html' });
      res.end(content);
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  if (url.pathname === '/api/info' && req.method === 'GET') {
    try {
      const p = await ensureBrowser();
      operationCount++;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({
        url: p.url(),
        title: await p.title(),
        viewport: p.viewportSize()
      }));
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  if (url.pathname === '/api/close' && req.method === 'POST') {
    try {
      await closeBrowser();
      operationCount++;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ success: true, action: 'close_browser' }));
    } catch (error) {
      errorCount++;
      res.writeHead(500, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: error.message }));
    }
    return;
  }

  // Not found
  res.writeHead(404, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ error: 'Not Found', available_endpoints: [
    'GET /health',
    'GET /metrics',
    'POST /api/navigate',
    'POST /api/screenshot',
    'POST /api/click',
    'POST /api/fill',
    'POST /api/evaluate',
    'GET /api/content',
    'GET /api/info',
    'POST /api/close'
  ]}));
});

function getRequestBody(req) {
  return new Promise((resolve, reject) => {
    let body = '';
    req.on('data', chunk => body += chunk);
    req.on('end', () => resolve(body));
    req.on('error', reject);
  });
}

const port = process.env.HEALTH_PORT || 3404;
server.listen(port, '0.0.0.0', () => {
  console.log(`Playwright MCP HTTP Server running on port ${port}`);
  console.log(`Health: http://localhost:${port}/health`);
  console.log(`Metrics: http://localhost:${port}/metrics`);
  console.log(`API endpoints available at http://localhost:${port}/api/*`);
});

// Graceful shutdown
process.on('SIGTERM', async () => {
  console.log('Shutting down...');
  await closeBrowser();
  server.close();
  process.exit(0);
});

process.on('SIGINT', async () => {
  console.log('Shutting down...');
  await closeBrowser();
  server.close();
  process.exit(0);
});
HTTP_EOF

echo "Health check enabled on port ${HEALTH_PORT:-3404}"

# Start the HTTP server (which includes both health checks and browser automation API)
echo "Starting Playwright MCP HTTP server..."
exec node http-server.js "$@"
