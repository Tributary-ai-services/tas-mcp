#!/usr/bin/env node

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  ListResourcesRequestSchema,
  ReadResourceRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';
import { ApifyClient } from 'apify-client';
import axios from 'axios';
import cheerio from 'cheerio';
import pLimit from 'p-limit';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';
import dotenv from 'dotenv';
import { z } from 'zod';

dotenv.config();

class ApifyMCPServer {
  constructor(options = {}) {
    this.apiToken = options.apiToken || process.env.APIFY_API_TOKEN;
    this.defaultMemoryMbytes = options.defaultMemoryMbytes || 512;
    this.defaultTimeoutSecs = options.defaultTimeoutSecs || 300;
    
    if (!this.apiToken) {
      console.warn('⚠️ No Apify API token provided. Some features may be limited.');
    }
    
    // Initialize Apify client
    this.client = new ApifyClient({
      token: this.apiToken,
    });
    
    // Rate limiting
    this.runLimit = pLimit(2); // 2 concurrent actor runs
    this.apiLimit = pLimit(5); // 5 concurrent API calls
    
    this.server = new Server(
      {
        name: 'apify-mcp-server',
        version: process.env.APIFY_MCP_VERSION || '1.0.0',
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
            name: 'run_actor',
            description: 'Run an Apify Actor for web scraping, automation, or data extraction',
            inputSchema: {
              type: 'object',
              properties: {
                actor_id: {
                  type: 'string',
                  description: 'Actor ID or name (e.g., apify/web-scraper, apify/instagram-scraper)',
                },
                input: {
                  type: 'object',
                  description: 'Input configuration for the actor',
                  default: {},
                },
                memory_mbytes: {
                  type: 'integer',
                  description: 'Memory allocation in MB',
                  minimum: 128,
                  maximum: 32768,
                  default: 512,
                },
                timeout_secs: {
                  type: 'integer',
                  description: 'Timeout in seconds',
                  minimum: 30,
                  maximum: 3600,
                  default: 300,
                },
                build: {
                  type: 'string',
                  description: 'Specific build version to use (optional)',
                },
                wait_for_finish: {
                  type: 'boolean',
                  description: 'Wait for the run to complete',
                  default: true,
                },
              },
              required: ['actor_id'],
            },
          },
          {
            name: 'get_actor_info',
            description: 'Get detailed information about an Apify Actor',
            inputSchema: {
              type: 'object',
              properties: {
                actor_id: {
                  type: 'string',
                  description: 'Actor ID or name',
                },
              },
              required: ['actor_id'],
            },
          },
          {
            name: 'search_actors',
            description: 'Search for Actors in the Apify Store',
            inputSchema: {
              type: 'object',
              properties: {
                query: {
                  type: 'string',
                  description: 'Search query for actors',
                },
                category: {
                  type: 'string',
                  description: 'Filter by category',
                  enum: ['E_COMMERCE', 'SOCIAL_MEDIA', 'TRAVEL', 'NEWS', 'SEO', 'DEVELOPER_TOOLS', 'ENTERTAINMENT', 'OTHER'],
                },
                limit: {
                  type: 'integer',
                  description: 'Maximum number of results',
                  minimum: 1,
                  maximum: 100,
                  default: 20,
                },
                sort_by: {
                  type: 'string',
                  description: 'Sort results by',
                  enum: ['relevance', 'popularity', 'newest', 'name'],
                  default: 'relevance',
                },
              },
              required: ['query'],
            },
          },
          {
            name: 'get_run_status',
            description: 'Get the status and results of an Actor run',
            inputSchema: {
              type: 'object',
              properties: {
                run_id: {
                  type: 'string',
                  description: 'The run ID to check',
                },
                fetch_items: {
                  type: 'boolean',
                  description: 'Whether to fetch the dataset items',
                  default: true,
                },
                items_limit: {
                  type: 'integer',
                  description: 'Maximum number of items to fetch',
                  minimum: 1,
                  maximum: 1000,
                  default: 100,
                },
              },
              required: ['run_id'],
            },
          },
          {
            name: 'get_dataset_items',
            description: 'Retrieve items from an Apify dataset',
            inputSchema: {
              type: 'object',
              properties: {
                dataset_id: {
                  type: 'string',
                  description: 'Dataset ID to fetch items from',
                },
                limit: {
                  type: 'integer',
                  description: 'Maximum number of items to fetch',
                  minimum: 1,
                  maximum: 1000,
                  default: 100,
                },
                offset: {
                  type: 'integer',
                  description: 'Number of items to skip',
                  minimum: 0,
                  default: 0,
                },
                format: {
                  type: 'string',
                  description: 'Output format',
                  enum: ['json', 'csv', 'xml'],
                  default: 'json',
                },
                clean: {
                  type: 'boolean',
                  description: 'Clean the items (remove empty values)',
                  default: false,
                },
              },
              required: ['dataset_id'],
            },
          },
          {
            name: 'scrape_url',
            description: 'Quick web scraping using Apify Web Scraper actor',
            inputSchema: {
              type: 'object',
              properties: {
                urls: {
                  type: 'array',
                  description: 'URLs to scrape',
                  items: { type: 'string', format: 'uri' },
                  minItems: 1,
                  maxItems: 10,
                },
                extract_data: {
                  type: 'object',
                  description: 'Data extraction configuration',
                  properties: {
                    title: { type: 'string', description: 'CSS selector for title' },
                    content: { type: 'string', description: 'CSS selector for content' },
                    links: { type: 'string', description: 'CSS selector for links' },
                    images: { type: 'string', description: 'CSS selector for images' },
                  },
                },
                javascript_enabled: {
                  type: 'boolean',
                  description: 'Enable JavaScript rendering',
                  default: true,
                },
                max_pages: {
                  type: 'integer',
                  description: 'Maximum pages to scrape per URL',
                  minimum: 1,
                  maximum: 100,
                  default: 1,
                },
              },
              required: ['urls'],
            },
          },
        ],
      };
    });

    // List available resources (popular actors and categories)
    this.server.setRequestHandler(ListResourcesRequestSchema, async () => {
      return {
        resources: [
          {
            uri: 'apify://actors/popular',
            name: 'Popular Actors',
            description: 'Most popular Apify Actors for web scraping and automation',
            mimeType: 'application/json',
          },
          {
            uri: 'apify://actors/categories',
            name: 'Actor Categories',
            description: 'Browse actors by category (e-commerce, social media, etc.)',
            mimeType: 'application/json',
          },
          {
            uri: 'apify://templates/scraping',
            name: 'Scraping Templates',
            description: 'Pre-configured templates for common scraping tasks',
            mimeType: 'application/json',
          },
          {
            uri: 'apify://help/getting-started',
            name: 'Getting Started Guide',
            description: 'Learn how to use Apify for web scraping and automation',
            mimeType: 'text/plain',
          },
        ],
      };
    });

    // Read a specific resource
    this.server.setRequestHandler(ReadResourceRequestSchema, async (request) => {
      const { uri } = request.params;
      
      if (uri === 'apify://actors/popular') {
        const popularActors = [
          {
            id: 'apify/web-scraper',
            name: 'Web Scraper',
            description: 'Universal web scraper for any website',
            category: 'DEVELOPER_TOOLS',
            runs_count: 50000,
          },
          {
            id: 'apify/instagram-scraper',
            name: 'Instagram Scraper',
            description: 'Extract posts, profiles, and hashtags from Instagram',
            category: 'SOCIAL_MEDIA',
            runs_count: 25000,
          },
          {
            id: 'apify/google-search-results-scraper',
            name: 'Google Search Results Scraper',
            description: 'Scrape Google search results and ads',
            category: 'SEO',
            runs_count: 30000,
          },
          {
            id: 'apify/amazon-product-scraper',
            name: 'Amazon Product Scraper',
            description: 'Extract product data from Amazon',
            category: 'E_COMMERCE',
            runs_count: 20000,
          },
          {
            id: 'apify/linkedin-company-scraper',
            name: 'LinkedIn Company Scraper',
            description: 'Extract company data from LinkedIn',
            category: 'SOCIAL_MEDIA',
            runs_count: 15000,
          },
        ];

        return {
          contents: [
            {
              uri,
              mimeType: 'application/json',
              text: JSON.stringify({ popular_actors: popularActors }, null, 2),
            },
          ],
        };
      }

      if (uri === 'apify://actors/categories') {
        const categories = {
          E_COMMERCE: ['Product scraping', 'Price monitoring', 'Review extraction'],
          SOCIAL_MEDIA: ['Profile scraping', 'Post extraction', 'Hashtag analysis'],
          TRAVEL: ['Hotel data', 'Flight information', 'Review scraping'],
          NEWS: ['Article extraction', 'News monitoring', 'RSS feeds'],
          SEO: ['Search results', 'Keyword analysis', 'SERP tracking'],
          DEVELOPER_TOOLS: ['Web scraping', 'API testing', 'Data processing'],
          ENTERTAINMENT: ['Movie data', 'TV shows', 'Gaming information'],
          OTHER: ['General automation', 'Custom scrapers', 'Data tools'],
        };

        return {
          contents: [
            {
              uri,
              mimeType: 'application/json',
              text: JSON.stringify({ categories }, null, 2),
            },
          ],
        };
      }

      if (uri === 'apify://templates/scraping') {
        const templates = [
          {
            name: 'E-commerce Product Scraping',
            actor: 'apify/web-scraper',
            description: 'Extract product titles, prices, and descriptions',
            input_template: {
              startUrls: [{ url: 'https://example-shop.com/products' }],
              pageFunction: 'function pageFunction(context) { return { title: $("h1").text(), price: $(".price").text() }; }',
            },
          },
          {
            name: 'News Article Extraction',
            actor: 'apify/web-scraper',
            description: 'Extract article headlines and content',
            input_template: {
              startUrls: [{ url: 'https://news-site.com' }],
              pageFunction: 'function pageFunction(context) { return { headline: $(".headline").text(), content: $(".article-body").text() }; }',
            },
          },
          {
            name: 'Social Media Profile Data',
            actor: 'apify/instagram-scraper',
            description: 'Extract profile information from social platforms',
            input_template: {
              usernames: ['example_user'],
              resultsType: 'posts',
              resultsLimit: 50,
            },
          },
        ];

        return {
          contents: [
            {
              uri,
              mimeType: 'application/json',
              text: JSON.stringify({ scraping_templates: templates }, null, 2),
            },
          ],
        };
      }

      if (uri === 'apify://help/getting-started') {
        return {
          contents: [
            {
              uri,
              mimeType: 'text/plain',
              text: `Apify MCP Server - Getting Started Guide

OVERVIEW
The Apify MCP Server provides access to 5,000+ web scraping and automation actors through the Model Context Protocol. This enables AI agents to perform complex data extraction tasks.

BASIC USAGE
1. Search for actors: Use 'search_actors' to find relevant scrapers
2. Get actor info: Use 'get_actor_info' to understand actor capabilities  
3. Run actors: Use 'run_actor' to execute scraping tasks
4. Get results: Use 'get_run_status' to retrieve scraped data

POPULAR ACTORS
- apify/web-scraper: Universal web scraper for any website
- apify/instagram-scraper: Instagram posts and profiles
- apify/google-search-results-scraper: Google search results
- apify/amazon-product-scraper: Amazon product data

QUICK START EXAMPLE
1. Search: search_actors({ query: "instagram" })
2. Run: run_actor({ actor_id: "apify/instagram-scraper", input: { usernames: ["example"] } })
3. Results: Actor will return structured data from Instagram profiles

BEST PRACTICES
- Use specific actors for better performance
- Set appropriate memory and timeout limits
- Clean and validate scraped data
- Respect website terms of service
- Use rate limiting for large scraping jobs

API TOKEN
Set APIFY_API_TOKEN environment variable for full functionality.
Without token, only public actors and limited features are available.

For more information: https://docs.apify.com/`,
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
        case 'run_actor':
          return this.handleRunActor(args);
        case 'get_actor_info':
          return this.handleGetActorInfo(args);
        case 'search_actors':
          return this.handleSearchActors(args);
        case 'get_run_status':
          return this.handleGetRunStatus(args);
        case 'get_dataset_items':
          return this.handleGetDatasetItems(args);
        case 'scrape_url':
          return this.handleScrapeUrl(args);
        default:
          throw new Error(`Unknown tool: ${name}`);
      }
    });
  }

  async handleRunActor(args) {
    const {
      actor_id,
      input = {},
      memory_mbytes = this.defaultMemoryMbytes,
      timeout_secs = this.defaultTimeoutSecs,
      build,
      wait_for_finish = true,
    } = args;

    if (!this.apiToken) {
      throw new Error('Apify API token is required to run actors');
    }

    try {
      const runResult = await this.runLimit(async () => {
        const runOptions = {
          memory: memory_mbytes,
          timeout: timeout_secs,
        };

        if (build) {
          runOptions.build = build;
        }

        const run = await this.client.actor(actor_id).call(input, runOptions);
        
        if (wait_for_finish) {
          // Wait for the run to finish and get results
          const dataset = await this.client.dataset(run.defaultDatasetId);
          const { items } = await dataset.listItems();
          
          return {
            run_id: run.id,
            status: run.status,
            started_at: run.startedAt,
            finished_at: run.finishedAt,
            stats: run.stats,
            dataset_id: run.defaultDatasetId,
            items_count: items.length,
            items: items.slice(0, 100), // Limit to first 100 items
            actor_info: {
              id: actor_id,
              memory_mbytes,
              timeout_secs,
            },
          };
        } else {
          return {
            run_id: run.id,
            status: run.status,
            started_at: run.startedAt,
            actor_info: {
              id: actor_id,
              memory_mbytes,
              timeout_secs,
            },
            message: 'Actor started. Use get_run_status to check progress.',
          };
        }
      });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify(runResult, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Failed to run actor ${actor_id}: ${error.message}`);
    }
  }

  async handleGetActorInfo(args) {
    const { actor_id } = args;

    try {
      const actor = await this.apiLimit(async () => {
        return await this.client.actor(actor_id).get();
      });

      const actorInfo = {
        id: actor.id,
        name: actor.name,
        username: actor.username,
        description: actor.description,
        public: actor.isPublic,
        deprecated: actor.isDeprecated,
        stats: {
          runs: actor.stats?.totalRuns || 0,
          runs_last_30_days: actor.stats?.totalRunsLast30Days || 0,
          users: actor.stats?.totalUsers || 0,
        },
        example_run_input: actor.exampleRunInput,
        input_schema: actor.inputSchema,
        output_schema: actor.outputSchema,
        created_at: actor.createdAt,
        modified_at: actor.modifiedAt,
        readme: actor.readme?.length > 1000 ? 
          actor.readme.substring(0, 1000) + '...' : 
          actor.readme,
      };

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify(actorInfo, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Failed to get actor info for ${actor_id}: ${error.message}`);
    }
  }

  async handleSearchActors(args) {
    const {
      query,
      category,
      limit = 20,
      sort_by = 'relevance',
    } = args;

    try {
      const searchOptions = {
        search: query,
        limit,
        offset: 0,
      };

      if (category) {
        searchOptions.category = category;
      }

      // Map sort_by to Apify API format
      const sortMap = {
        relevance: 'relevance',
        popularity: 'totalRuns',
        newest: 'createdAt',
        name: 'name',
      };
      searchOptions.sortBy = sortMap[sort_by] || 'relevance';

      const searchResult = await this.apiLimit(async () => {
        return await this.client.actors().list(searchOptions);
      });

      const actors = searchResult.items.map(actor => ({
        id: actor.id,
        name: actor.name,
        username: actor.username,
        description: actor.description?.substring(0, 200) || '',
        category: actor.categories?.[0] || 'OTHER',
        stats: {
          runs: actor.stats?.totalRuns || 0,
          users: actor.stats?.totalUsers || 0,
        },
        public: actor.isPublic,
        deprecated: actor.isDeprecated,
        created_at: actor.createdAt,
      }));

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              query,
              category,
              total_results: searchResult.total,
              limit,
              sort_by,
              actors,
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Failed to search actors: ${error.message}`);
    }
  }

  async handleGetRunStatus(args) {
    const {
      run_id,
      fetch_items = true,
      items_limit = 100,
    } = args;

    if (!this.apiToken) {
      throw new Error('Apify API token is required to get run status');
    }

    try {
      const runInfo = await this.apiLimit(async () => {
        return await this.client.run(run_id).get();
      });

      let items = [];
      if (fetch_items && runInfo.defaultDatasetId && runInfo.status === 'SUCCEEDED') {
        const dataset = await this.client.dataset(runInfo.defaultDatasetId);
        const datasetItems = await dataset.listItems({ limit: items_limit });
        items = datasetItems.items;
      }

      const result = {
        run_id: runInfo.id,
        status: runInfo.status,
        started_at: runInfo.startedAt,
        finished_at: runInfo.finishedAt,
        duration_seconds: runInfo.finishedAt && runInfo.startedAt ? 
          Math.round((new Date(runInfo.finishedAt) - new Date(runInfo.startedAt)) / 1000) : null,
        stats: runInfo.stats,
        usage: {
          memory_mbytes: runInfo.options?.memory,
          timeout_secs: runInfo.options?.timeout,
        },
        dataset_id: runInfo.defaultDatasetId,
        items_count: items.length,
        items: items,
        actor_id: runInfo.actId,
      };

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify(result, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Failed to get run status for ${run_id}: ${error.message}`);
    }
  }

  async handleGetDatasetItems(args) {
    const {
      dataset_id,
      limit = 100,
      offset = 0,
      format = 'json',
      clean = false,
    } = args;

    if (!this.apiToken) {
      throw new Error('Apify API token is required to access datasets');
    }

    try {
      const dataset = await this.client.dataset(dataset_id);
      const result = await this.apiLimit(async () => {
        return await dataset.listItems({
          limit,
          offset,
          clean,
        });
      });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              dataset_id,
              format,
              limit,
              offset,
              clean,
              items_count: result.items.length,
              total_count: result.total,
              items: result.items,
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Failed to get dataset items for ${dataset_id}: ${error.message}`);
    }
  }

  async handleScrapeUrl(args) {
    const {
      urls,
      extract_data = {},
      javascript_enabled = true,
      max_pages = 1,
    } = args;

    if (!this.apiToken) {
      throw new Error('Apify API token is required for web scraping');
    }

    try {
      // Prepare input for Web Scraper actor
      const input = {
        startUrls: urls.map(url => ({ url })),
        linkSelector: 'a[href]',
        globs: [{ glob: '**' }],
        pseudoUrls: [],
        pageFunction: this.generatePageFunction(extract_data),
        proxyConfiguration: { useApifyProxy: true },
        maxRequestsPerCrawl: urls.length * max_pages,
        maxConcurrency: 1,
        requestsFromUrl: '',
        additionalMimeTypes: [],
        suggestResponseEncoding: '',
        forceResponseEncoding: '',
        ignoreSSLErrors: false,
        ignoreCorsAndCSP: true,
        downloadMedia: false,
        downloadCss: false,
        waitUntil: ['networkidle2'],
        breakpointLocation: 'NONE',
        browserLog: false,
        injectJQuery: true,
        injectUnderscore: false,
        customData: {},
      };

      const runResult = await this.runLimit(async () => {
        const run = await this.client.actor('apify/web-scraper').call(input, {
          memory: 1024,
          timeout: 300,
        });
        
        const dataset = await this.client.dataset(run.defaultDatasetId);
        const { items } = await dataset.listItems();
        
        return {
          run_id: run.id,
          urls_scraped: urls,
          items_count: items.length,
          items: items,
          extraction_config: extract_data,
          settings: {
            javascript_enabled,
            max_pages,
          },
        };
      });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify(runResult, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Failed to scrape URLs: ${error.message}`);
    }
  }

  generatePageFunction(extractData) {
    const selectors = Object.entries(extractData).map(([key, selector]) => {
      return `${key}: $('${selector}').text().trim()`;
    }).join(',\n    ');

    if (selectors) {
      return `function pageFunction(context) {
  const { page, request, log } = context;
  
  return {
    url: request.url,
    title: $('title').text(),
    ${selectors}
  };
}`;
    } else {
      return `function pageFunction(context) {
  const { page, request, log } = context;
  
  return {
    url: request.url,
    title: $('title').text(),
    content: $('body').text().trim().substring(0, 1000)
  };
}`;
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
  .option('api-token', {
    type: 'string',
    description: 'Apify API token',
    default: process.env.APIFY_API_TOKEN,
  })
  .option('default-memory', {
    type: 'number',
    description: 'Default memory allocation in MB',
    default: 512,
  })
  .option('default-timeout', {
    type: 'number',
    description: 'Default timeout in seconds',
    default: 300,
  })
  .help()
  .argv;

// Start the server
const server = new ApifyMCPServer({
  apiToken: argv.apiToken,
  defaultMemoryMbytes: argv.defaultMemory,
  defaultTimeoutSecs: argv.defaultTimeout,
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
  console.error('Failed to start Apify MCP server:', error);
  process.exit(1);
});