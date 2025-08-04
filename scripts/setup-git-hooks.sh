#!/bin/bash
#
# Setup Git Hooks for TAS MCP
# This script installs Git hooks for automatic code formatting
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Setting up Git hooks for TAS MCP..."

# Create hooks directory if it doesn't exist
mkdir -p "$REPO_ROOT/.git/hooks"

# Install pre-commit hook
echo "Installing pre-commit hook..."
cat > "$REPO_ROOT/.git/hooks/pre-commit" << 'EOF'
#!/bin/bash
#
# TAS MCP Pre-commit Hook
# Automatically formats Go code with gofmt and goimports before commit
#

set -e

echo "Running pre-commit formatting checks..."

# Check if we have any Go files staged for commit
go_files=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' | grep -v '/gen/' | grep -v '/proto/gen/' || true)

if [ -z "$go_files" ]; then
    echo "No Go files to format"
    exit 0
fi

echo "Formatting Go files..."

# Format with goimports (includes gofmt)
if command -v goimports >/dev/null 2>&1; then
    echo "Running goimports on staged files..."
    echo "$go_files" | xargs goimports -w
else
    echo "goimports not found, using gofmt..."
    echo "$go_files" | xargs gofmt -w
fi

# Check if formatting changed any files
if ! git diff --quiet; then
    echo ""
    echo "✅ Code has been automatically formatted!"
    echo "The following files were modified:"
    git diff --name-only
    echo ""
    echo "Adding formatted files to the commit..."
    
    # Add the formatted files back to staging
    echo "$go_files" | xargs git add
    
    echo "✅ Formatted files added to commit"
else
    echo "✅ All Go files are already properly formatted"
fi

# Run format check to ensure everything is correct
echo "Verifying formatting..."
if make fmt-check >/dev/null 2>&1; then
    echo "✅ All formatting checks passed!"
else
    echo "❌ Formatting verification failed!"
    echo "Please run 'make fmt' and try committing again."
    exit 1
fi

echo "✅ Pre-commit formatting complete!"
EOF

# Make it executable
chmod +x "$REPO_ROOT/.git/hooks/pre-commit"

echo "✅ Pre-commit hook installed successfully!"
echo ""
echo "The pre-commit hook will now:"
echo "  • Automatically format Go code with goimports/gofmt"
echo "  • Add formatted files to your commit"
echo "  • Verify formatting before allowing commit"
echo ""
echo "To bypass the hook (not recommended), use: git commit --no-verify"
echo ""
echo "🎉 Git hooks setup complete!"