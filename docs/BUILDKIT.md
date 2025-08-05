# Docker BuildKit Support

This document describes the Docker BuildKit optimizations available for TAS MCP Server.

## Overview

Docker BuildKit is a new build subsystem in Docker that provides:
- Improved build performance
- Better caching mechanisms
- Parallel build execution
- Advanced Dockerfile features

## Current Status

The TAS MCP Server Dockerfile has been optimized for BuildKit compatibility with:
- Multi-stage builds for smaller images
- Optimized layer caching
- `.dockerignore` for reduced build context
- Clear separation of build and runtime stages

## Building with Standard Docker

The project works perfectly with the standard Docker builder:

```bash
# Using make
make docker

# Direct docker command
docker build -t tas-mcp:dev .
```

## Optional: Installing Docker Buildx

For advanced BuildKit features, you can install docker-buildx:

### Ubuntu/Debian
```bash
# Install from Docker's official repository
sudo apt-get update
sudo apt-get install docker-buildx-plugin
```

### macOS
```bash
# If using Docker Desktop, buildx is included
# Otherwise, install via Homebrew:
brew install docker-buildx
```

### Manual Installation
```bash
# Download the latest release
BUILDX_VERSION="v0.11.2"
curl -LO "https://github.com/docker/buildx/releases/download/${BUILDX_VERSION}/buildx-${BUILDX_VERSION}.linux-amd64"

# Install as Docker CLI plugin
mkdir -p ~/.docker/cli-plugins
mv buildx-${BUILDX_VERSION}.linux-amd64 ~/.docker/cli-plugins/docker-buildx
chmod +x ~/.docker/cli-plugins/docker-buildx

# Verify installation
docker buildx version
```

## Using Docker Bake

The project includes a `docker-bake.hcl` file for advanced build configurations:

```bash
# Build default target
docker buildx bake

# Build specific target
docker buildx bake tas-mcp-dev

# Build all federation servers
docker buildx bake federation

# Build for multiple platforms
docker buildx bake tas-mcp-multiplatform
```

## BuildKit Environment Variables

You can enable BuildKit features with environment variables:

```bash
# Enable BuildKit
export DOCKER_BUILDKIT=1

# Then build normally
docker build -t tas-mcp:dev .
```

## Optimizations Applied

1. **Layer Caching**: Dependencies are downloaded in a separate layer
2. **Build Context**: Optimized `.dockerignore` reduces context size
3. **Multi-stage Build**: Smaller final images with only runtime dependencies
4. **Security**: Non-root user and minimal attack surface

## Performance Comparison

Typical build time improvements with BuildKit:
- First build: Similar to standard builder
- Subsequent builds: 30-50% faster due to better caching
- Parallel builds: Up to 70% faster for complex builds

## Troubleshooting

### BuildKit Not Available
If you see warnings about BuildKit, the standard builder will be used automatically. This is perfectly fine and the build will complete successfully.

### Cache Issues
Clear Docker build cache if needed:
```bash
docker builder prune
```

### Build Context Too Large
Check `.dockerignore` is working:
```bash
# Show build context size
du -sh .
```

## Future Enhancements

When BuildKit/buildx is widely available, we can enable:
- `--mount=type=cache` for Go module and build caches
- `--mount=type=secret` for secure secret handling
- Cross-platform builds with `--platform`
- Remote caching for CI/CD pipelines

## References

- [Docker BuildKit Documentation](https://docs.docker.com/build/buildkit/)
- [Dockerfile Best Practices](https://docs.docker.com/develop/develop-images/dockerfile_best-practices/)
- [Docker Buildx](https://docs.docker.com/buildx/working-with-buildx/)