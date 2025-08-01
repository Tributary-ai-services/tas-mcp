#!/bin/sh
# Simple health check script for Docker container

set -e

# Check if the server is responding on the health check port
if command -v wget >/dev/null 2>&1; then
    wget --no-verbose --tries=1 --spider "http://localhost:${HEALTH_CHECK_PORT:-8082}/health" || exit 1
elif command -v curl >/dev/null 2>&1; then
    curl -f "http://localhost:${HEALTH_CHECK_PORT:-8082}/health" > /dev/null || exit 1
else
    # Fallback: try to connect to the port
    nc -z localhost "${HEALTH_CHECK_PORT:-8082}" || exit 1
fi

echo "Health check passed"
exit 0