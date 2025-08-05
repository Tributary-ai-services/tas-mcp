# Docker Bake configuration for TAS MCP Server
# This file defines build configurations for Docker BuildKit

# Default group
group "default" {
  targets = ["tas-mcp"]
}

# Variables
variable "VERSION" {
  default = "dev"
}

variable "BUILD_DATE" {
  default = ""
}

variable "VCS_REF" {
  default = ""
}

# Main target
target "tas-mcp" {
  dockerfile = "Dockerfile"
  context = "."
  tags = [
    "tas-mcp:${VERSION}",
    "tas-mcp:latest"
  ]
  args = {
    VERSION = VERSION
    BUILD_DATE = BUILD_DATE
    VCS_REF = VCS_REF
  }
  platforms = ["linux/amd64"]
  cache-from = ["type=registry,ref=tas-mcp:buildcache"]
  cache-to = ["type=registry,ref=tas-mcp:buildcache,mode=max"]
}

# Development target with debug symbols
target "tas-mcp-dev" {
  inherits = ["tas-mcp"]
  tags = ["tas-mcp:dev"]
  args = {
    VERSION = "dev"
  }
  output = ["type=docker"]
}

# Multi-platform target
target "tas-mcp-multiplatform" {
  inherits = ["tas-mcp"]
  platforms = [
    "linux/amd64",
    "linux/arm64",
    "linux/arm/v7"
  ]
}

# CI target with inline cache
target "tas-mcp-ci" {
  inherits = ["tas-mcp"]
  cache-from = [
    "type=registry,ref=tas-mcp:buildcache",
    "type=registry,ref=tas-mcp:latest"
  ]
  cache-to = ["type=inline"]
}

# Deployment targets for different environments
target "tas-mcp-staging" {
  inherits = ["tas-mcp"]
  tags = [
    "tas-mcp:${VERSION}-staging",
    "tas-mcp:staging"
  ]
}

target "tas-mcp-production" {
  inherits = ["tas-mcp"]
  tags = [
    "tas-mcp:${VERSION}",
    "tas-mcp:latest",
    "tas-mcp:stable"
  ]
  platforms = ["linux/amd64", "linux/arm64"]
}

# Federation server targets
target "duckduckgo-mcp" {
  dockerfile = "deployments/docker-compose/duckduckgo-mcp/Dockerfile"
  context = "."
  tags = ["tas-mcp/duckduckgo-mcp-server:1.0.0"]
  platforms = ["linux/amd64"]
}

target "apify-mcp" {
  dockerfile = "deployments/docker-compose/apify-mcp/Dockerfile"
  context = "."
  tags = ["tas-mcp/apify-mcp-server:1.0.0"]
  platforms = ["linux/amd64"]
}

# Build all federation servers
group "federation" {
  targets = ["tas-mcp", "duckduckgo-mcp", "apify-mcp"]
}