#!/bin/sh

set -e

echo "🐘 Starting PostgreSQL MCP Server v${POSTGRES_MCP_VERSION:-1.0.0}"
echo "📊 Database: ${DATABASE_URL:-postgresql://postgres:password@localhost:5432/postgres}"
echo "🔒 Read-only mode: ${READ_ONLY:-true}"
echo "🔌 Max connections: ${MAX_CONNECTIONS:-10}"

# Health check endpoint setup (for Docker health checks)
if [ "$HEALTH_CHECK_ENABLED" = "true" ]; then
    echo "🏥 Health check enabled on port ${HEALTH_PORT:-3401}"
    
    # Create simple health check server
    cat > health-server.js << 'HEALTH_EOF'
import http from 'http';
import pg from 'pg';

const { Pool } = pg;
const pool = new Pool({
  connectionString: process.env.DATABASE_URL,
  max: 1,
});

const server = http.createServer(async (req, res) => {
  if (req.url === '/health' && req.method === 'GET') {
    try {
      const client = await pool.connect();
      await client.query('SELECT 1');
      client.release();
      
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({
        status: 'healthy',
        service: 'postgres-mcp-server',
        version: process.env.POSTGRES_MCP_VERSION || '1.0.0',
        timestamp: new Date().toISOString(),
        database: 'connected'
      }));
    } catch (error) {
      res.writeHead(503, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({
        status: 'unhealthy',
        service: 'postgres-mcp-server',
        error: error.message,
        timestamp: new Date().toISOString()
      }));
    }
  } else {
    res.writeHead(404);
    res.end('Not Found');
  }
});

const port = process.env.HEALTH_PORT || 3401;
server.listen(port, '0.0.0.0', () => {
  console.log(`Health check server running on port ${port}`);
});
HEALTH_EOF

    # Start health server in background
    node health-server.js &
fi

# Wait for database to be ready
echo "⏳ Waiting for database connection..."
timeout=30
count=0

while [ $count -lt $timeout ]; do
    if node -e "
        import pg from 'pg';
        const { Pool } = pg;
        const pool = new Pool({ connectionString: process.env.DATABASE_URL });
        pool.connect()
            .then(client => { client.release(); console.log('Database ready'); process.exit(0); })
            .catch(() => process.exit(1));
    " 2>/dev/null; then
        echo "✅ Database connection established"
        break
    fi
    
    count=$((count + 1))
    echo "⏳ Waiting for database... ($count/$timeout)"
    sleep 1
done

if [ $count -eq $timeout ]; then
    echo "❌ Database connection timeout after ${timeout}s"
    exit 1
fi

# Start the main PostgreSQL MCP server
echo "🚀 Starting PostgreSQL MCP server..."
exec node server.js "$@"