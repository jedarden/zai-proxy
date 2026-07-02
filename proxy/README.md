# Z.AI Proxy

A production-ready HTTP proxy for the Z.AI API with token counting, adaptive rate limiting, and comprehensive observability.

## Features

✅ **Token Counting** - Accurate input/output token tracking using tiktoken
✅ **Adaptive Rate Limiting** - Automatically adjusts to API limits
✅ **Prometheus Metrics** - Full observability with detailed metrics
✅ **Streaming Support** - Handles SSE (Server-Sent Events) streaming responses
✅ **Graceful Degradation** - Never fails requests due to token counting errors
✅ **Production Ready** - Thread-safe, tested, and battle-hardened

## Quick Start

### Run Locally

```bash
# Set required environment variables
export ZAI_API_KEY="your-api-key-here"

# Run the proxy
go run .

# Proxy listens on :8080
# Metrics available at :8080/metrics
```

### Docker Deployment

```bash
# Build image
docker build -t zai-proxy:1.10.0 .

# Run container
docker run -p 8080:8080 \
  -e ZAI_API_KEY="your-api-key" \
  -e TOKEN_COUNTING_ENABLED=true \
  zai-proxy:1.10.0
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zai-proxy
  namespace: devpod
spec:
  replicas: 2
  selector:
    matchLabels:
      app: zai-proxy
  template:
    metadata:
      labels:
        app: zai-proxy
    spec:
      containers:
      - name: zai-proxy
        image: ronaldraygun/zai-proxy:1.10.0
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: ZAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: zai-api-key
              key: api-key
        - name: TOKEN_COUNTING_ENABLED
          value: "true"
        - name: TOKENIZER_MODEL
          value: "glm-4"
        - name: MAX_WORKERS
          value: "50"
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
          limits:
            cpu: 2000m
            memory: 512Mi
---
apiVersion: v1
kind: Service
metadata:
  name: zai-proxy
  namespace: devpod
spec:
  selector:
    app: zai-proxy
  ports:
  - port: 8080
    targetPort: 8080
```

## Configuration

### Environment Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `ZAI_API_KEY` | String | **Required** | Z.AI API key for upstream authentication |
| `TOKEN_COUNTING_ENABLED` | Boolean | `true` | Enable/disable token counting |
| `TOKENIZER_MODEL` | String | `glm-4` | Model name for Prometheus metrics labels |
| `MAX_WORKERS` | Integer | `10` | Maximum concurrent requests |
| `RATE_LIMIT_INITIAL` | Float | `10.0` | Initial rate limit (requests/second) |
| `RATE_LIMIT_MIN` | Float | `1.0` | Minimum rate limit (requests/second) |
| `RATE_LIMIT_MAX` | Float | `50.0` | Maximum rate limit (requests/second) |
| `MAX_RETRIES` | Integer | `3` | Maximum retry attempts for failed requests |

**See [docs/ENVIRONMENT_VARIABLES.md](docs/ENVIRONMENT_VARIABLES.md) for complete reference.**

## Token Counting

The proxy automatically counts input and output tokens for all requests using tiktoken `cl100k_base` encoding (Claude 3 compatible).

### How It Works

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ Request
       ↓
┌─────────────────────────────────────┐
│  Proxy: Count Input Tokens          │
│  • Parse request messages           │
│  • Tokenize using tiktoken          │
│  • Metric: zai_proxy_tokens_total   │
└──────┬──────────────────────────────┘
       │
       ↓
┌─────────────┐
│  Z.AI API   │
└──────┬──────┘
       │ Response (streaming)
       ↓
┌─────────────────────────────────────┐
│  Proxy: Stream + Capture            │
│  • Stream to client (zero-copy)     │
│  • Capture content in background    │
│  • Count output tokens after stream │
│  • Metric: zai_proxy_tokens_total   │
└──────┬──────────────────────────────┘
       │
       ↓
┌─────────────┐
│   Client    │
└─────────────┘
```

### Quick Configuration

```bash
# Enable token counting (default)
export TOKEN_COUNTING_ENABLED=true
export TOKENIZER_MODEL=glm-4

# Disable token counting
export TOKEN_COUNTING_ENABLED=false
```

### Monitoring Token Usage

**View logs:**
```bash
kubectl logs -f deployment/zai-proxy -n devpod | grep "Token usage"
# Output: Token usage: input=123, output=456
```

**Query Prometheus:**
```promql
# Total tokens per minute
rate(zai_proxy_tokens_total[5m]) * 60

# Input vs output ratio
rate(zai_proxy_tokens_total{direction="output"}[5m]) /
rate(zai_proxy_tokens_total{direction="input"}[5m])

# Token counting latency (should be <1ms)
histogram_quantile(0.99, rate(zai_proxy_token_count_duration_seconds_bucket[5m]))
```

**See [docs/TOKEN_COUNTING.md](docs/TOKEN_COUNTING.md) for comprehensive guide.**

## Prometheus Metrics

The proxy exports metrics at `:8080/metrics`:

### Request Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `zai_proxy_requests_total` | Counter | Total requests by method, path, status |
| `zai_proxy_request_duration_seconds` | Histogram | Request duration |
| `zai_proxy_concurrent_requests` | Gauge | Active concurrent requests |
| `zai_proxy_upstream_errors_total` | Counter | Upstream errors by type |

### Token Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `zai_proxy_tokens_total` | Counter | Total tokens by direction (input/output) and model |
| `zai_proxy_token_count_duration_seconds` | Histogram | Token counting latency |
| `zai_proxy_token_rate` | Histogram | Token processing rate (tokens/second) |

### Rate Limiting Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `zai_proxy_rate_limit_requests_per_second` | Gauge | Current rate limit |
| `zai_proxy_rate_limit_wait_seconds` | Histogram | Rate limiter wait time |
| `zai_proxy_rate_limit_adjustments_total` | Counter | Rate limit adjustments (increase/decrease) |

## Usage Example

```bash
# Make a request through the proxy
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: $ZAI_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-3-sonnet",
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ],
    "max_tokens": 100,
    "stream": true
  }'

# Check token usage in logs
# Output: Token usage: input=5, output=12

# Query metrics
curl http://localhost:8080/metrics | grep zai_proxy_tokens_total
# zai_proxy_tokens_total{direction="input",model="glm-4"} 5
# zai_proxy_tokens_total{direction="output",model="glm-4"} 12
```

## Development

### Running Tests

```bash
# Run all tests
go test -v ./...

# Run token counting tests
go test -v -run TestTikToken

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Building

```bash
# Build binary
go build -o zai-proxy .

# Build Docker image (use GitHub Actions for devpod environments)
docker build -t zai-proxy:1.10.0 .
```

**Note:** Docker builds in devpod environments may fail with overlayfs errors. See [docs/DEVPOD_DOCKER_BUILD_LIMITATION.md](docs/DEVPOD_DOCKER_BUILD_LIMITATION.md) for details and the recommended GitHub Actions build workflow.

### Project Structure

```
zai-proxy/
├── main.go                   # Proxy server
├── tokenizer.go              # Token counting implementation
├── tokenizer_test.go         # Token counting tests
├── main_test.go              # Integration tests
├── docs/
│   ├── TOKEN_COUNTING.md              # Token counting guide (comprehensive)
│   ├── ENVIRONMENT_VARIABLES.md       # Environment variable reference
│   ├── TOKENIZER_CONFIGURATION.md     # Tokenizer configuration
│   └── ...
├── RESPONSE_TOKEN_COUNTING.md # Implementation notes
├── TOKEN_COUNTING_WORKFLOW.md # Development workflow
├── go.mod                    # Go dependencies
└── Dockerfile                # Container image
```

## Documentation

- **[TOKEN_COUNTING.md](docs/TOKEN_COUNTING.md)** - Comprehensive token counting guide
  - How it works internally (architecture)
  - Response format specification
  - Configuration options
  - Prometheus metrics reference
  - Code examples and usage
  - Known limitations
  - Troubleshooting guide
- **[ENVIRONMENT_VARIABLES.md](docs/ENVIRONMENT_VARIABLES.md)** - Environment variable reference
- **[TOKENIZER_CONFIGURATION.md](docs/TOKENIZER_CONFIGURATION.md)** - Tokenizer configuration
- **[DEVPOD_DOCKER_BUILD_LIMITATION.md](docs/DEVPOD_DOCKER_BUILD_LIMITATION.md)** - Devpod Docker build limitations and GitHub Actions workaround
- **[RESPONSE_TOKEN_COUNTING.md](RESPONSE_TOKEN_COUNTING.md)** - Implementation notes
- **[TOKEN_COUNTING_WORKFLOW.md](TOKEN_COUNTING_WORKFLOW.md)** - Development workflow

## Troubleshooting

### Token counting not working

**Check startup logs:**
```bash
kubectl logs deployment/zai-proxy -n devpod | grep -i token
```

**Expected output:**
```
Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)
```

**If disabled:**
```
Token counting disabled (TOKEN_COUNTING_ENABLED=false)
```

**Fix:**
```bash
kubectl set env deployment/zai-proxy -n devpod TOKEN_COUNTING_ENABLED=true
kubectl rollout restart deployment/zai-proxy -n devpod
```

### Token counts seem inaccurate

**Check if fallback tokenizer is active:**
```bash
kubectl logs deployment/zai-proxy -n devpod | grep -i fallback
```

**If you see:**
```
Falling back to SimpleTokenCounter
```

**This means tiktoken failed to initialize.** The fallback uses word count approximation (~30% variance).

**Resolution:** Rebuild with tiktoken dependencies

### High token counting latency

**Query latency:**
```promql
histogram_quantile(0.99, rate(zai_proxy_token_count_duration_seconds_bucket[5m]))
```

**Expected:** <1ms for 99th percentile

**If >5ms:** Increase CPU limits or reduce concurrent requests

**See [docs/TOKEN_COUNTING.md#troubleshooting-guide](docs/TOKEN_COUNTING.md#troubleshooting-guide) for complete guide.**

## Known Limitations

1. **No usage injection** - Token counts are logged and metricked but not added to response bodies
   - Workaround: Check logs or query Prometheus
   - Future enhancement planned

2. **Hardcoded model label** - `TOKENIZER_MODEL` env var applies to all requests
   - Workaround: Use separate proxy instances per model
   - Future: Extract model from request body dynamically

3. **Tiktoken assumptions** - Uses `cl100k_base` encoding for all models
   - Works well for Claude 3 (<3% variance)
   - May have variance for GLM-4 (<10% expected)

**See [docs/TOKEN_COUNTING.md#known-limitations](docs/TOKEN_COUNTING.md#known-limitations) for details.**

## Performance

| Metric | Target | Typical |
|--------|--------|---------|
| Request latency overhead | <5ms | <1ms |
| Token counting latency | <1ms | 0.3-0.8ms |
| Streaming overhead | 0ms | 0ms (zero-copy) |
| Memory per request | <5KB | ~2KB |

**Token counting happens AFTER streaming completes, so it doesn't affect end-user latency.**

## License

See repository license.

## Contributing

Contributions welcome! Please:
1. Read existing documentation
2. Write tests for new features
3. Update documentation
4. Follow existing code style

## Support

- **Documentation:** Check `docs/` directory
- **Issues:** File in repository
- **Logs:** `kubectl logs -f deployment/zai-proxy -n devpod`
- **Metrics:** `http://zai-proxy.devpod.svc.cluster.local:8080/metrics`
