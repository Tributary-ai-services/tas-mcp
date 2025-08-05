#!/usr/bin/env node

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  ListResourcesRequestSchema,
  ReadResourceRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';
import { search } from 'duckduckgo-search';
import axios from 'axios';
import { JSDOM } from 'jsdom';
import pLimit from 'p-limit';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';
import dotenv from 'dotenv';

dotenv.config();

class DuckDuckGoMCPServer {
  constructor(options = {}) {
    this.maxResults = options.maxResults || 10;
    this.safeSearch = options.safeSearch || 'moderate';
    this.region = options.region || 'us-en';
    this.timeout = options.timeout || 30000;
    
    // Rate limiting
    this.searchLimit = pLimit(1); // 1 request per second for search
    this.fetchLimit = pLimit(3); // 3 concurrent content fetches
    
    this.server = new Server(
      {
        name: 'duckduckgo-mcp-server',
        version: process.env.DUCKDUCKGO_MCP_VERSION || '1.0.0',
      },
      {
        capabilities: {
          tools: {},
          resources: {},
        },
      }
    );

    this.setupHandlers();
  }

  setupHandlers() {
    // List available tools
    this.server.setRequestHandler(ListToolsRequestSchema, async () => {
      return {
        tools: [
          {
            name: 'search',
            description: 'Search the web using DuckDuckGo with privacy-focused results',
            inputSchema: {
              type: 'object',
              properties: {
                query: {
                  type: 'string',
                  description: 'The search query (max 400 characters)',
                  maxLength: 400,
                },
                max_results: {
                  type: 'integer',
                  description: 'Maximum number of results to return (1-20)',
                  minimum: 1,
                  maximum: 20,
                  default: 10,
                },
                safe_search: {
                  type: 'string',
                  description: 'Safe search level',
                  enum: ['strict', 'moderate', 'off'],
                  default: 'moderate',
                },
                region: {
                  type: 'string',
                  description: 'Search region (e.g., us-en, uk-en, de-de)',
                  default: 'us-en',
                },
                time_filter: {
                  type: 'string',
                  description: 'Time filter for results',
                  enum: ['day', 'week', 'month', 'year'],
                },
              },
              required: ['query'],
            },
          },
          {
            name: 'search_news',
            description: 'Search for news articles using DuckDuckGo News',
            inputSchema: {
              type: 'object',
              properties: {
                query: {
                  type: 'string',
                  description: 'The news search query (max 400 characters)',
                  maxLength: 400,
                },
                max_results: {
                  type: 'integer',
                  description: 'Maximum number of news results (1-30)',
                  minimum: 1,
                  maximum: 30,
                  default: 10,
                },
                region: {
                  type: 'string',
                  description: 'News region (e.g., us-en, uk-en)',
                  default: 'us-en',
                },
                time_filter: {
                  type: 'string',
                  description: 'Time filter for news',
                  enum: ['day', 'week', 'month'],
                },
              },
              required: ['query'],
            },
          },
          {
            name: 'fetch_content',
            description: 'Fetch and extract main content from a webpage URL',
            inputSchema: {
              type: 'object',
              properties: {
                url: {
                  type: 'string',
                  description: 'The URL to fetch content from',
                  format: 'uri',
                },
                extract_text: {
                  type: 'boolean',
                  description: 'Whether to extract only text content',
                  default: true,
                },
                max_content_length: {
                  type: 'integer',
                  description: 'Maximum content length in characters',
                  default: 10000,
                  maximum: 50000,
                },
              },
              required: ['url'],
            },
          },
          {
            name: 'search_images',
            description: 'Search for images using DuckDuckGo Images',
            inputSchema: {
              type: 'object',
              properties: {
                query: {
                  type: 'string',
                  description: 'The image search query',
                  maxLength: 400,
                },
                max_results: {
                  type: 'integer',
                  description: 'Maximum number of image results (1-50)',
                  minimum: 1,
                  maximum: 50,
                  default: 20,
                },
                safe_search: {
                  type: 'string',
                  description: 'Safe search level for images',
                  enum: ['strict', 'moderate', 'off'],
                  default: 'moderate',
                },
                size: {
                  type: 'string',
                  description: 'Image size filter',
                  enum: ['small', 'medium', 'large', 'wallpaper'],
                },
                color: {
                  type: 'string',
                  description: 'Image color filter',
                  enum: ['color', 'monochrome', 'red', 'orange', 'yellow', 'green', 'blue', 'purple', 'pink', 'brown', 'black', 'gray', 'teal', 'white'],
                },
                type: {
                  type: 'string',
                  description: 'Image type filter',
                  enum: ['photo', 'clipart', 'gif', 'transparent', 'line'],
                },
              },
              required: ['query'],
            },
          },
        ],
      };
    });

    // List available resources (recent searches as resources)
    this.server.setRequestHandler(ListResourcesRequestSchema, async () => {
      return {
        resources: [
          {
            uri: 'duckduckgo://search/trending',
            name: 'Trending Searches',
            description: 'Popular search trends and topics',
            mimeType: 'application/json',
          },
          {
            uri: 'duckduckgo://search/help',
            name: 'Search Help',
            description: 'DuckDuckGo search syntax and advanced operators',
            mimeType: 'text/plain',
          },
        ],
      };
    });

    // Read a specific resource
    this.server.setRequestHandler(ReadResourceRequestSchema, async (request) => {
      const { uri } = request.params;
      
      if (uri === 'duckduckgo://search/help') {
        return {
          contents: [
            {
              uri,
              mimeType: 'text/plain',
              text: `DuckDuckGo Search Syntax Help:

Basic Search:
- Simple keywords: "climate change"
- Exact phrases: "artificial intelligence" (use quotes)
- Exclude terms: cats -dogs (exclude dogs from cat results)

Advanced Operators:
- site:example.com - Search within a specific site
- filetype:pdf - Find specific file types
- inurl:keyword - Search for keyword in URLs
- intitle:keyword - Search for keyword in page titles
- OR operator: cats OR dogs
- AND operator: cats AND dogs (default behavior)

Special Searches:
- !bang commands: !w wikipedia, !g google, !yt youtube
- Time filters: Use time_filter parameter for recent results
- Region filters: Specify region for localized results
- Safe search: Control adult content filtering

Privacy Features:
- No tracking of searches
- No stored search history
- Anonymous search results
- No personal data collection`,
            },
          ],
        };
      }

      if (uri === 'duckduckgo://search/trending') {
        // Note: DuckDuckGo doesn't provide trending API, so we'll return a helpful message
        return {
          contents: [
            {
              uri,
              mimeType: 'application/json',
              text: JSON.stringify({
                message: 'DuckDuckGo does not track or provide trending searches to maintain user privacy',
                suggestion: 'Use the search tool with current topics or news-related queries instead',
                popular_categories: [
                  'Technology and AI',
                  'Current Events',
                  'Science and Health',
                  'Entertainment',
                  'Sports',
                  'Weather',
                ],
              }, null, 2),
            },
          ],
        };
      }

      throw new Error(`Unknown resource URI: ${uri}`);
    });

    // Handle tool calls
    this.server.setRequestHandler(CallToolRequestSchema, async (request) => {
      const { name, arguments: args } = request.params;

      switch (name) {
        case 'search':
          return this.handleSearch(args);
        case 'search_news':
          return this.handleSearchNews(args);
        case 'fetch_content':
          return this.handleFetchContent(args);
        case 'search_images':
          return this.handleSearchImages(args);
        default:
          throw new Error(`Unknown tool: ${name}`);
      }
    });
  }

  async handleSearch(args) {
    const {
      query,
      max_results = 10,
      safe_search = 'moderate',
      region = 'us-en',
      time_filter,
    } = args;

    try {
      const results = await this.searchLimit(async () => {
        const searchOptions = {
          keywords: query,
          region,
          safesearch: safe_search,
          max_results,
        };

        if (time_filter) {
          searchOptions.time = time_filter;
        }

        return await search(searchOptions);
      });

      const formattedResults = results.map((result, index) => ({
        rank: index + 1,
        title: result.title || '',
        url: result.href || '',
        snippet: result.body || '',
        displayedLink: result.href || '',
      }));

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              query,
              total_results: formattedResults.length,
              region,
              safe_search,
              time_filter,
              results: formattedResults,
              search_metadata: {
                search_engine: 'DuckDuckGo',
                privacy_friendly: true,
                no_tracking: true,
                timestamp: new Date().toISOString(),
              },
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Search failed: ${error.message}`);
    }
  }

  async handleSearchNews(args) {
    const {
      query,
      max_results = 10,
      region = 'us-en',
      time_filter,
    } = args;

    try {
      const results = await this.searchLimit(async () => {
        const searchOptions = {
          keywords: query,
          region,
          max_results,
        };

        if (time_filter) {
          searchOptions.time = time_filter;
        }

        // DuckDuckGo news search
        return await search({ ...searchOptions, type: 'news' });
      });

      const formattedResults = results.map((result, index) => ({
        rank: index + 1,
        title: result.title || '',
        url: result.href || '',
        snippet: result.body || '',
        source: result.source || '',
        date: result.date || '',
        image: result.image || null,
      }));

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              query,
              total_results: formattedResults.length,
              region,
              time_filter,
              results: formattedResults,
              search_metadata: {
                search_type: 'news',
                search_engine: 'DuckDuckGo News',
                privacy_friendly: true,
                timestamp: new Date().toISOString(),
              },
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`News search failed: ${error.message}`);
    }
  }

  async handleSearchImages(args) {
    const {
      query,
      max_results = 20,
      safe_search = 'moderate',
      size,
      color,
      type,
    } = args;

    try {
      const results = await this.searchLimit(async () => {
        const searchOptions = {
          keywords: query,
          safesearch: safe_search,
          max_results,
          type: 'images',
        };

        if (size) searchOptions.size = size;
        if (color) searchOptions.color = color;
        if (type) searchOptions.type_image = type;

        return await search(searchOptions);
      });

      const formattedResults = results.map((result, index) => ({
        rank: index + 1,
        title: result.title || '',
        image_url: result.image || '',
        thumbnail_url: result.thumbnail || '',
        source_url: result.url || '',
        source: result.source || '',
        width: result.width || null,
        height: result.height || null,
      }));

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              query,
              total_results: formattedResults.length,
              safe_search,
              filters: { size, color, type },
              results: formattedResults,
              search_metadata: {
                search_type: 'images',
                search_engine: 'DuckDuckGo Images',
                privacy_friendly: true,
                timestamp: new Date().toISOString(),
              },
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Image search failed: ${error.message}`);
    }
  }

  async handleFetchContent(args) {
    const {
      url,
      extract_text = true,
      max_content_length = 10000,
    } = args;

    try {
      const content = await this.fetchLimit(async () => {
        const response = await axios.get(url, {
          timeout: this.timeout,
          headers: {
            'User-Agent': 'Mozilla/5.0 (compatible; DuckDuckGo-MCP-Server/1.0)',
            'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
            'Accept-Language': 'en-US,en;q=0.5',
            'Accept-Encoding': 'gzip, deflate',
            'Connection': 'keep-alive',
            'DNT': '1',
          },
          maxRedirects: 5,
        });

        if (!response.data) {
          throw new Error('No content received');
        }

        return response.data;
      });

      let extractedContent;
      let metadata = {};

      if (extract_text) {
        const dom = new JSDOM(content);
        const document = dom.window.document;

        // Extract metadata
        const title = document.querySelector('title')?.textContent || '';
        const description = document.querySelector('meta[name="description"]')?.getAttribute('content') || '';
        const ogTitle = document.querySelector('meta[property="og:title"]')?.getAttribute('content') || '';
        const ogDescription = document.querySelector('meta[property="og:description"]')?.getAttribute('content') || '';

        metadata = {
          title: ogTitle || title,
          description: ogDescription || description,
          url,
          extracted_at: new Date().toISOString(),
        };

        // Remove script and style elements
        const scripts = document.querySelectorAll('script, style, nav, header, footer, aside');
        scripts.forEach(el => el.remove());

        // Try to find main content areas
        const contentSelectors = [
          'main',
          'article',
          '[role="main"]',
          '.content',
          '.main-content',
          '.article-content',
          '.post-content',
          '.entry-content',
          '#content',
          '#main',
        ];

        let mainContent = null;
        for (const selector of contentSelectors) {
          const element = document.querySelector(selector);
          if (element) {
            mainContent = element;
            break;
          }
        }

        // If no main content found, use body
        if (!mainContent) {
          mainContent = document.body;
        }

        // Extract text content
        extractedContent = mainContent?.textContent || '';
        
        // Clean up whitespace
        extractedContent = extractedContent
          .replace(/\s+/g, ' ')
          .replace(/\n\s*\n/g, '\n')
          .trim();

        // Truncate if too long
        if (extractedContent.length > max_content_length) {
          extractedContent = extractedContent.substring(0, max_content_length) + '...';
          metadata.truncated = true;
          metadata.original_length = extractedContent.length;
        }
      } else {
        extractedContent = content;
        metadata = {
          url,
          content_type: 'html',
          extracted_at: new Date().toISOString(),
        };
      }

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              url,
              metadata,
              content: extractedContent,
              extraction_settings: {
                extract_text,
                max_content_length,
              },
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Content fetch failed for ${url}: ${error.message}`);
    }
  }

  async start() {
    const transport = new StdioServerTransport();
    await this.server.connect(transport);
  }

  async close() {
    // Cleanup resources if needed
  }
}

// CLI argument parsing
const argv = yargs(hideBin(process.argv))
  .option('max-results', {
    type: 'number',
    description: 'Default maximum number of search results',
    default: 10,
  })
  .option('safe-search', {
    type: 'string',
    description: 'Default safe search level',
    choices: ['strict', 'moderate', 'off'],
    default: 'moderate',
  })
  .option('region', {
    type: 'string',
    description: 'Default search region',
    default: 'us-en',
  })
  .option('timeout', {
    type: 'number',
    description: 'Request timeout in milliseconds',
    default: 30000,
  })
  .help()
  .argv;

// Start the server
const server = new DuckDuckGoMCPServer({
  maxResults: argv.maxResults,
  safeSearch: argv.safeSearch,
  region: argv.region,
  timeout: argv.timeout,
});

// Handle graceful shutdown
process.on('SIGINT', async () => {
  console.error('Received SIGINT, shutting down gracefully...');
  await server.close();
  process.exit(0);
});

process.on('SIGTERM', async () => {
  console.error('Received SIGTERM, shutting down gracefully...');
  await server.close();
  process.exit(0);
});

// Start the server
server.start().catch((error) => {
  console.error('Failed to start DuckDuckGo MCP server:', error);
  process.exit(1);
});