# Playwright MCP Server

Browser automation and web testing server for the TAS MCP Federation using Playwright.

## Overview

This MCP server provides comprehensive browser automation capabilities through Playwright, enabling AI agents to interact with web pages, fill forms, take screenshots, and extract content programmatically.

## Features

- **Navigation**: Navigate to URLs, go back/forward, reload pages
- **Screenshots**: Capture full page or element screenshots (PNG/JPEG)
- **Form Interaction**: Fill inputs, select dropdowns, click buttons
- **Element Interaction**: Click, hover, wait for elements
- **Text Extraction**: Get text content and attribute values
- **JavaScript Evaluation**: Execute arbitrary JavaScript in browser context
- **Viewport Control**: Set browser window size for responsive testing
- **Keyboard/Mouse**: Simulate key presses and mouse events

## Quick Start

### Using Docker Compose

```bash
# Start the Playwright MCP server
cd tas-mcp/deployments/docker-compose/playwright-mcp
docker-compose up -d

# Check health
curl http://localhost:3404/health
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PLAYWRIGHT_MCP_VERSION` | `1.0.0` | Server version |
| `HEADLESS` | `true` | Run browser in headless mode |
| `DEFAULT_TIMEOUT` | `30000` | Default timeout in milliseconds |
| `VIEWPORT_WIDTH` | `1280` | Default viewport width |
| `VIEWPORT_HEIGHT` | `720` | Default viewport height |
| `USER_AGENT` | Chrome 120 | Custom user agent string |
| `HEALTH_PORT` | `3404` | Health check endpoint port |
| `LOG_LEVEL` | `info` | Logging level |

## Available Tools

### Navigation

- **navigate**: Navigate to a URL
- **go_back**: Navigate back in history
- **go_forward**: Navigate forward in history
- **reload**: Reload the current page

### Screenshots

- **screenshot**: Capture screenshots (full page or element)

### Form Interaction

- **fill**: Fill a form field with text
- **type**: Type text with keyboard simulation
- **select**: Select option(s) from a dropdown
- **click**: Click on an element

### Element Interaction

- **hover**: Hover over an element
- **wait_for_selector**: Wait for an element to appear
- **scroll**: Scroll the page or an element

### Content Extraction

- **get_text**: Get text content of an element
- **get_attribute**: Get an attribute value from an element
- **get_page_content**: Get HTML content of the page
- **get_page_info**: Get URL, title, and viewport info

### Advanced

- **evaluate**: Execute JavaScript in the browser
- **set_viewport**: Set browser viewport size
- **press_key**: Press keyboard keys
- **close_browser**: Close the browser instance

## Example Usage

### Navigate and Screenshot

```json
{
  "tool": "navigate",
  "arguments": {
    "url": "https://example.com",
    "wait_until": "networkidle"
  }
}
```

```json
{
  "tool": "screenshot",
  "arguments": {
    "full_page": true,
    "type": "png"
  }
}
```

### Fill a Form

```json
{
  "tool": "fill",
  "arguments": {
    "selector": "#email",
    "value": "user@example.com"
  }
}
```

```json
{
  "tool": "click",
  "arguments": {
    "selector": "button[type='submit']"
  }
}
```

### Extract Content

```json
{
  "tool": "get_text",
  "arguments": {
    "selector": ".article-content"
  }
}
```

## Resource Requirements

- **Memory**: 2GB recommended (browser operations are memory-intensive)
- **Shared Memory**: 2GB (`shm_size`) required for Chrome stability
- **Capabilities**: `SYS_ADMIN` for Chrome sandbox

## Security Considerations

- The server runs Chromium in a sandboxed environment
- Headless mode is enabled by default for security
- No persistent storage of browsing data
- Rate limiting recommended for production deployments

## Health Check

```bash
curl http://localhost:3404/health
```

Response:
```json
{
  "status": "healthy",
  "service": "playwright-mcp-server",
  "version": "1.0.0",
  "browser_capabilities": {
    "chromium": true,
    "headless": true,
    "viewport": { "width": 1280, "height": 720 }
  }
}
```

## Metrics

Prometheus metrics are available at `/metrics`:

```bash
curl http://localhost:3404/metrics
```

## Integration with TAS MCP Federation

This server integrates with the TAS MCP Federation platform. Once deployed, it will be automatically discovered and available through the federation gateway.

## License

MIT License - See LICENSE file for details.
