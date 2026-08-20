# Z.AI Proxy Dashboard

Real-time web dashboard for monitoring zai-proxy metrics, token usage, and request history.

## Features

✅ **Real-time Metrics** - Live updates via Server-Sent Events (SSE)
✅ **Prometheus Scraping** - Collects metrics from zai-proxy endpoints
✅ **In-Memory Storage** - Bounded, dual-resolution history with automatic downsampling
✅ **Multi-Variant Support** - Monitor production and canary deployments side-by-side
✅ **Token Tracking** - Visualize input/output token rates and totals
✅ **Request Analytics** - Latency percentiles, error rates, throughput
✅ **React Frontend** - Modern UI built with React, Vite, and Tailwind CSS

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Dashboard                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐      ┌─────────────┐      ┌──────────────┐              │
│  │   Collector  │─────▶│   Storage   │─────▶│   SSE Hub    │              │
│  │              │      │             │      │              │              │
│  │ Scrapes      │      │ Ring buffers│      │ Broadcasts   │              │
│  │ Prometheus   │      │ metrics_5s  │      │ live updates  │              │
│  │ endpoints    │      │ metrics_1m  │      │ to clients   │              │
│  └──────────────┘      └─────────────┘      └──────────────┘              │
│         │                                        │                          │
│         │                                        │                          │
│         ▼                                        ▼                          │
│  ┌──────────────┐                         ┌──────────┐                    │
│  │  zai-proxy   │                         │  React   │                    │
│  │  :8080/metrics                        │  Frontend │                    │
│  └──────────────┘                         └──────────┘                    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Components

- **Collector** - Scrapes Prometheus metrics from configured targets every 5 seconds
- **Storage** - Process-local, bounded ring buffers with dual-resolution history:
  - 5-second snapshots (24h retention)
  - 1-minute averages (7d retention)
  - History is intentionally empty after a pod restart; Grafana is the durable source.
- **SSE Hub** - Real-time broadcast of new snapshots to connected web clients
- **API Router** - REST endpoints for historical data queries
- **Frontend** - React SPA with live charts and status displays

## Quick Start

### Run Locally

```bash
# From dashboard directory
cd dashboard/

# Set required environment variables (optional, defaults shown)
export SCRAPE_TARGETS="http://localhost:8080/metrics"
export LISTEN_ADDR=":8080"

# Build and run
go run .

# Dashboard available at http://localhost:8080
# Metrics API at http://localhost:8080/api/metrics
# SSE stream at http://localhost:8080/api/events
```

### Frontend Development

```bash
cd dashboard/frontend/

# Install dependencies
npm install

# Run dev server (proxies API to :8080)
npm run dev

# Build for production
npm run build

# Run tests
npm run test
```

### Docker Deployment

```bash
# Build image
docker build -t zai-proxy-dashboard:latest .

# Run container
docker run -p 8080:8080 \
  -e SCRAPE_TARGETS="http://zai-proxy:8080/metrics" \
  zai-proxy-dashboard:latest
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zai-proxy-dashboard
  namespace: devpod
spec:
  replicas: 1
  selector:
    matchLabels:
      app: zai-proxy-dashboard
  template:
    metadata:
      labels:
        app: zai-proxy-dashboard
    spec:
      containers:
      - name: dashboard
        image: ronaldraygun/zai-proxy-dashboard:1.1.3
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: SCRAPE_TARGETS
          value: "http://zai-proxy.devpod.svc.cluster.local:8080/metrics"
        resources:
          requests:
            cpu: 100m
            memory: 64Mi
          limits:
            cpu: 500m
            memory: 256Mi
---
apiVersion: v1
kind: Service
metadata:
  name: zai-proxy-dashboard
  namespace: devpod
spec:
  selector:
    app: zai-proxy-dashboard
  ports:
  - port: 8080
    targetPort: 8080
```

## Configuration

### Environment Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `LISTEN_ADDR` | String | `:8080` | HTTP listen address |
| `SCRAPE_TARGETS` | String | `http://zai-proxy.devpod.svc.cluster.local:8080/metrics` | Comma-separated Prometheus endpoints to scrape |
| `SCRAPE_INTERVAL` | Duration | `5s` | Scrape interval |
| `SCRAPE_TIMEOUT` | Duration | `3s` | HTTP timeout for each scrape |
| `RETENTION_5S` | Duration | `24h` | Retention for high-resolution data |
| `RETENTION_1M` | Duration | `168h` (7d) | Retention for downsampled data |

### Variant Detection

The dashboard automatically detects deployment variants from scrape target URLs:

- **Production** - Default variant for any target without "test" or "canary" in the URL
- **Canary** - Detected if URL contains "test" or "canary"

```bash
# Single production instance
SCRAPE_TARGETS="http://zai-proxy:8080/metrics"

# Multiple instances (auto-detected variants)
SCRAPE_TARGETS="http://zai-proxy:8080/metrics,http://zai-proxy-canary:8080/metrics"
```

## API Endpoints

### REST API

#### `GET /healthz`

Health check endpoint.

**Response:** `{"status":"ok"}`

#### `GET /api/status`

Returns current health summary for all variants.

**Response:**
```json
{
  "production": {
    "healthy": true,
    "last_scrape": "2026-06-21T10:30:00Z",
    "req_rate": 45.2,
    "error_rate_pct": 0.1,
    "latency_p50_ms": 120,
    "concurrent": 12,
    "worker_utilization": 0.24,
    "rate_limit_rps": 50.0,
    "token_rate_in": 15000,
    "token_rate_out": 45000
  },
  "canary": {
    "healthy": true,
    "last_scrape": "2026-06-21T10:30:00Z",
    "req_rate": 5.1,
    "error_rate_pct": 0.0,
    "latency_p50_ms": 115,
    "concurrent": 2,
    "worker_utilization": 0.10,
    "rate_limit_rps": 10.0,
    "token_rate_in": 1500,
    "token_rate_out": 4800
  }
}
```

#### `GET /api/metrics`

Returns historical metrics for a time range.

**Query Parameters:**
- `range` - Time range: `5m`, `15m`, `1h`, `6h`, `24h`, `7d` (default: `1h`)
- `variant` - Variant filter: `production`, `canary`, `all` (default: `all`)

**Response:** JSON array of `MetricSnapshot` objects

```json
[
  {
    "timestamp": 1708500000000,
    "variant": "production",
    "requests_2xx": 1000,
    "requests_4xx": 10,
    "requests_5xx": 1,
    "tokens_input": 50000,
    "tokens_output": 150000,
    "tokens_cache_read": 10000,
    "tokens_cache_write": 8000,
    "concurrent_requests": 12,
    "max_workers": 50,
    "rate_limit_rps": 50.0,
    "rate_limit_rejections": 0,
    "req_rate": 45.2,
    "token_rate_in": 15000,
    "token_rate_out": 45000,
    "latency_p50": 120,
    "latency_p95": 250,
    "latency_p99": 450,
    "error_rate_pct": 0.1,
    "worker_utilization": 0.24,
    "upstream_errors": 0,
    "retry_attempts": 2
  }
]
```

#### `GET /api/config`

Returns dashboard configuration.

**Response:**
```json
{
  "scrape_interval": 5,
  "targets": ["http://zai-proxy.devpod.svc.cluster.local:8080/metrics"]
}
```

### SSE Endpoint

#### `GET /api/events`

Server-Sent Events stream for real-time metric updates.

**Headers:**
```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

**Message Format:**
```
data: {"type":"connected","scrape_interval":5,"variants":["production","canary"]}

data: {"type":"metrics","data":{"timestamp":1708500000000,"variant":"production",...}}

: heartbeat
```

**Message Types:**
- `connected` - Initial connection confirmation
- `metrics` - New metric snapshot from collector
- `: heartbeat` - Keep-alive (every 30s)

## Data Model

### MetricSnapshot

Represents a single point-in-time collection of metrics from a zai-proxy instance.

```go
type MetricSnapshot struct {
    Timestamp             int64                   // Unix timestamp (ms)
    Variant               string                  // "production" or "canary"
    Requests2xx           float64                 // Total 2xx requests
    Requests4xx           float64                 // Total 4xx requests
    Requests5xx           float64                 // Total 5xx requests
    TokensInput           float64                 // Total input tokens
    TokensOutput          float64                 // Total output tokens
    TokensCacheRead       float64                 // Total cache-read tokens
    TokensCacheWrite      float64                 // Total cache-write tokens
    ConcurrentRequests    float64                 // Current concurrent requests
    MaxWorkers            float64                 // Maximum workers
    RateLimitRps          float64                 // Current rate limit (req/s)
    RateLimitRejections   float64                 // Total rate limit rejections
    RateLimitAdjIncrease  float64                 // Total rate limit increases
    RateLimitAdjDecrease  float64                 // Total rate limit decreases
    UpstreamErrors        float64                 // Total upstream errors
    RetryAttempts         float64                 // Total retry attempts
    LatencyP50            float64                 // Request latency p50 (ms)
    LatencyP95            float64                 // Request latency p95 (ms)
    LatencyP99            float64                 // Request latency p99 (ms)
    RequestSizeAvg        float64                 // Average request size (bytes)
    ResponseSizeAvg       float64                 // Average response size (bytes)
    TokenRateIn           float64                 // Input token rate (tokens/s)
    TokenRateOut          float64                 // Output token rate (tokens/s)
    TokenRateCacheRead    float64                 // Cache-read token rate (tokens/s)
    TokenRateCacheWrite   float64                 // Cache-write token rate (tokens/s)
    ReqRate               float64                 // Request rate (req/s)
    ErrorRatePct          float64                 // Error rate percentage
    WorkerUtilization     float64                 // Worker utilization ratio (0-1)
    StatusCodeRates       map[string]float64      // Per-status-code rates (req/s)
}
```

## Storage

### In-Memory Ring Buffers

The dashboard has no database or persistent volume. It keeps one fixed-size
ring buffer per supported variant (production and canary) at each resolution:

- 5-second raw snapshots for 24 hours
- 1-minute averages for 7 days

The buffers are process-local, so a restart begins with an empty window and
refills as the collector scrapes. This is intentional; use Grafana/Prometheus
for durable history.

### Automatic Downsampling

Every 10 minutes, the dashboard:
1. Reads the retained 5-second snapshots
2. Groups by minute bucket and variant
3. Computes averages for all numeric fields
4. Refreshes the 1-minute ring buffer
5. Cleans up data beyond retention periods

### Query Routing

The API automatically selects the appropriate buffer based on query range:
- ≤ 1 hour → 5-second data for detail
- > 1 hour → 1-minute data for efficiency

## Development

### Backend Tests

```bash
cd dashboard/

# Run all tests
go test -v ./...

# Run specific package tests
go test -v ./collector
go test -v ./storage
go test -v ./api

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Frontend Tests

```bash
cd dashboard/frontend/

# Run tests once
npm run test

# Watch mode
npm run test:watch

# Coverage
npm run test -- --coverage
```

### Building

```bash
# Backend binary
go build -o zai-proxy-dashboard .

# Frontend only
cd frontend/
npm run build

# Full Docker image
docker build -t zai-proxy-dashboard:latest .
```

## Project Structure

```
dashboard/
├── main.go                   # Entry point, server setup
├── go.mod                    # Go dependencies
├── go.sum                    # Go dependency checksums
├── Dockerfile                # Multi-stage container build
├── VERSION                   # Version string
├── api/
│   ├── router.go            # HTTP route handlers
│   ├── middleware.go        # Logging, CORS, recovery
│   └── sse.go               # SSE hub implementation
├── collector/
│   ├── collector.go         # Prometheus scraper
│   └── parser.go            # Prometheus text format parser
├── frontend/
│   ├── package.json         # Node.js dependencies
│   ├── vite.config.ts       # Vite build config
│   ├── tailwind.config.js   # Tailwind CSS config
│   └── src/
│       ├── main.tsx         # React entry point
│       ├── App.tsx          # Main app component
│       └── ...              # Components, hooks, utils
├── logger/
│   └── logger.go            # Structured logging
├── model/
│   └── metrics.go           # Data structures
└── storage/
    ├── config.go            # Retention and fixed-capacity configuration
    └── storage.go           # In-memory ring-buffer storage layer
```

## Troubleshooting

### Dashboard not showing data

**Check collector is scraping:**
```bash
kubectl logs -f deployment/zai-proxy-dashboard -n devpod | grep "scrape"
```

**Expected output:**
```
collector initialized with targets: [http://zai-proxy:8080/metrics]
```

**Check if proxy is reachable:**
```bash
kubectl exec -n devpod deployment/zai-proxy-dashboard -- wget -O- http://zai-proxy.devpod.svc.cluster.local:8080/metrics
```

### SSE connection drops

**Check network connectivity:**
```bash
# Test SSE endpoint
curl -N http://localhost:8080/api/events
```

**Common causes:**
- Proxy timeouts (increase `SCRAPE_TIMEOUT`)
- Network policies blocking connections
- Client not handling keep-alive heartbeats

### High memory usage

**Adjust retention periods:**

Set `RETENTION_5S` and `RETENTION_1M` in the GitOps deployment manifest,
then let ArgoCD reconcile it. The rings are fixed-size at startup, so a pod
restart is required for a changed retention to take effect.

## Performance

| Metric | Target | Typical |
|--------|--------|---------|
| Scrape latency | <100ms | 20-50ms |
| Storage write latency | <10ms | 1-3ms |
| Query latency (1h) | <500ms | 50-200ms |
| Query latency (7d) | <2s | 500ms-1s |
| Memory per variant | <50MB | 20-30MB |
| Storage memory | Well below 256Mi | Bounded by retention and two variants |

**Note:** Metrics depend on scrape interval and request volume.

## Monitoring

### Logs

```bash
# View all logs
kubectl logs -f deployment/zai-proxy-dashboard -n devpod

# Component-specific logs
kubectl logs -f deployment/zai-proxy-dashboard -n devpod | grep "collector"
kubectl logs -f deployment/zai-proxy-dashboard -n devpod | grep "sse"
kubectl logs -f deployment/zai-proxy-dashboard -n devpod | grep "storage"
```

### Health Checks

```bash
# Kubernetes liveness/readiness
kubectl get endpoints zai-proxy-dashboard -n devpod

# Manual health check
curl http://dashboard-url/healthz
```

### Metrics

The dashboard itself does not export Prometheus metrics (it's a consumer, not a producer). Monitor via:

- Container resource usage (CPU, memory)
- Database file size
- SSE client connection count (logs)

## License

See repository license.

## Contributing

Contributions welcome! Please:
1. Write tests for new features
2. Update documentation
3. Follow existing code style
4. Test frontend and backend changes

## Support

- **Documentation:** Check parent `README.md` and `docs/` directory
- **Issues:** File in repository
- **Logs:** `kubectl logs -f deployment/zai-proxy-dashboard -n devpod`
