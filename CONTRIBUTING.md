# 🤝 Contributing to TAS MCP Server

Thank you for your interest in contributing to the TAS MCP Server! This document provides guidelines and instructions for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [How to Contribute](#how-to-contribute)
- [Development Process](#development-process)
- [Style Guidelines](#style-guidelines)
- [Commit Guidelines](#commit-guidelines)
- [Pull Request Process](#pull-request-process)
- [Community](#community)

## Code of Conduct

By participating in this project, you agree to abide by our Code of Conduct:

- **Be respectful**: Treat everyone with respect. No harassment, discrimination, or offensive behavior.
- **Be collaborative**: Work together towards common goals.
- **Be constructive**: Provide helpful feedback and accept criticism gracefully.
- **Be inclusive**: Welcome newcomers and help them get started.

## Getting Started

1. **Fork the repository** on GitHub
2. **Clone your fork**:
   ```bash
   git clone https://github.com/YOUR_USERNAME/tas-mcp.git
   cd tas-mcp
   ```
3. **Add upstream remote**:
   ```bash
   git remote add upstream https://github.com/tributary-ai-services/tas-mcp.git
   ```
4. **Set up development environment**:
   ```bash
   make init
   ```

## How to Contribute

### Reporting Bugs

1. **Check existing issues** to avoid duplicates
2. **Create a new issue** with:
   - Clear, descriptive title
   - Steps to reproduce
   - Expected vs actual behavior
   - System information (OS, Go version, etc.)
   - Relevant logs or error messages

**Bug Report Template:**
```markdown
## Description
Brief description of the bug

## Steps to Reproduce
1. Step one
2. Step two
3. ...

## Expected Behavior
What should happen

## Actual Behavior
What actually happens

## Environment
- OS: [e.g., Ubuntu 22.04]
- Go version: [e.g., 1.22.5]
- TAS MCP version: [e.g., 1.0.0]

## Additional Context
Any other relevant information
```

### Suggesting Features

1. **Check the roadmap** in README.md
2. **Search existing issues** for similar requests
3. **Create a feature request** with:
   - Use case and motivation
   - Proposed solution
   - Alternative solutions considered
   - Additional context

**Feature Request Template:**
```markdown
## Feature Description
Brief description of the feature

## Use Case
Why is this feature needed? What problem does it solve?

## Proposed Solution
How should this feature work?

## Alternatives Considered
What other solutions have you considered?

## Additional Context
Any mockups, diagrams, or examples
```

### Contributing Code

1. **Find an issue** to work on:
   - Look for issues labeled `good first issue` or `help wanted`
   - Comment on the issue to claim it
   - Wait for maintainer approval

2. **Create a feature branch**:
   ```bash
   git checkout -b feature/issue-123-description
   ```

3. **Make your changes**:
   - Write clean, readable code
   - Add tests for new functionality
   - Update documentation as needed

4. **Test thoroughly**:
   ```bash
   make test
   make lint
   ```

5. **Submit a pull request**

## Development Process

### Setting Up Your Environment

See [DEVELOPER.md](DEVELOPER.md) for detailed setup instructions.

### Workflow

1. **Sync with upstream**:
   ```bash
   git fetch upstream
   git checkout main
   git merge upstream/main
   ```

2. **Create feature branch**:
   ```bash
   git checkout -b feature/your-feature-name
   ```

3. **Make changes**:
   - Write code
   - Add/update tests
   - Update documentation

4. **Run tests**:
   ```bash
   make test
   make lint
   ```

5. **Commit changes**:
   ```bash
   git add .
   git commit -m "feat: add amazing feature"
   ```

6. **Push to your fork**:
   ```bash
   git push origin feature/your-feature-name
   ```

7. **Create pull request** on GitHub

## Style Guidelines

### Go Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting
- Use meaningful variable and function names
- Keep functions small and focused
- Document exported types and functions

### Example:
```go
// EventProcessor processes incoming MCP events according to configured rules.
// It implements retry logic and metric collection.
type EventProcessor struct {
    logger    *zap.Logger
    forwarder EventForwarder
    metrics   *ProcessorMetrics
}

// ProcessEvent validates and processes a single event.
// It returns an error if the event cannot be processed.
func (p *EventProcessor) ProcessEvent(ctx context.Context, event *Event) error {
    // Validate event
    if err := p.validateEvent(event); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    
    // Process event
    start := time.Now()
    if err := p.forwarder.Forward(ctx, event); err != nil {
        p.metrics.IncrementErrors()
        return fmt.Errorf("forwarding failed: %w", err)
    }
    
    p.metrics.RecordLatency(time.Since(start))
    return nil
}
```

### Documentation Style

- Use clear, concise language
- Include code examples
- Keep documentation up-to-date
- Use proper markdown formatting

## Commit Guidelines

We follow [Conventional Commits](https://www.conventionalcommits.org/):

### Format
```
<type>(<scope>): <subject>

<body>

<footer>
```

### Types
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `perf`: Performance improvements
- `test`: Adding or updating tests
- `build`: Build system changes
- `ci`: CI/CD changes
- `chore`: Other changes (updating dependencies, etc.)

### Examples
```bash
# Feature
git commit -m "feat(forwarding): add support for Kafka targets"

# Bug fix
git commit -m "fix(grpc): handle connection timeout correctly"

# Documentation
git commit -m "docs: update API examples in README"

# With body
git commit -m "feat(auth): implement JWT authentication

- Add JWT validation middleware
- Support multiple signing algorithms
- Include refresh token logic

Closes #123"
```

## Pull Request Process

### Before Submitting

1. **Update your branch**:
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

2. **Run all checks**:
   ```bash
   make test
   make lint
   make fmt
   ```

3. **Update documentation** if needed

### PR Requirements

- **Clear title** following commit conventions
- **Description** explaining:
  - What changes were made
  - Why they were made
  - How they were tested
- **Link to related issue** (if applicable)
- **Tests** for new functionality
- **Documentation** updates
- **No merge conflicts**

### PR Template
```markdown
## Description
Brief description of changes

## Related Issue
Fixes #123

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed

## Checklist
- [ ] Code follows style guidelines
- [ ] Self-review completed
- [ ] Comments added for complex code
- [ ] Documentation updated
- [ ] No new warnings generated
```

### Review Process

1. **Automated checks** must pass:
   - CI/CD pipeline
   - Code coverage
   - Linting

2. **Code review** by maintainers:
   - Code quality
   - Test coverage
   - Documentation
   - Performance impact

3. **Approval and merge**:
   - At least one maintainer approval
   - All conversations resolved
   - Branch up-to-date with main

## Community

### Communication Channels

- **GitHub Issues**: Bug reports and feature requests
- **GitHub Discussions**: General discussions and questions
- **Discord**: Real-time chat and support
- **Email**: dev@tributary-ai-services.com

### Getting Help

- Check the [documentation](docs/)
- Search [existing issues](https://github.com/tributary-ai-services/tas-mcp/issues)
- Ask in [GitHub Discussions](https://github.com/tributary-ai-services/tas-mcp/discussions)
- Join our [Discord server](https://discord.gg/tas-mcp)

### Recognition

We value all contributions! Contributors will be:
- Listed in [CONTRIBUTORS.md](CONTRIBUTORS.md)
- Mentioned in release notes
- Given credit in relevant documentation

## License

By contributing to TAS MCP Server, you agree that your contributions will be licensed under the Apache License 2.0.

---

Thank you for contributing to TAS MCP Server! 🚀