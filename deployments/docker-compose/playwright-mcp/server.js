#!/usr/bin/env node

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  ListResourcesRequestSchema,
  ReadResourceRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';
import { chromium } from 'playwright';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';
import dotenv from 'dotenv';

dotenv.config();

class PlaywrightMCPServer {
  constructor(options = {}) {
    this.headless = options.headless !== false;
    this.timeout = options.timeout || 30000;
    this.viewport = options.viewport || { width: 1280, height: 720 };
    this.userAgent = options.userAgent || 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';

    // Browser and context management
    this.browser = null;
    this.context = null;
    this.page = null;

    this.server = new Server(
      {
        name: 'playwright-mcp-server',
        version: process.env.PLAYWRIGHT_MCP_VERSION || '1.0.0',
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

  async ensureBrowser() {
    if (!this.browser) {
      this.browser = await chromium.launch({
        headless: this.headless,
        args: [
          '--no-sandbox',
          '--disable-setuid-sandbox',
          '--disable-dev-shm-usage',
          '--disable-gpu',
        ],
      });
    }

    if (!this.context) {
      this.context = await this.browser.newContext({
        viewport: this.viewport,
        userAgent: this.userAgent,
      });
    }

    if (!this.page) {
      this.page = await this.context.newPage();
      this.page.setDefaultTimeout(this.timeout);
    }

    return this.page;
  }

  setupHandlers() {
    // List available tools
    this.server.setRequestHandler(ListToolsRequestSchema, async () => {
      return {
        tools: [
          {
            name: 'navigate',
            description: 'Navigate to a URL in the browser',
            inputSchema: {
              type: 'object',
              properties: {
                url: {
                  type: 'string',
                  description: 'The URL to navigate to',
                  format: 'uri',
                },
                wait_until: {
                  type: 'string',
                  description: 'When to consider navigation complete',
                  enum: ['load', 'domcontentloaded', 'networkidle', 'commit'],
                  default: 'load',
                },
              },
              required: ['url'],
            },
          },
          {
            name: 'screenshot',
            description: 'Take a screenshot of the current page or a specific element',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for element to screenshot (optional, captures full page if not provided)',
                },
                full_page: {
                  type: 'boolean',
                  description: 'Capture full scrollable page',
                  default: false,
                },
                type: {
                  type: 'string',
                  description: 'Image format',
                  enum: ['png', 'jpeg'],
                  default: 'png',
                },
                quality: {
                  type: 'integer',
                  description: 'Quality for jpeg (0-100)',
                  minimum: 0,
                  maximum: 100,
                },
              },
            },
          },
          {
            name: 'click',
            description: 'Click on an element',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for the element to click',
                },
                button: {
                  type: 'string',
                  description: 'Mouse button to use',
                  enum: ['left', 'right', 'middle'],
                  default: 'left',
                },
                click_count: {
                  type: 'integer',
                  description: 'Number of clicks',
                  default: 1,
                  minimum: 1,
                  maximum: 3,
                },
                delay: {
                  type: 'integer',
                  description: 'Delay between mousedown and mouseup in ms',
                  default: 0,
                },
              },
              required: ['selector'],
            },
          },
          {
            name: 'fill',
            description: 'Fill a form field with text',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for the input element',
                },
                value: {
                  type: 'string',
                  description: 'Text value to fill',
                },
                clear_first: {
                  type: 'boolean',
                  description: 'Clear the field before filling',
                  default: true,
                },
              },
              required: ['selector', 'value'],
            },
          },
          {
            name: 'type',
            description: 'Type text with keyboard simulation (supports special keys)',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for the element to type into',
                },
                text: {
                  type: 'string',
                  description: 'Text to type',
                },
                delay: {
                  type: 'integer',
                  description: 'Delay between key presses in ms',
                  default: 0,
                },
              },
              required: ['selector', 'text'],
            },
          },
          {
            name: 'select',
            description: 'Select option(s) from a dropdown',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for the select element',
                },
                value: {
                  type: 'string',
                  description: 'Value attribute of option to select',
                },
                label: {
                  type: 'string',
                  description: 'Label text of option to select',
                },
                index: {
                  type: 'integer',
                  description: 'Index of option to select (0-based)',
                },
              },
              required: ['selector'],
            },
          },
          {
            name: 'hover',
            description: 'Hover over an element',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for the element to hover over',
                },
              },
              required: ['selector'],
            },
          },
          {
            name: 'wait_for_selector',
            description: 'Wait for an element to appear on the page',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector to wait for',
                },
                state: {
                  type: 'string',
                  description: 'Element state to wait for',
                  enum: ['attached', 'detached', 'visible', 'hidden'],
                  default: 'visible',
                },
                timeout: {
                  type: 'integer',
                  description: 'Timeout in milliseconds',
                  default: 30000,
                },
              },
              required: ['selector'],
            },
          },
          {
            name: 'get_text',
            description: 'Get text content of an element',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for the element',
                },
              },
              required: ['selector'],
            },
          },
          {
            name: 'get_attribute',
            description: 'Get an attribute value from an element',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for the element',
                },
                attribute: {
                  type: 'string',
                  description: 'Attribute name to get',
                },
              },
              required: ['selector', 'attribute'],
            },
          },
          {
            name: 'evaluate',
            description: 'Execute JavaScript in the browser context',
            inputSchema: {
              type: 'object',
              properties: {
                script: {
                  type: 'string',
                  description: 'JavaScript code to execute',
                },
              },
              required: ['script'],
            },
          },
          {
            name: 'get_page_content',
            description: 'Get the HTML content of the current page',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for specific element (optional, gets full page if not provided)',
                },
              },
            },
          },
          {
            name: 'get_page_info',
            description: 'Get information about the current page (URL, title, etc.)',
            inputSchema: {
              type: 'object',
              properties: {},
            },
          },
          {
            name: 'go_back',
            description: 'Navigate back in browser history',
            inputSchema: {
              type: 'object',
              properties: {
                wait_until: {
                  type: 'string',
                  description: 'When to consider navigation complete',
                  enum: ['load', 'domcontentloaded', 'networkidle', 'commit'],
                  default: 'load',
                },
              },
            },
          },
          {
            name: 'go_forward',
            description: 'Navigate forward in browser history',
            inputSchema: {
              type: 'object',
              properties: {
                wait_until: {
                  type: 'string',
                  description: 'When to consider navigation complete',
                  enum: ['load', 'domcontentloaded', 'networkidle', 'commit'],
                  default: 'load',
                },
              },
            },
          },
          {
            name: 'reload',
            description: 'Reload the current page',
            inputSchema: {
              type: 'object',
              properties: {
                wait_until: {
                  type: 'string',
                  description: 'When to consider reload complete',
                  enum: ['load', 'domcontentloaded', 'networkidle', 'commit'],
                  default: 'load',
                },
              },
            },
          },
          {
            name: 'set_viewport',
            description: 'Set the browser viewport size',
            inputSchema: {
              type: 'object',
              properties: {
                width: {
                  type: 'integer',
                  description: 'Viewport width in pixels',
                  minimum: 320,
                  maximum: 3840,
                },
                height: {
                  type: 'integer',
                  description: 'Viewport height in pixels',
                  minimum: 240,
                  maximum: 2160,
                },
              },
              required: ['width', 'height'],
            },
          },
          {
            name: 'scroll',
            description: 'Scroll the page or an element',
            inputSchema: {
              type: 'object',
              properties: {
                selector: {
                  type: 'string',
                  description: 'CSS selector for element to scroll (optional, scrolls page if not provided)',
                },
                x: {
                  type: 'integer',
                  description: 'Horizontal scroll amount in pixels',
                  default: 0,
                },
                y: {
                  type: 'integer',
                  description: 'Vertical scroll amount in pixels',
                  default: 0,
                },
              },
            },
          },
          {
            name: 'press_key',
            description: 'Press a keyboard key',
            inputSchema: {
              type: 'object',
              properties: {
                key: {
                  type: 'string',
                  description: 'Key to press (e.g., "Enter", "Tab", "Escape", "ArrowDown")',
                },
                modifiers: {
                  type: 'array',
                  items: {
                    type: 'string',
                    enum: ['Alt', 'Control', 'Meta', 'Shift'],
                  },
                  description: 'Modifier keys to hold while pressing',
                },
              },
              required: ['key'],
            },
          },
          {
            name: 'close_browser',
            description: 'Close the browser instance',
            inputSchema: {
              type: 'object',
              properties: {},
            },
          },
        ],
      };
    });

    // List available resources
    this.server.setRequestHandler(ListResourcesRequestSchema, async () => {
      return {
        resources: [
          {
            uri: 'playwright://browser/status',
            name: 'Browser Status',
            description: 'Current browser and page status',
            mimeType: 'application/json',
          },
          {
            uri: 'playwright://help/selectors',
            name: 'Selector Help',
            description: 'Guide for CSS selectors and Playwright locators',
            mimeType: 'text/plain',
          },
        ],
      };
    });

    // Read a specific resource
    this.server.setRequestHandler(ReadResourceRequestSchema, async (request) => {
      const { uri } = request.params;

      if (uri === 'playwright://help/selectors') {
        return {
          contents: [
            {
              uri,
              mimeType: 'text/plain',
              text: `Playwright Selector Guide:

CSS Selectors:
- By ID: "#myId"
- By class: ".myClass"
- By tag: "div"
- By attribute: "[data-testid='value']"
- Combined: "div.class#id"
- Descendant: "div .child"
- Direct child: "div > .child"
- Nth child: "li:nth-child(2)"

Text Selectors:
- Exact text: "text=Submit"
- Contains text: "text=Submit" (partial match)
- Case insensitive: "text=submit" >> visible=true

Role Selectors:
- By role: "role=button[name='Submit']"
- Links: "role=link[name='Click here']"
- Headings: "role=heading[level=1]"

Placeholder/Label:
- By placeholder: "[placeholder='Enter email']"
- By label: "label:has-text('Email') >> input"

Best Practices:
1. Prefer data-testid attributes for test stability
2. Use role selectors for accessibility
3. Avoid brittle selectors based on classes that may change
4. Use visible=true to match only visible elements
5. Combine selectors for precision: "div.container >> button.submit"`,
            },
          ],
        };
      }

      if (uri === 'playwright://browser/status') {
        return {
          contents: [
            {
              uri,
              mimeType: 'application/json',
              text: JSON.stringify({
                browser_connected: !!this.browser,
                context_created: !!this.context,
                page_created: !!this.page,
                current_url: this.page ? await this.page.url() : null,
                current_title: this.page ? await this.page.title() : null,
                viewport: this.viewport,
                headless: this.headless,
                timestamp: new Date().toISOString(),
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
        case 'navigate':
          return this.handleNavigate(args);
        case 'screenshot':
          return this.handleScreenshot(args);
        case 'click':
          return this.handleClick(args);
        case 'fill':
          return this.handleFill(args);
        case 'type':
          return this.handleType(args);
        case 'select':
          return this.handleSelect(args);
        case 'hover':
          return this.handleHover(args);
        case 'wait_for_selector':
          return this.handleWaitForSelector(args);
        case 'get_text':
          return this.handleGetText(args);
        case 'get_attribute':
          return this.handleGetAttribute(args);
        case 'evaluate':
          return this.handleEvaluate(args);
        case 'get_page_content':
          return this.handleGetPageContent(args);
        case 'get_page_info':
          return this.handleGetPageInfo(args);
        case 'go_back':
          return this.handleGoBack(args);
        case 'go_forward':
          return this.handleGoForward(args);
        case 'reload':
          return this.handleReload(args);
        case 'set_viewport':
          return this.handleSetViewport(args);
        case 'scroll':
          return this.handleScroll(args);
        case 'press_key':
          return this.handlePressKey(args);
        case 'close_browser':
          return this.handleCloseBrowser();
        default:
          throw new Error(`Unknown tool: ${name}`);
      }
    });
  }

  async handleNavigate(args) {
    const { url, wait_until = 'load' } = args;

    try {
      const page = await this.ensureBrowser();
      const response = await page.goto(url, { waitUntil: wait_until });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              url: page.url(),
              title: await page.title(),
              status: response?.status() || null,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Navigation failed: ${error.message}`);
    }
  }

  async handleScreenshot(args) {
    const { selector, full_page = false, type = 'png', quality } = args;

    try {
      const page = await this.ensureBrowser();

      const screenshotOptions = {
        type,
        fullPage: full_page,
      };

      if (type === 'jpeg' && quality) {
        screenshotOptions.quality = quality;
      }

      let buffer;
      if (selector) {
        const element = await page.locator(selector);
        buffer = await element.screenshot(screenshotOptions);
      } else {
        buffer = await page.screenshot(screenshotOptions);
      }

      const base64 = buffer.toString('base64');

      return {
        content: [
          {
            type: 'image',
            data: base64,
            mimeType: type === 'jpeg' ? 'image/jpeg' : 'image/png',
          },
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              selector: selector || 'full page',
              full_page,
              type,
              size_bytes: buffer.length,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Screenshot failed: ${error.message}`);
    }
  }

  async handleClick(args) {
    const { selector, button = 'left', click_count = 1, delay = 0 } = args;

    try {
      const page = await this.ensureBrowser();
      await page.click(selector, { button, clickCount: click_count, delay });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'click',
              selector,
              button,
              click_count,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Click failed: ${error.message}`);
    }
  }

  async handleFill(args) {
    const { selector, value, clear_first = true } = args;

    try {
      const page = await this.ensureBrowser();

      if (clear_first) {
        await page.fill(selector, '');
      }
      await page.fill(selector, value);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'fill',
              selector,
              value_length: value.length,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Fill failed: ${error.message}`);
    }
  }

  async handleType(args) {
    const { selector, text, delay = 0 } = args;

    try {
      const page = await this.ensureBrowser();
      await page.locator(selector).pressSequentially(text, { delay });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'type',
              selector,
              text_length: text.length,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Type failed: ${error.message}`);
    }
  }

  async handleSelect(args) {
    const { selector, value, label, index } = args;

    try {
      const page = await this.ensureBrowser();

      let selected;
      if (value !== undefined) {
        selected = await page.selectOption(selector, { value });
      } else if (label !== undefined) {
        selected = await page.selectOption(selector, { label });
      } else if (index !== undefined) {
        selected = await page.selectOption(selector, { index });
      } else {
        throw new Error('Must provide value, label, or index');
      }

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'select',
              selector,
              selected_values: selected,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Select failed: ${error.message}`);
    }
  }

  async handleHover(args) {
    const { selector } = args;

    try {
      const page = await this.ensureBrowser();
      await page.hover(selector);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'hover',
              selector,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Hover failed: ${error.message}`);
    }
  }

  async handleWaitForSelector(args) {
    const { selector, state = 'visible', timeout = 30000 } = args;

    try {
      const page = await this.ensureBrowser();
      await page.waitForSelector(selector, { state, timeout });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'wait_for_selector',
              selector,
              state,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Wait for selector failed: ${error.message}`);
    }
  }

  async handleGetText(args) {
    const { selector } = args;

    try {
      const page = await this.ensureBrowser();
      const text = await page.textContent(selector);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              selector,
              text,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Get text failed: ${error.message}`);
    }
  }

  async handleGetAttribute(args) {
    const { selector, attribute } = args;

    try {
      const page = await this.ensureBrowser();
      const value = await page.getAttribute(selector, attribute);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              selector,
              attribute,
              value,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Get attribute failed: ${error.message}`);
    }
  }

  async handleEvaluate(args) {
    const { script } = args;

    try {
      const page = await this.ensureBrowser();
      const result = await page.evaluate(script);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              result,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Evaluate failed: ${error.message}`);
    }
  }

  async handleGetPageContent(args) {
    const { selector } = args;

    try {
      const page = await this.ensureBrowser();

      let content;
      if (selector) {
        content = await page.locator(selector).innerHTML();
      } else {
        content = await page.content();
      }

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              selector: selector || 'full page',
              content_length: content.length,
              content: content.length > 50000 ? content.substring(0, 50000) + '...' : content,
              truncated: content.length > 50000,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Get page content failed: ${error.message}`);
    }
  }

  async handleGetPageInfo(args) {
    try {
      const page = await this.ensureBrowser();

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              url: page.url(),
              title: await page.title(),
              viewport: page.viewportSize(),
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Get page info failed: ${error.message}`);
    }
  }

  async handleGoBack(args) {
    const { wait_until = 'load' } = args;

    try {
      const page = await this.ensureBrowser();
      const response = await page.goBack({ waitUntil: wait_until });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'go_back',
              url: page.url(),
              status: response?.status() || null,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Go back failed: ${error.message}`);
    }
  }

  async handleGoForward(args) {
    const { wait_until = 'load' } = args;

    try {
      const page = await this.ensureBrowser();
      const response = await page.goForward({ waitUntil: wait_until });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'go_forward',
              url: page.url(),
              status: response?.status() || null,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Go forward failed: ${error.message}`);
    }
  }

  async handleReload(args) {
    const { wait_until = 'load' } = args;

    try {
      const page = await this.ensureBrowser();
      const response = await page.reload({ waitUntil: wait_until });

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'reload',
              url: page.url(),
              status: response?.status() || null,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Reload failed: ${error.message}`);
    }
  }

  async handleSetViewport(args) {
    const { width, height } = args;

    try {
      const page = await this.ensureBrowser();
      await page.setViewportSize({ width, height });
      this.viewport = { width, height };

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'set_viewport',
              viewport: { width, height },
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Set viewport failed: ${error.message}`);
    }
  }

  async handleScroll(args) {
    const { selector, x = 0, y = 0 } = args;

    try {
      const page = await this.ensureBrowser();

      if (selector) {
        await page.locator(selector).evaluate((el, { x, y }) => {
          el.scrollBy(x, y);
        }, { x, y });
      } else {
        await page.evaluate(({ x, y }) => {
          window.scrollBy(x, y);
        }, { x, y });
      }

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'scroll',
              selector: selector || 'window',
              x,
              y,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Scroll failed: ${error.message}`);
    }
  }

  async handlePressKey(args) {
    const { key, modifiers = [] } = args;

    try {
      const page = await this.ensureBrowser();

      let keyCombo = key;
      if (modifiers.length > 0) {
        keyCombo = [...modifiers, key].join('+');
      }

      await page.keyboard.press(keyCombo);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'press_key',
              key: keyCombo,
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Press key failed: ${error.message}`);
    }
  }

  async handleCloseBrowser() {
    try {
      if (this.page) {
        await this.page.close();
        this.page = null;
      }
      if (this.context) {
        await this.context.close();
        this.context = null;
      }
      if (this.browser) {
        await this.browser.close();
        this.browser = null;
      }

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              success: true,
              action: 'close_browser',
              timestamp: new Date().toISOString(),
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      throw new Error(`Close browser failed: ${error.message}`);
    }
  }

  async start() {
    const transport = new StdioServerTransport();
    await this.server.connect(transport);
  }

  async close() {
    await this.handleCloseBrowser();
  }
}

// CLI argument parsing
const argv = yargs(hideBin(process.argv))
  .option('headless', {
    type: 'boolean',
    description: 'Run browser in headless mode',
    default: true,
  })
  .option('timeout', {
    type: 'number',
    description: 'Default timeout in milliseconds',
    default: 30000,
  })
  .option('viewport-width', {
    type: 'number',
    description: 'Default viewport width',
    default: 1280,
  })
  .option('viewport-height', {
    type: 'number',
    description: 'Default viewport height',
    default: 720,
  })
  .help()
  .argv;

// Start the server
const server = new PlaywrightMCPServer({
  headless: argv.headless,
  timeout: argv.timeout,
  viewport: {
    width: argv.viewportWidth,
    height: argv.viewportHeight,
  },
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
  console.error('Failed to start Playwright MCP server:', error);
  process.exit(1);
});
