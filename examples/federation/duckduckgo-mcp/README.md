# DuckDuckGo MCP Integration Example

This example demonstrates how to integrate with the DuckDuckGo MCP Server for privacy-focused web search.

## Features Demonstrated

- Privacy-focused web search with no tracking
- News search with time filtering
- Image search with advanced filters
- Webpage content extraction
- Advanced search scenarios
- Privacy-focused search patterns

## Running the Example

```bash
cd examples/federation/duckduckgo-mcp
go mod tidy
go run main.go
```

## Prerequisites

- DuckDuckGo MCP Server running on localhost:3402
- TAS MCP Federation server running on localhost:8080

## Search Capabilities

- **search** - Web search with region and safe search controls
- **search_news** - News search with time filtering
- **search_images** - Image search with size, color, and type filters  
- **fetch_content** - Webpage content extraction and parsing

## Privacy Benefits

- No user tracking or data collection
- No stored search history
- Anonymous search queries
- Privacy-first design