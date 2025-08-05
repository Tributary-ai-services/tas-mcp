# Apify MCP Integration Example

This example demonstrates how to integrate with the Apify MCP Server for comprehensive web scraping and automation.

## Features Demonstrated

- Access to 5,000+ Apify web scraping actors
- Actor discovery and search by category
- Custom scraping configurations
- Dataset management and export
- Multi-category scraping workflows
- Popular actors exploration

## Running the Example

```bash
cd examples/federation/apify-mcp
go mod tidy
go run main.go
```

## Prerequisites

- Apify MCP Server running on localhost:3403
- TAS MCP Federation server running on localhost:8080
- Optional: Apify API token for full functionality

## Scraping Capabilities

- **run_actor** - Execute any Apify actor with custom configurations
- **get_actor_info** - Retrieve detailed actor metadata
- **search_actors** - Discover actors by category or query
- **get_run_status** - Monitor actor execution status
- **get_dataset_items** - Access extracted data from runs
- **scrape_url** - Quick URL scraping with data extraction

## Supported Categories

- E-commerce scraping
- Social media data extraction
- News and content scraping
- SEO and search tools
- Developer utilities
- Entertainment data