#!/bin/bash

set -e

echo "🚀 Starting Git MCP Server"
echo "=========================="
echo "Port: ${MCP_PORT}"
echo "Host: ${MCP_HOST}"
echo "Repository Path: ${REPOSITORY_PATH}"
echo "Log Level: ${LOG_LEVEL}"

# Create a default test repository if none exists
if [ ! -d "${REPOSITORY_PATH}/.git" ] && [ -z "$(ls -A ${REPOSITORY_PATH} 2>/dev/null)" ]; then
    echo "📁 Creating default test repository..."
    cd "${REPOSITORY_PATH}"
    git init
    git config user.name "Git MCP Server"
    git config user.email "git-mcp@tributary-ai.services"
    
    # Create initial files
    echo "# Git MCP Server Test Repository" > README.md
    echo "This repository was created automatically by Git MCP Server for testing purposes." >> README.md
    echo "" >> README.md
    echo "## Available Operations" >> README.md
    echo "- git_status" >> README.md
    echo "- git_diff_unstaged" >> README.md
    echo "- git_diff_staged" >> README.md
    echo "- git_commit" >> README.md
    echo "- git_add" >> README.md
    echo "- git_reset" >> README.md
    echo "- git_log" >> README.md
    echo "- git_create_branch" >> README.md
    echo "- git_checkout" >> README.md
    
    echo "print('Hello from Git MCP Server!')" > hello.py
    echo "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from Git MCP Server!\")\n}" > hello.go
    
    git add .
    git commit -m "Initial commit: Add test files for Git MCP operations"
    
    # Create a development branch
    git checkout -b development
    echo "# Development Branch" > dev-notes.md
    echo "This branch is for testing branch operations." >> dev-notes.md
    git add dev-notes.md
    git commit -m "Add development branch with notes"
    git checkout main
    
    echo "✅ Default test repository created with sample files and branches"
fi

# Verify git repository
if [ -d "${REPOSITORY_PATH}/.git" ]; then
    echo "✅ Git repository found at ${REPOSITORY_PATH}"
    cd "${REPOSITORY_PATH}"
    echo "📊 Repository status:"
    git log --oneline -5 || echo "No commits found"
    git branch -a || echo "No branches found"
else
    echo "⚠️  No git repository found at ${REPOSITORY_PATH}"
fi

# Create a simple health check endpoint if the server doesn't have one
cat > /tmp/health_server.py << 'EOF'
#!/usr/bin/env python3
"""
Simple health check server for Git MCP Server
Runs alongside the main MCP server to provide health checks
"""
import asyncio
import json
from http.server import HTTPServer, BaseHTTPRequestHandler
import threading
import os

class HealthHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-type', 'application/json')
            self.end_headers()
            
            health_data = {
                "status": "healthy",
                "service": "git-mcp-server",
                "version": "1.0.0",
                "repository_path": os.environ.get('REPOSITORY_PATH', '/repositories'),
                "timestamp": "2025-08-04T00:00:00Z"
            }
            
            self.wfile.write(json.dumps(health_data).encode())
        else:
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b'Not Found')
    
    def log_message(self, format, *args):
        # Suppress default logging
        pass

def run_health_server():
    server = HTTPServer(('0.0.0.0', 3001), HealthHandler)
    server.serve_forever()

if __name__ == '__main__':
    thread = threading.Thread(target=run_health_server)
    thread.daemon = True
    thread.start()
    print("Health server started on port 3001")
    
    # Keep the script running
    try:
        while True:
            asyncio.sleep(1)
    except KeyboardInterrupt:
        pass
EOF

# Start health check server in background
python3 /tmp/health_server.py &
HEALTH_PID=$!

# Cleanup function
cleanup() {
    echo "🛑 Shutting down Git MCP Server..."
    if [ ! -z "$HEALTH_PID" ]; then
        kill $HEALTH_PID 2>/dev/null || true
    fi
    exit 0
}

# Set up signal handlers
trap cleanup SIGTERM SIGINT

echo "🔄 Starting Git MCP Server..."
echo "Repository: ${REPOSITORY_PATH}"
echo "Listening on: ${MCP_HOST}:${MCP_PORT}"
echo "Health check: http://localhost:3001/health"

# Execute the main command with all arguments
exec "$@" \
    --repository "${REPOSITORY_PATH}" \
    --port "${MCP_PORT}" \
    --host "${MCP_HOST}" \
    --log-level "${LOG_LEVEL}"