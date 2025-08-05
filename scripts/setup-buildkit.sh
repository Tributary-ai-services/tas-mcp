#!/bin/bash
# Setup Docker BuildKit for TAS MCP Server

set -e

echo "🔧 Setting up Docker BuildKit for TAS MCP Server..."

# Check Docker version
DOCKER_VERSION=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "0.0.0")
MIN_VERSION="18.09"

version_ge() {
    [ "$(printf '%s\n' "$MIN_VERSION" "$1" | sort -V | head -n1)" = "$MIN_VERSION" ]
}

if ! version_ge "$DOCKER_VERSION"; then
    echo "❌ Docker version $DOCKER_VERSION is too old. BuildKit requires Docker $MIN_VERSION or newer."
    exit 1
fi

echo "✅ Docker version $DOCKER_VERSION supports BuildKit"

# Enable BuildKit
echo "📦 Enabling Docker BuildKit..."
export DOCKER_BUILDKIT=1

# Add to shell profile if not already there
SHELL_RC="${HOME}/.bashrc"
if [ -n "$ZSH_VERSION" ]; then
    SHELL_RC="${HOME}/.zshrc"
fi

if ! grep -q "DOCKER_BUILDKIT" "$SHELL_RC" 2>/dev/null; then
    echo "" >> "$SHELL_RC"
    echo "# Enable Docker BuildKit" >> "$SHELL_RC"
    echo "export DOCKER_BUILDKIT=1" >> "$SHELL_RC"
    echo "✅ Added DOCKER_BUILDKIT to $SHELL_RC"
else
    echo "✅ DOCKER_BUILDKIT already configured in $SHELL_RC"
fi

# Test BuildKit
echo "🧪 Testing BuildKit..."
if DOCKER_BUILDKIT=1 docker build --help 2>&1 | grep -q "cache-from"; then
    echo "✅ BuildKit features are available"
else
    echo "⚠️  BuildKit features might not be fully available"
fi

echo ""
echo "🎉 BuildKit setup complete!"
echo ""
echo "📖 Usage examples:"
echo "  # Build with BuildKit (automatic with updated Makefile)"
echo "  make docker"
echo ""
echo "  # Build with explicit BuildKit"
echo "  DOCKER_BUILDKIT=1 docker build -t tas-mcp:dev ."
echo ""
echo "  # Use Docker Bake for advanced builds"
echo "  docker buildx bake -f docker-bake.hcl"
echo ""
echo "  # Build specific target"
echo "  DOCKER_BUILDKIT=1 docker build --target=builder -t tas-mcp:builder ."
echo ""
echo "🔄 Please reload your shell or run: source $SHELL_RC"