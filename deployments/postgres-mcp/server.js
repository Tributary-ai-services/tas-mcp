#!/usr/bin/env node

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  ListResourcesRequestSchema,
  ReadResourceRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';
import pg from 'pg';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';
import dotenv from 'dotenv';

dotenv.config();

const { Pool } = pg;

class PostgreSQLMCPServer {
  constructor(connectionString, options = {}) {
    this.connectionString = connectionString;
    this.readOnly = options.readOnly !== false; // Default to read-only
    this.maxConnections = options.maxConnections || 10;
    this.queryTimeout = options.queryTimeout || 30000;
    
    this.pool = new Pool({
      connectionString: this.connectionString,
      max: this.maxConnections,
      idleTimeoutMillis: 30000,
      connectionTimeoutMillis: 2000,
    });

    this.server = new Server(
      {
        name: 'postgres-mcp-server',
        version: process.env.POSTGRES_MCP_VERSION || '1.0.0',
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
            name: 'query',
            description: 'Execute a read-only SQL query against the PostgreSQL database',
            inputSchema: {
              type: 'object',
              properties: {
                sql: {
                  type: 'string',
                  description: 'The SQL query to execute (SELECT statements only)',
                },
                params: {
                  type: 'array',
                  description: 'Optional parameters for parameterized queries',
                  items: { type: 'string' },
                },
              },
              required: ['sql'],
            },
          },
          {
            name: 'describe_table',
            description: 'Get detailed schema information for a specific table',
            inputSchema: {
              type: 'object',
              properties: {
                table_name: {
                  type: 'string',
                  description: 'Name of the table to describe',
                },
                schema_name: {
                  type: 'string',
                  description: 'Schema name (defaults to public)',
                  default: 'public',
                },
              },
              required: ['table_name'],
            },
          },
          {
            name: 'list_tables',
            description: 'List all tables in the database with basic information',
            inputSchema: {
              type: 'object',
              properties: {
                schema_name: {
                  type: 'string',
                  description: 'Schema name to filter (optional)',
                },
              },
            },
          },
          {
            name: 'analyze_query',
            description: 'Analyze a query execution plan without executing it',
            inputSchema: {
              type: 'object',
              properties: {
                sql: {
                  type: 'string',
                  description: 'The SQL query to analyze',
                },
              },
              required: ['sql'],
            },
          },
        ],
      };
    });

    // List available resources
    this.server.setRequestHandler(ListResourcesRequestSchema, async () => {
      const client = await this.pool.connect();
      try {
        const result = await client.query(`
          SELECT schemaname, tablename, tableowner, hasindexes, hasrules, hastriggers
          FROM pg_tables 
          WHERE schemaname NOT IN ('information_schema', 'pg_catalog')
          ORDER BY schemaname, tablename
        `);

        return {
          resources: result.rows.map(row => ({
            uri: `postgres://table/${row.schemaname}/${row.tablename}`,
            name: `${row.schemaname}.${row.tablename}`,
            description: `PostgreSQL table: ${row.schemaname}.${row.tablename}`,
            mimeType: 'application/json',
          })),
        };
      } finally {
        client.release();
      }
    });

    // Read a specific resource
    this.server.setRequestHandler(ReadResourceRequestSchema, async (request) => {
      const { uri } = request.params;
      const match = uri.match(/^postgres:\/\/table\/([^\/]+)\/([^\/]+)$/);
      
      if (!match) {
        throw new Error(`Invalid resource URI: ${uri}`);
      }

      const [, schemaName, tableName] = match;
      const client = await this.pool.connect();
      
      try {
        // Get table schema information
        const schemaResult = await client.query(`
          SELECT 
            column_name,
            data_type,
            is_nullable,
            column_default,
            character_maximum_length,
            numeric_precision,
            numeric_scale
          FROM information_schema.columns
          WHERE table_schema = $1 AND table_name = $2
          ORDER BY ordinal_position
        `, [schemaName, tableName]);

        // Get table statistics
        const statsResult = await client.query(`
          SELECT 
            schemaname,
            tablename,
            attname,
            n_distinct,
            correlation
          FROM pg_stats
          WHERE schemaname = $1 AND tablename = $2
        `, [schemaName, tableName]);

        return {
          contents: [
            {
              uri,
              mimeType: 'application/json',
              text: JSON.stringify({
                schema: schemaName,
                table: tableName,
                columns: schemaResult.rows,
                statistics: statsResult.rows,
              }, null, 2),
            },
          ],
        };
      } finally {
        client.release();
      }
    });

    // Handle tool calls
    this.server.setRequestHandler(CallToolRequestSchema, async (request) => {
      const { name, arguments: args } = request.params;

      switch (name) {
        case 'query':
          return this.handleQuery(args);
        case 'describe_table':
          return this.handleDescribeTable(args);
        case 'list_tables':
          return this.handleListTables(args);
        case 'analyze_query':
          return this.handleAnalyzeQuery(args);
        default:
          throw new Error(`Unknown tool: ${name}`);
      }
    });
  }

  async handleQuery(args) {
    const { sql, params = [] } = args;
    
    // Security check: ensure read-only operations
    if (this.readOnly && !this.isReadOnlyQuery(sql)) {
      throw new Error('Only SELECT queries are allowed in read-only mode');
    }

    const client = await this.pool.connect();
    try {
      // Set transaction to read-only if configured
      if (this.readOnly) {
        await client.query('BEGIN READ ONLY');
      }

      const result = await client.query({
        text: sql,
        values: params,
        rowMode: 'array',
      });

      if (this.readOnly) {
        await client.query('COMMIT');
      }

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              rows: result.rows,
              fields: result.fields.map(f => ({ name: f.name, dataTypeID: f.dataTypeID })),
              rowCount: result.rowCount,
              command: result.command,
            }, null, 2),
          },
        ],
      };
    } catch (error) {
      if (this.readOnly) {
        try {
          await client.query('ROLLBACK');
        } catch (rollbackError) {
          console.error('Error during rollback:', rollbackError);
        }
      }
      throw new Error(`Query execution failed: ${error.message}`);
    } finally {
      client.release();
    }
  }

  async handleDescribeTable(args) {
    const { table_name, schema_name = 'public' } = args;
    const client = await this.pool.connect();
    
    try {
      // Get detailed table information
      const tableInfo = await client.query(`
        SELECT 
          t.table_name,
          t.table_type,
          t.table_schema,
          obj_description(c.oid) as table_comment
        FROM information_schema.tables t
        LEFT JOIN pg_class c ON c.relname = t.table_name
        LEFT JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = t.table_schema
        WHERE t.table_schema = $1 AND t.table_name = $2
      `, [schema_name, table_name]);

      if (tableInfo.rows.length === 0) {
        throw new Error(`Table ${schema_name}.${table_name} not found`);
      }

      // Get column information
      const columns = await client.query(`
        SELECT 
          column_name,
          data_type,
          is_nullable,
          column_default,
          character_maximum_length,
          numeric_precision,
          numeric_scale,
          col_description(pgc.oid, c.ordinal_position) as column_comment
        FROM information_schema.columns c
        LEFT JOIN pg_class pgc ON pgc.relname = c.table_name
        LEFT JOIN pg_namespace pgn ON pgn.oid = pgc.relnamespace AND pgn.nspname = c.table_schema
        WHERE c.table_schema = $1 AND c.table_name = $2
        ORDER BY c.ordinal_position
      `, [schema_name, table_name]);

      // Get indexes
      const indexes = await client.query(`
        SELECT 
          indexname,
          indexdef,
          indisunique,
          indisprimary
        FROM pg_indexes 
        WHERE schemaname = $1 AND tablename = $2
      `, [schema_name, table_name]);

      // Get constraints
      const constraints = await client.query(`
        SELECT 
          constraint_name,
          constraint_type,
          column_name,
          foreign_table_schema,
          foreign_table_name,
          foreign_column_name
        FROM information_schema.table_constraints tc
        LEFT JOIN information_schema.key_column_usage kcu 
          ON tc.constraint_name = kcu.constraint_name 
          AND tc.table_schema = kcu.table_schema
        LEFT JOIN information_schema.referential_constraints rc 
          ON tc.constraint_name = rc.constraint_name
        LEFT JOIN information_schema.key_column_usage fkcu 
          ON rc.unique_constraint_name = fkcu.constraint_name
        WHERE tc.table_schema = $1 AND tc.table_name = $2
      `, [schema_name, table_name]);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              table: tableInfo.rows[0],
              columns: columns.rows,
              indexes: indexes.rows,
              constraints: constraints.rows,
            }, null, 2),
          },
        ],
      };
    } finally {
      client.release();
    }
  }

  async handleListTables(args) {
    const { schema_name } = args;
    const client = await this.pool.connect();
    
    try {
      let query = `
        SELECT 
          schemaname,
          tablename,
          tableowner,
          hasindexes,
          hasrules,
          hastriggers,
          rowsecurity
        FROM pg_tables 
        WHERE schemaname NOT IN ('information_schema', 'pg_catalog')
      `;
      
      const params = [];
      if (schema_name) {
        query += ' AND schemaname = $1';
        params.push(schema_name);
      }
      
      query += ' ORDER BY schemaname, tablename';
      
      const result = await client.query(query, params);

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              tables: result.rows,
              count: result.rowCount,
            }, null, 2),
          },
        ],
      };
    } finally {
      client.release();
    }
  }

  async handleAnalyzeQuery(args) {
    const { sql } = args;
    const client = await this.pool.connect();
    
    try {
      const result = await client.query(`EXPLAIN (FORMAT JSON, ANALYZE false) ${sql}`);
      
      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              query: sql,
              plan: result.rows[0]['QUERY PLAN'],
            }, null, 2),
          },
        ],
      };
    } finally {
      client.release();
    }
  }

  isReadOnlyQuery(sql) {
    const trimmed = sql.trim().toLowerCase();
    const readOnlyPrefixes = ['select', 'with', 'show', 'explain', 'describe'];
    return readOnlyPrefixes.some(prefix => trimmed.startsWith(prefix));
  }

  async start() {
    const transport = new StdioServerTransport();
    await this.server.connect(transport);
  }

  async close() {
    await this.pool.end();
  }
}

// CLI argument parsing
const argv = yargs(hideBin(process.argv))
  .option('connection-string', {
    alias: 'c',
    type: 'string',
    description: 'PostgreSQL connection string',
    default: process.env.DATABASE_URL || 'postgresql://postgres:password@localhost:5432/postgres',
  })
  .option('read-only', {
    type: 'boolean',
    description: 'Enable read-only mode (default: true)',
    default: true,
  })
  .option('max-connections', {
    type: 'number',
    description: 'Maximum number of database connections',
    default: 10,
  })
  .option('query-timeout', {
    type: 'number',
    description: 'Query timeout in milliseconds',
    default: 30000,
  })
  .help()
  .argv;

// Start the server
const server = new PostgreSQLMCPServer(argv.connectionString, {
  readOnly: argv.readOnly,
  maxConnections: argv.maxConnections,
  queryTimeout: argv.queryTimeout,
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
  console.error('Failed to start PostgreSQL MCP server:', error);
  process.exit(1);
});