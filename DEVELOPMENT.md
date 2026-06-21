# Developer Guide

Comprehensive onboarding guide for zai-proxy developers.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Project Structure](#project-structure)
- [Local Development Setup](#local-development-setup)
  - [Proxy Development](#proxy-development)
  - [Dashboard Development](#dashboard-development)
- [Running Tests](#running-tests)
- [Debugging](#debugging)
- [Common Development Workflows](#common-development-workflows)
- [CI/CD Process](#cicd-process)
- [Code Style and Conventions](#code-style-and-conventions)
- [Troubleshooting](#troubleshooting)

## Prerequisites

### Required Tools

- **Go 1.23+** - Proxy and dashboard backend
- **Node.js 18+** and **npm** - Dashboard frontend
- **Docker** - Container builds and testing
- **Git** - Version control

### Optional Tools

- **Make** - Convenience targets (if available)
- **kubectl** - Kubernetes access for deployment testing
- **Visual Studio Code** or **GoLand** - Recommended IDEs

### Verify Installation

```bash
# Check Go version
go version
# Expected: go version go1.23.x or later

# Check Node.js version
node --version
# Expected: v18.x.x or later

# Check npm version
npm --version

# Check Docker
docker --version
```

## Project Structure

```
zai-proxy/
├── proxy/                    # Go reverse proxy
│   ├── cmd/                  # Command-line entry points
│   ├── evaluation/           # Token counting evaluation tools
│   ├── scripts/              # Utility scripts
│   ├── tests/                # Integration test fixtures
│   ├── main.go               # Proxy server implementation
│   ├── tokenizer.go          # Token counting logic
│   ├── translator.go         # Provider format translation
│   ├── metrics.go            # Prometheus metrics
│   ├── bodyparser.go         # Request/response parsing
│   ├── *_test.go             # Test files
│   ├── go.mod                # Go dependencies
│   ├── Dockerfile            # Container image
│   └── README.md             # Proxy-specific docs
│
├── dashboard/                # Monitoring dashboard
│   ├── api/                  # HTTP handlers and SSE
│   ├── collector/            # Prometheus scraper
│   ├── storage/              # SQLite database layer
│   ├── logger/               # Structured logging
│   ├── model/                # Data structures
│   ├── frontend/             # React UI
│   │   ├── src/              # TypeScript source
│   │   │   ├── components/   # React components
│   │   │   ├── hooks/        # Custom React hooks
│   │   │   ├── utils/        # Utilities
│   │   │   └── main.tsx      # Entry point
│   │   ├── package.json      # Node dependencies
│   │   └── vite.config.ts    # Vite build config
│   ├── main.go               # Dashboard server
│   ├── go.mod                # Go dependencies
│   ├── Dockerfile            # Multi-stage build
│   └── README.md             # Dashboard-specific docs
│
├── docs/                     # Documentation
│   ├── plan/                 # Architecture and roadmap
│   ├── notes/                # Operational guides
│   └── research/             # External research
│
├── CONTRIBUTING.md           # Contribution guidelines
├── DEVELOPMENT.md            # This file
└── README.md                 # Project overview
```

## Local Development Setup

### Quick Start (Full Stack)

```bash
# Clone repository
git clone https://git.ardenone.com/jedarden/zai-proxy.git
cd zai-proxy

# Setup proxy
cd proxy
go mod download
export ZAI_API_KEY="your-api-key-here"

# Setup dashboard (new terminal)
cd ../dashboard/frontend
npm install

# Run proxy (terminal 1)
cd ../proxy
go run .

# Run dashboard backend (terminal 2)
cd ../dashboard
export SCRAPE_TARGETS="http://localhost:8080/metrics"
go run .

# Run dashboard frontend (terminal 3)
cd frontend
npm run dev
```

### Proxy Development

The proxy is a Go HTTP reverse proxy that fronts the Z.AI API.

#### Setup

```bash
cd /home/coding/zai-proxy/proxy

# Download dependencies
go mod download

# Set required environment variables
export ZAI_API_KEY="your-zai-api-key"

# Optional: Configure tokenizer model
export TOKENIZER_MODEL="glm-4"  # Default

# Optional: Adjust rate limits
export RATE_LIMIT_INITIAL="10.0"
export RATE_LIMIT_MAX="50.0"
export MAX_WORKERS="50"
```

#### Run Locally

```bash
# Simple run
go run .

# Run with specific listen address
export LISTEN_ADDR=":8080"
go run .

# Run with debug logging
export LOG_LEVEL="debug"
go run .
```

The proxy listens on `:8080` by default:
- **Proxy API:** `http://localhost:8080/v1/messages`
- **Metrics:** `http://localhost:8080/metrics`

#### Test Proxy

```bash
# Make a test request
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: $ZAI_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-3-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Check metrics
curl http://localhost:8080/metrics | grep zai_proxy
```

#### Build Binary

```bash
# Build for current platform
go build -o zai-proxy .

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o zai-proxy-linux-amd64 .

# Build with version info
VERSION=$(cat VERSION) go build -ldflags="-X main.Version=$VERSION" -o zai-proxy .
```

### Dashboard Development

The dashboard has a Go backend and React + TypeScript frontend.

#### Backend Setup

```bash
cd /home/coding/zai-proxy/dashboard

# Download dependencies
go mod download

# Set environment variables (optional, defaults shown)
export LISTEN_ADDR=":8081"           # Dashboard API port
export SCRAPE_TARGETS="http://localhost:8080/metrics"
export SCRAPE_INTERVAL="5s"
export DB_PATH="/tmp/dashboard.db"

# Run backend
go run .
```

The dashboard backend listens on `:8081` by default:
- **Frontend:** `http://localhost:8081/`
- **API:** `http://localhost:8081/api/`
- **SSE:** `http://localhost:8081/api/events`
- **Health:** `http://localhost:8081/healthz`

#### Frontend Development

```bash
cd /home/coding/zai-proxy/dashboard/frontend

# Install dependencies
npm install

# Run dev server (with hot reload)
npm run dev

# Build for production
npm run build

# Preview production build
npm run preview

# Run linter
npm run lint

# Run tests
npm run test
npm run test:watch  # Watch mode
```

The frontend dev server runs on `:5173` and proxies API requests to `:8081`.

#### Frontend Project Structure

```
frontend/src/
├── main.tsx              # React entry point
├── App.tsx               # Root component
├── components/           # React components
│   ├── MetricCard.tsx    # Metric display cards
│   ├── StatusHeader.tsx  # Status bar
│   ├── TokenPanel.tsx    # Token usage panel
│   ├── RequestPanel.tsx  # Request metrics
│   ├── RateLimitPanel.tsx # Rate limiting panel
│   └── ...
├── hooks/                # Custom React hooks
│   ├── useMetrics.ts     # Metrics data fetching
│   ├── useSSE.ts         # SSE connection
│   └── ...
└── utils/                # Utilities
    ├── formatters.ts     # Number/time formatting
    └── constants.ts      # Constants
```

## Running Tests

### Proxy Tests

```bash
cd /home/coding/zai-proxy/proxy

# Run all tests
go test -v ./...

# Run specific package tests
go test -v ./tokenizer
go test -v ./translator

# Run specific test
go test -v -run TestTokenCountingBasicRequest

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run benchmarks
go test -bench=. -benchmem

# Run regression tests (token counting accuracy)
go test -v -run Regression
```

### Dashboard Tests

#### Backend Tests

```bash
cd /home/coding/zai-proxy/dashboard

# Run all backend tests
go test -v ./...

# Run specific package tests
go test -v ./collector
go test -v ./storage
go test -v ./api

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

#### Frontend Tests

```bash
cd /home/coding/zai-proxy/dashboard/frontend

# Run tests once
npm run test

# Run in watch mode
npm run test:watch

# Run with coverage
npm run test -- --coverage

# Run specific test file
npm run test -- src/hooks/useMetrics.test.ts
```

### Running All Tests

```bash
# From project root
cd /home/coding/zai-proxy

# Run all Go tests
go test -v ./proxy/... ./dashboard/...

# Run all frontend tests
cd dashboard/frontend && npm run test
```

## Debugging

### Proxy Debugging

#### Enable Debug Logging

```bash
export LOG_LEVEL="debug"
go run .
```

#### Use Delve (Go Debugger)

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug with delve
dlv debug

# Common delve commands:
# (dlv) break main.go:125    # Set breakpoint
# (dlv) continue             # Continue execution
# (dlv) next                 # Next line
# (dlv) print variableName   # Print variable
# (dlv) goroutines           # List goroutines
# (dlv) goroutine 5          # Switch to goroutine 5
```

#### VS Code Debug Configuration

Create `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug Proxy",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/proxy",
      "env": {
        "ZAI_API_KEY": "your-api-key",
        "LOG_LEVEL": "debug"
      }
    },
    {
      "name": "Debug Dashboard Backend",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/dashboard",
      "env": {
        "SCRAPE_TARGETS": "http://localhost:8080/metrics",
        "LOG_LEVEL": "debug"
      }
    }
  ]
}
```

### Dashboard Debugging

#### Backend Debugging

Use the same delve approach as the proxy:

```bash
cd /home/coding/zai-proxy/dashboard
dlv debug
```

#### Frontend Debugging

The Vite dev server includes hot module replacement (HMR). Use browser DevTools:

```bash
cd /home/coding/zai-proxy/dashboard/frontend
npm run dev
```

**Chrome DevTools shortcuts:**
- `F12` or `Ctrl+Shift+I` - Open DevTools
- `Ctrl+Shift+J` - Jump to console
- React DevTools extension recommended

#### SSE Connection Debugging

```bash
# Test SSE endpoint directly
curl -N http://localhost:8081/api/events

# With verbose output
curl -v -N http://localhost:8081/api/events
```

### Common Debugging Scenarios

#### Token Counting Issues

```bash
# Check if tokenizer initialized correctly
kubectl logs deployment/zai-proxy -n mcp | grep -i "token counting"

# Expected: "Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)"

# Check if fallback is active
kubectl logs deployment/zai-proxy -n mcp | grep -i "fallback"
```

#### Dashboard Not Showing Data

```bash
# Check if collector is scraping
kubectl logs deployment/zai-proxy-dashboard -n mcp | grep "scrape"

# Check proxy metrics endpoint
curl http://zai-proxy.mcp.svc.cluster.local:8080/metrics

# Test direct connection from dashboard pod
kubectl exec -n mcp deployment/zai-proxy-dashboard -- \
  wget -O- http://zai-proxy.mcp.svc.cluster.local:8080/metrics
```

#### High Memory/CPU Usage

```bash
# Check container metrics
kubectl top pod -n mcp -l app=zai-proxy

# Check Go runtime metrics
curl http://localhost:8080/metrics | grep go_
```

## Common Development Workflows

### Adding a New Feature

1. **Create feature branch**
   ```bash
   git checkout -b feat/add-new-feature
   ```

2. **Write tests first**
   ```bash
   # Create test file
   touch proxy/myfeature_test.go
   ```

3. **Implement feature**
   ```bash
   # Edit source files
   vim proxy/myfeature.go
   ```

4. **Run tests**
   ```bash
   go test -v ./proxy
   npm run test  # If frontend changes
   ```

5. **Format code**
   ```bash
   gofmt -w ./proxy ./dashboard
   npm run lint  # Frontend
   ```

6. **Commit changes**
   ```bash
   git add .
   git commit -m "feat(proxy): add new feature"
   ```

7. **Push and create PR**
   ```bash
   git push origin feat/add-new-feature
   ```

### Adding a New Metric

1. **Define metric in `metrics.go`**
   ```go
   var myNewMetric = prometheus.NewCounterVec(
       prometheus.CounterOpts{
           Name: "zai_proxy_my_new_metric",
           Help: "Description of what this metric tracks",
       },
       []string{"label1", "label2"},
   )
   ```

2. **Register in `MetricsRegistry`**
   ```go
   var MetricsRegistry = []*prometheus.CounterVec{
       // ... existing metrics
       myNewMetric,
   }
   ```

3. **Use in code**
   ```go
   myNewMetric.WithLabelValues("value1", "value2").Inc()
   ```

4. **Add test**
   ```go
   func TestMyNewMetric(t *testing.T) {
       // Test that metric is registered
       // Test that metric increments correctly
   }
   ```

5. **Test locally**
   ```bash
   curl http://localhost:8080/metrics | grep my_new_metric
   ```

### Adding a New Frontend Component

1. **Create component file**
   ```bash
   touch dashboard/frontend/src/components/MyComponent.tsx
   ```

2. **Implement component**
   ```typescript
   interface MyComponentProps {
     title: string;
     value: number;
   }

   export function MyComponent({ title, value }: MyComponentProps) {
     return (
       <div className="p-4 bg-white rounded shadow">
         <h3 className="text-lg font-semibold">{title}</h3>
         <p className="text-2xl">{value}</p>
       </div>
     );
   }
   ```

3. **Add tests**
   ```typescript
   // MyComponent.test.tsx
   import { describe, it, expect } from 'vitest';
   import { render, screen } from '@testing-library/react';
   import { MyComponent } from './MyComponent';

   describe('MyComponent', () => {
     it('renders title and value', () => {
       render(<MyComponent title="Test" value={42} />);
       expect(screen.getByText('Test')).toBeTruthy();
       expect(screen.getByText('42')).toBeTruthy();
     });
   });
   ```

4. **Use in App**
   ```typescript
   import { MyComponent } from './components/MyComponent';

   // In your component
   <MyComponent title="My Metric" value={123} />
   ```

5. **Test**
   ```bash
   npm run test
   npm run lint
   ```

### Updating Dependencies

#### Go Dependencies

```bash
cd /home/coding/zai-proxy/proxy  # or dashboard/

# List dependencies
go list -m all

# Update all dependencies
go get -u ./...
go mod tidy

# Update specific dependency
go get -u github.com/prometheus/client_golang@latest
go mod tidy

# Verify tests still pass
go test -v ./...
```

#### Node Dependencies

```bash
cd /home/coding/zai-proxy/dashboard/frontend

# Check for updates
npm outdated

# Update all dependencies
npm update

# Update specific dependency
npm install package@latest

# Run tests to verify
npm run test
```

## CI/CD Process

The project uses GitHub Actions for CI/CD. See `.github/workflows/` for workflow definitions.

### Workflow Types

1. **On Push** - Runs tests and builds on every push
2. **On PR** - Runs full test suite including regression tests
3. **Manual** - Docker image builds for specific branches

### Build Process

#### Docker Image Build

The project uses GitHub Actions for Docker builds in devpod environments due to overlayfs limitations.

```yaml
# .github/workflows/build.yml
name: Build Docker Image

on:
  push:
    branches: [main]
  workflow_dispatch:

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build and push
        run: |
          docker build -t ghcr.io/ardenone/zai-proxy:${{ github.sha }} .
          docker push ghcr.io/ardenone/zai-proxy:${{ github.sha }}
```

### Deployment

Deployments are managed via GitOps through ArgoCD:

1. Update manifests in `jedarden/declarative-config` repository
2. Commit and push changes
3. ArgoCD automatically syncs changes to clusters

See `docs/notes/DEPLOYMENT.md` for detailed deployment procedures.

### Running CI Locally

```bash
# Run Go tests with race detector
go test -race -v ./...

# Run with coverage (required for CI)
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Check coverage threshold (typically 80%)
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep total
```

## Code Style and Conventions

### Go Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Run `gofmt -w .` before committing
- Use meaningful variable names
- Add doc comments for exported functions
- Handle errors explicitly
- Keep functions focused and small

```go
// Good
func countTokens(text string) (int, error) {
    if text == "" {
        return 0, fmt.Errorf("empty text")
    }
    // Implementation
    return count, nil
}

// Bad
func cnt(t string) int {
    // Too terse
}
```

### TypeScript/React Style

- Use functional components with hooks
- Prefer `const` over `let`
- Avoid `any` type
- Use TypeScript interfaces for props
- Follow existing component patterns

```typescript
// Good
interface Props {
  title: string;
  value: number;
}

export function MetricCard({ title, value }: Props) {
  return <div>{title}: {value}</div>;
}

// Bad
export function MetricCard(props: any) {
  return <div>{props.title}: {props.value}</div>;
}
```

### Commit Message Format

Follow [conventional commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types:**
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation changes
- `chore` - Maintenance tasks
- `refactor` - Code refactoring
- `test` - Adding or updating tests
- `perf` - Performance improvements

**Scopes:**
- `proxy` - Go proxy backend
- `dashboard` - Dashboard (backend + frontend)
- `frontend` - React frontend specifically

**Examples:**
```bash
feat(proxy): add GLM-4 tokenizer support
fix(dashboard): correct token count display
docs(proxy): update environment variables reference
chore: bump VERSION to 1.1.0
```

## Troubleshooting

### Port Already in Use

```bash
# Find process using port
lsof -i :8080

# Kill process
kill -9 <PID>

# Or use different port
export LISTEN_ADDR=":8081"
```

### Module Download Failed

```bash
# Clear Go module cache
go clean -modcache

# Re-download dependencies
go mod download

# Verify checksums
go mod verify
```

### Frontend Build Errors

```bash
# Clear node_modules and reinstall
rm -rf node_modules package-lock.json
npm install

# Clear Vite cache
rm -rf .vite dist
npm run build
```

### Test Failures

```bash
# Run tests with verbose output
go test -v ./...

# Run specific test with debug output
go test -v -run TestName ./...

# Check for race conditions
go test -race ./...
```

### Docker Build Issues

If Docker builds fail in devpod environments:

```bash
# Use GitHub Actions instead
# See .github/workflows/ for build workflows
# Or use a different environment with full Docker support
```

## Getting Help

- **Documentation:** Check `docs/notes/` for operational guides
- **Issues:** File in repository
- **Questions:** Contact `jedarden@jedarden.com`
- **Logs:** `kubectl logs -f deployment/zai-proxy -n mcp`
- **Metrics:** `http://zai-proxy.mcp.svc.cluster.local:8080/metrics`

## Additional Resources

- [CONTRIBUTING.md](CONTRIBUTING.md) - Contribution guidelines
- [proxy/README.md](proxy/README.md) - Proxy-specific documentation
- [dashboard/README.md](dashboard/README.md) - Dashboard-specific documentation
- [docs/notes/](docs/notes/) - Operational guides and procedures
