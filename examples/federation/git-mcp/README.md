# Git MCP Integration Example

This example demonstrates how to integrate with the Git MCP Server for repository automation and management.

## Features Demonstrated

- Repository status and diff operations
- Branch creation and management
- Commit operations and staging
- Repository history access
- Working tree management

## Running the Example

```bash
cd examples/federation/git-mcp
go mod tidy
go run main.go
```

## Prerequisites

- Git MCP Server running on localhost:3000
- TAS MCP Federation server running on localhost:8080
- Git repository for testing operations

## Git Capabilities

- **git_status** - Working tree status and changes
- **git_diff_unstaged** / **git_diff_staged** - File difference analysis
- **git_commit** - Commit creation and management
- **git_add** / **git_reset** - Staging area operations
- **git_log** - Repository history access
- **git_create_branch** / **git_checkout** - Branch management

## Use Cases

- Automated repository management
- CI/CD integration
- Code analysis workflows
- Development automation
- Repository monitoring