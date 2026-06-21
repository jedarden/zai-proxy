# Contributing to zai-proxy

Thank you for your interest in contributing to zai-proxy! This document provides guidelines and instructions for contributing to the project.

## Table of Contents

- [Development Environment Setup](#development-environment-setup)
- [Code Style and Conventions](#code-style-and-conventions)
- [Testing Requirements](#testing-requirements)
- [Commit Message Conventions](#commit-message-conventions)
- [Pull Request Process](#pull-request-process)
- [Adding New Features or Fixing Bugs](#adding-new-features-or-fixing-bugs)
- [Documentation](#documentation)
- [Getting Help](#getting-help)

## Development Environment Setup

### Prerequisites

- **Go 1.21+** - For the proxy backend
- **Node.js 18+** and **npm** - For the dashboard frontend
- **Docker** - For container builds and testing
- **Make** (optional) - For convenience targets

### Clone and Setup

```bash
# Clone the repository
git clone https://git.ardenone.com/jedarden/zai-proxy.git
cd zai-proxy

# Setup Go environment (proxy)
cd proxy
go mod download

# Setup Node environment (dashboard)
cd ../dashboard/frontend
npm install
```

### Running Locally

**Proxy:**
```bash
cd proxy
export ZAI_API_KEY="your-api-key-here"
go run .
# Proxy listens on :8080
# Metrics available at :8080/metrics
```

**Dashboard:**
```bash
cd dashboard/frontend
npm run dev
# Frontend dev server on :5173
# Backend API on :8081
```

## Code Style and Conventions

### Go Code (proxy/)

- **Follow standard Go conventions** as described in [Effective Go](https://golang.org/doc/effective_go)
- **Run `gofmt`** before committing: `gofmt -w .`
- **Use meaningful names** - err, ctx, req, resp are acceptable; i, j, x should be avoided unless trivial
- **Exported functions** must have doc comments
- **Error handling** - Always check and handle errors, use `fmt.Errorf` with `%w` for wrapping
- **Package organization** - Keep related functions together, separate concerns into files (e.g., `tokenizer.go`, `translator.go`, `metrics.go`)

### TypeScript/React Code (dashboard/frontend/)

- **Use functional components** with hooks
- **Follow existing patterns** - check similar components before adding new ones
- **Type safety** - Avoid `any`, use proper interfaces
- **File naming** - `PascalCase.tsx` for components, `kebab-case.ts` for utilities

### General

- **Keep functions focused** - Single responsibility principle
- **Add comments** for non-obvious logic
- **Update tests** when modifying behavior
- **Run linters** before committing

## Testing Requirements

### Go Tests

```bash
# Run all tests
go test -v ./...

# Run specific test
go test -v -run TestTokenCountingBasicRequest

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run benchmarks
go test -bench=. -benchmem
```

**Testing Requirements:**
- **Unit tests** for new functions and methods
- **Integration tests** for API endpoints
- **Regression tests** in `tokenizer_regression_test.go` for token counting accuracy
- **Benchmarks** for performance-critical code (target: <5ms per token counting operation)

### Frontend Tests

```bash
cd dashboard/frontend
npm test          # Run Jest tests
npm run lint      # Run ESLint
```

### Manual Testing

- **Test streaming responses** - Ensure SSE streams work correctly
- **Verify metrics** - Check `:8080/metrics` after operations
- **Test edge cases** - Empty inputs, malformed JSON, concurrent requests

## Commit Message Conventions

We use **conventional commits** format:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types:**
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation changes
- `chore` - Maintenance tasks (version bumps, dependencies)
- `refactor` - Code refactoring (no behavior change)
- `test` - Adding or updating tests
- `perf` - Performance improvements

**Scopes:**
- `proxy` - Go proxy backend
- `dashboard` - Dashboard (Go backend + React frontend)
- `frontend` - React frontend specifically
- (none) - General or multiple components

**Examples:**
```bash
feat(proxy): add GLM-4 tokenizer support
fix(dashboard): correct token count display for large numbers
docs(proxy): update environment variables reference
chore: bump VERSION to 1.1.0
```

## Pull Request Process

1. **Branch naming** - Use descriptive names: `feat/add-glm-4-support`, `fix/token-leak`
2. **Update documentation** - Include doc changes in the same PR
3. **Add tests** - Ensure tests pass before submitting
4. **Single purpose** - One PR should do one thing well
5. **Commit squash** - Keep history clean, squash related commits

### PR Description Template

```markdown
## What
Brief description of changes.

## Why
Context and motivation for the change.

## How
Implementation approach and key decisions.

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed

## Checklist
- [ ] Documentation updated
- [ ] No breaking changes (or documented)
- [ ] Tests added/updated
```

## Adding New Features or Fixing Bugs

### Feature Workflow

1. **Discuss first** - Open an issue or discussion for significant features
2. **Design phase** - Consider architecture, metrics, edge cases
3. **Implementation** - Write code following conventions
4. **Testing** - Add comprehensive tests
5. **Documentation** - Update relevant docs
6. **Metrics** - Add Prometheus metrics for observability

### Bug Fix Workflow

1. **Reproduce** - Create a test case that reproduces the bug
2. **Fix** - Implement the minimal fix
3. **Verify** - Ensure the test now passes
4. **Regression check** - Run full test suite
5. **Document** - Add comments if the fix is non-obvious

### Adding Metrics

New metrics should follow the Prometheus naming convention:

```go
var myMetric = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "zai_proxy_<metric_name>",
        Help: "<Description>",
    },
    []string{"label1", "label2"},
)
```

**Register in `metrics.go`:**
- Add to `MetricsRegistry`
- Initialize in `initMetrics()`

## Documentation

### Documentation Structure

```
zai-proxy/
├── README.md                      # Project overview
├── CONTRIBUTING.md                # This file
├── proxy/
│   ├── README.md                  # Proxy-specific docs
│   └── docs/                      # Detailed proxy documentation
└── dashboard/
    └── README.md                  # Dashboard-specific docs
```

### When to Update Docs

- **README.md** - When adding user-facing features
- **docs/** - When adding detailed implementation notes
- **Comments** - When code is non-trivial

### Writing Style

- **Be concise** - Get to the point
- **Include examples** - Show, don't just tell
- **Keep current** - Update docs when code changes
- **Use diagrams** - For complex flows

## Getting Help

### Questions

- **Check documentation** - `docs/` directories have detailed info
- **Search issues** - Your question may have been answered
- **Contact**: `jedarden@jedarden.com`

### Reporting Issues

When reporting bugs, include:

1. **Environment** - Go version, OS, reproduction steps
2. **Expected behavior** - What should happen
3. **Actual behavior** - What actually happens
4. **Logs** - Relevant error messages or stack traces
5. **Minimal reproduction** - Smallest code that reproduces the issue

### Security Issues

For security vulnerabilities, contact directly: `jedarden@jedarden.com`

## Development Workflow Summary

```bash
# 1. Create feature branch
git checkout -b feat/my-feature

# 2. Make changes and test
go test -v ./...
npm test

# 3. Format and lint
gofmt -w .
npm run lint

# 4. Commit with conventional message
git commit -m "feat(proxy): add my feature"

# 5. Push and create PR
git push origin feat/my-feature
```

## License

By contributing to zai-proxy, you agree that your contributions will be licensed under the same license as the project.
