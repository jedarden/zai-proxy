# ZAI Proxy Prometheus Metrics Documentation

## Overview

The zai-proxy exports comprehensive metrics for monitoring token consumption, request performance, rate limiting, and system health. All metrics are exposed on the `/metrics` endpoint in Prometheus text format.

## Metrics Endpoint

```bash
# Access metrics
curl http://zai-proxy:8080/metrics

# Query from within Kubernetes cluster
curl http://zai-proxy.mcp.svc.cluster.local:8080/metrics
```

## Token Consumption Metrics

### `zai_proxy_tokens_total`

**Type:** Counter

**Description:** Total number of tokens processed, tracking both input (prompt) and output (completion) tokens separately.

**Labels:**
- `direction` - Token direction: `input` (prompt tokens) or `output` (completion tokens)
- `model` - Tokenizer model name (e.g., `glm-4`, `claude-3`)
- `variant` - Deployment variant: `stable` (production) or `canary` (testing)

**Example values:**
```prometheus
zai_proxy_tokens_total{direction="input",model="glm-4",variant="stable"} 1250000
zai_proxy_tokens_total{direction="output",model="glm-4",variant="stable"} 3500000
zai_proxy_tokens_total{direction="input",model="glm-4",variant="canary"} 15000
zai_proxy_tokens_total{direction="output",model="glm-4",variant="canary"} 42000
```

**Use cases:**
- Track total token consumption over time
- Calculate cost based on token usage
- Compare input vs output token ratios
- Monitor canary deployment token usage separately from production

### `zai_proxy_token_rate_seconds`

**Type:** Histogram

**Description:** Time taken to process tokens (tokenization speed). Lower values indicate faster tokenization. Measures the duration of the tokenization operation itself.

**Labels:**
- `direction` - Token direction: `input` or `output`
- `model` - Tokenizer model name
- `variant` - Deployment variant: `stable` or `canary`

**Buckets:** `[.00001, .00005, .0001, .0005, .001, .005, .01, .05, .1]` (seconds)

**Example values:**
```prometheus
zai_proxy_token_rate_seconds_bucket{direction="input",model="glm-4",variant="stable",le="0.001"} 9500
zai_proxy_token_rate_seconds_bucket{direction="input",model="glm-4",variant="stable",le="0.005"} 9980
zai_proxy_token_rate_seconds_bucket{direction="input",model="glm-4",variant="stable",le="+Inf"} 10000
zai_proxy_token_rate_seconds_sum{direction="input",model="glm-4",variant="stable"} 8.234
zai_proxy_token_rate_seconds_count{direction="input",model="glm-4",variant="stable"} 10000
```

**Use cases:**
- Monitor tokenization performance
- Detect tokenizer slowdowns
- Compare performance between models
- Alert on slow tokenization (>10ms P95)

### `zai_proxy_token_rate`

**Type:** Histogram

**Description:** Token processing throughput in tokens per second. Higher values indicate faster processing. Measures how many tokens are processed per unit time.

**Labels:**
- `direction` - Token direction: `input` or `output`
- `model` - Tokenizer model name
- `variant` - Deployment variant: `stable` or `canary`

**Buckets:** `[10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000]` (tokens/second)

**Example values:**
```prometheus
zai_proxy_token_rate_bucket{direction="input",model="glm-4",variant="stable",le="1000"} 120
zai_proxy_token_rate_bucket{direction="input",model="glm-4",variant="stable",le="5000"} 850
zai_proxy_token_rate_bucket{direction="input",model="glm-4",variant="stable",le="+Inf"} 1000
zai_proxy_token_rate_sum{direction="input",model="glm-4",variant="stable"} 2500000
zai_proxy_token_rate_count{direction="input",model="glm-4",variant="stable"} 1000
```

**Use cases:**
- Monitor tokenization throughput
- Compare throughput between input and output tokenization
- Identify performance bottlenecks
- Capacity planning based on tokens/second

### `zai_proxy_token_count_duration_seconds`

**Type:** Histogram

**Description:** Overall duration of token counting operations, including both tokenization and any overhead.

**Labels:**
- `variant` - Deployment variant: `stable` or `canary`

**Buckets:** `[.0001, .0005, .001, .005, .01, .025, .05, .1]` (seconds)

**Use cases:**
- Monitor total token counting overhead
- Ensure token counting latency stays below target (<5ms P95)
- Compare performance between stable and canary deployments

## Request Performance Metrics

### `zai_proxy_requests_total`

**Type:** Counter

**Description:** Total number of requests processed.

**Labels:**
- `method` - HTTP method (GET, POST, etc.)
- `path` - Request path
- `status_code` - HTTP status code
- `variant` - Deployment variant

**Example query:**
```promql
# Request rate by status code
rate(zai_proxy_requests_total{variant="stable"}[5m])

# Error rate
rate(zai_proxy_requests_total{variant="stable",status_code=~"5.."}[5m])
```

### `zai_proxy_request_duration_seconds`

**Type:** Histogram

**Description:** Request duration from start to completion.

**Labels:**
- `method`, `path`, `status_code`, `variant`

**Buckets:** `[.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300]` (seconds)

**Example query:**
```promql
# P95 latency
histogram_quantile(0.95, rate(zai_proxy_request_duration_seconds_bucket{variant="stable"}[5m]))

# Average latency
rate(zai_proxy_request_duration_seconds_sum{variant="stable"}[5m]) /
rate(zai_proxy_request_duration_seconds_count{variant="stable"}[5m])
```

### `zai_proxy_request_size_bytes` / `zai_proxy_response_size_bytes`

**Type:** Histogram

**Description:** Request and response body sizes in bytes.

**Labels:**
- Request: `method`, `path`, `variant`
- Response: `method`, `path`, `status_code`, `variant`

**Buckets:** Exponential (100, 1000, 10000, ...)

**Example query:**
```promql
# Average response size
rate(zai_proxy_response_size_bytes_sum{variant="stable"}[5m]) /
rate(zai_proxy_response_size_bytes_count{variant="stable"}[5m])
```

## Concurrency & Worker Metrics

### `zai_proxy_concurrent_requests`

**Type:** Gauge

**Description:** Number of requests currently being processed.

**Labels:**
- `variant` - Deployment variant

**Example query:**
```promql
# Current load
zai_proxy_concurrent_requests{variant="stable"}
```

### `zai_proxy_max_workers`

**Type:** Gauge

**Description:** Maximum number of concurrent workers allowed (configured limit).

**Labels:**
- `variant` - Deployment variant

### `zai_proxy_worker_utilization_ratio`

**Type:** Gauge

**Description:** Worker utilization ratio (concurrent_requests / max_workers). Value ranges from 0.0 to 1.0 (or higher if overloaded).

**Labels:**
- `variant` - Deployment variant

**Example query:**
```promql
# Worker utilization percentage
zai_proxy_worker_utilization_ratio{variant="stable"} * 100

# Alert when utilization exceeds 80%
zai_proxy_worker_utilization_ratio{variant="stable"} > 0.8
```

## Rate Limiting Metrics

### `zai_proxy_rate_limit_requests_per_second`

**Type:** Gauge

**Description:** Current rate limit in requests per second. This value adjusts automatically based on upstream 429 responses.

**Labels:**
- `variant` - Deployment variant

### `zai_proxy_rate_limit_wait_seconds`

**Type:** Histogram

**Description:** Time spent waiting for rate limiter before processing request.

**Labels:**
- `variant` - Deployment variant

**Buckets:** `[.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10]` (seconds)

### `zai_proxy_rate_limit_adjustments_total`

**Type:** Counter

**Description:** Number of times the rate limit was adjusted (increased or decreased).

**Labels:**
- `direction` - Adjustment direction: `increase` or `decrease`
- `variant` - Deployment variant

**Example query:**
```promql
# Rate limit adjustments over time
rate(zai_proxy_rate_limit_adjustments_total{variant="stable"}[10m])
```

### `zai_proxy_rate_limit_rejections_total`

**Type:** Counter

**Description:** Number of requests rejected due to rate limiting.

**Labels:**
- `variant` - Deployment variant

## Error Metrics

### `zai_proxy_upstream_errors_total`

**Type:** Counter

**Description:** Total number of upstream errors by error type.

**Labels:**
- `error_type` - Error type: `request_creation`, `upstream_connection`, `read_error`, `write_error`
- `variant` - Deployment variant

**Example query:**
```promql
# Error rate by type
rate(zai_proxy_upstream_errors_total{variant="stable"}[5m])
```

### `zai_proxy_retry_attempts_total`

**Type:** Counter

**Description:** Total number of retry attempts.

**Labels:**
- `reason` - Retry reason: `429` (rate limited), `network_error`, or `retry` (general)
- `variant` - Deployment variant

## Build Info Metric

### `zai_proxy_build_info`

**Type:** Gauge (always 1)

**Description:** Build information including version, variant, commit hash, and build time. This metric always has value 1 and exists solely to export build metadata as labels.

**Labels:**
- `version` - Version number (e.g., `v1.3.0`)
- `variant` - Deployment variant: `stable` or `canary`
- `commit` - Git commit hash
- `build_time` - Build timestamp

**Example query:**
```promql
# View current deployed version
zai_proxy_build_info{variant="stable"}
```

## Example Prometheus Queries

### Token Consumption Analysis

```promql
# Total tokens processed per hour (input + output)
sum(increase(zai_proxy_tokens_total{variant="stable"}[1h]))

# Input vs output token ratio
sum(rate(zai_proxy_tokens_total{direction="input",variant="stable"}[5m])) /
sum(rate(zai_proxy_tokens_total{direction="output",variant="stable"}[5m]))

# Token usage by model
sum by (model) (rate(zai_proxy_tokens_total{variant="stable"}[5m]))

# Compare stable vs canary token usage
sum(rate(zai_proxy_tokens_total{variant="stable"}[5m])) by (direction)
sum(rate(zai_proxy_tokens_total{variant="canary"}[5m])) by (direction)
```

### Tokenization Performance

```promql
# P95 tokenization latency (input)
histogram_quantile(0.95,
  rate(zai_proxy_token_rate_seconds_bucket{direction="input",variant="stable"}[5m]))

# Average tokenization throughput (tokens/second)
rate(zai_proxy_token_rate_sum{direction="input",variant="stable"}[5m]) /
rate(zai_proxy_token_rate_count{direction="input",variant="stable"}[5m])

# Slow tokenization alert (P95 > 10ms)
histogram_quantile(0.95,
  rate(zai_proxy_token_rate_seconds_bucket{variant="stable"}[5m])) > 0.01
```

### Request Performance

```promql
# Requests per second
sum(rate(zai_proxy_requests_total{variant="stable"}[5m]))

# P95 request latency
histogram_quantile(0.95,
  rate(zai_proxy_request_duration_seconds_bucket{variant="stable"}[5m]))

# Error rate (5xx responses)
sum(rate(zai_proxy_requests_total{variant="stable",status_code=~"5.."}[5m])) /
sum(rate(zai_proxy_requests_total{variant="stable"}[5m]))
```

### Canary vs Production Comparison

```promql
# Token processing rate comparison
sum(rate(zai_proxy_tokens_total{variant="stable"}[5m])) by (direction)
sum(rate(zai_proxy_tokens_total{variant="canary"}[5m])) by (direction)

# Latency comparison (P95)
histogram_quantile(0.95,
  rate(zai_proxy_request_duration_seconds_bucket{variant="stable"}[5m]))
histogram_quantile(0.95,
  rate(zai_proxy_request_duration_seconds_bucket{variant="canary"}[5m]))

# Error rate comparison
sum(rate(zai_proxy_requests_total{variant="stable",status_code=~"5.."}[5m])) /
sum(rate(zai_proxy_requests_total{variant="stable"}[5m]))
sum(rate(zai_proxy_requests_total{variant="canary",status_code=~"5.."}[5m])) /
sum(rate(zai_proxy_requests_total{variant="canary"}[5m]))
```

### Capacity Planning

```promql
# Worker utilization trend
zai_proxy_worker_utilization_ratio{variant="stable"}

# Concurrent requests vs max workers
zai_proxy_concurrent_requests{variant="stable"}
zai_proxy_max_workers{variant="stable"}

# Rate limiting pressure
rate(zai_proxy_rate_limit_adjustments_total{direction="decrease",variant="stable"}[10m])
```

## Grafana Dashboard Suggestions

### Dashboard 1: Token Consumption Overview

**Panels:**

1. **Total Tokens Processed (Time Series)**
   ```promql
   sum(rate(zai_proxy_tokens_total{variant="stable"}[5m])) by (direction)
   ```

2. **Token Rate by Model (Time Series)**
   ```promql
   sum(rate(zai_proxy_tokens_total{variant="stable"}[5m])) by (model, direction)
   ```

3. **Token Cost Estimate (Stat Panel)**
   ```promql
   # Assuming $0.01 per 1000 input tokens, $0.03 per 1000 output tokens
   (sum(increase(zai_proxy_tokens_total{direction="input",variant="stable"}[24h])) * 0.00001) +
   (sum(increase(zai_proxy_tokens_total{direction="output",variant="stable"}[24h])) * 0.00003)
   ```

4. **Tokenization Latency (Heatmap)**
   ```promql
   sum(rate(zai_proxy_token_rate_seconds_bucket{variant="stable"}[5m])) by (le)
   ```

5. **Input vs Output Token Ratio (Gauge)**
   ```promql
   sum(rate(zai_proxy_tokens_total{direction="input",variant="stable"}[5m])) /
   sum(rate(zai_proxy_tokens_total{direction="output",variant="stable"}[5m]))
   ```

### Dashboard 2: Performance & Health

**Panels:**

1. **Request Rate (Time Series)**
   ```promql
   sum(rate(zai_proxy_requests_total{variant="stable"}[5m])) by (status_code)
   ```

2. **Request Latency Percentiles (Time Series)**
   ```promql
   histogram_quantile(0.50, rate(zai_proxy_request_duration_seconds_bucket{variant="stable"}[5m]))
   histogram_quantile(0.95, rate(zai_proxy_request_duration_seconds_bucket{variant="stable"}[5m]))
   histogram_quantile(0.99, rate(zai_proxy_request_duration_seconds_bucket{variant="stable"}[5m]))
   ```

3. **Error Rate (Time Series)**
   ```promql
   sum(rate(zai_proxy_requests_total{variant="stable",status_code=~"5.."}[5m])) /
   sum(rate(zai_proxy_requests_total{variant="stable"}[5m]))
   ```

4. **Worker Utilization (Gauge)**
   ```promql
   zai_proxy_worker_utilization_ratio{variant="stable"} * 100
   ```

5. **Concurrent Requests (Time Series)**
   ```promql
   zai_proxy_concurrent_requests{variant="stable"}
   zai_proxy_max_workers{variant="stable"}
   ```

6. **Upstream Errors (Time Series)**
   ```promql
   sum(rate(zai_proxy_upstream_errors_total{variant="stable"}[5m])) by (error_type)
   ```

### Dashboard 3: Canary Deployment Comparison

**Panels:**

1. **Token Usage: Stable vs Canary (Time Series)**
   ```promql
   sum(rate(zai_proxy_tokens_total[5m])) by (variant, direction)
   ```

2. **Latency Comparison (Time Series)**
   ```promql
   histogram_quantile(0.95, rate(zai_proxy_request_duration_seconds_bucket[5m])) by (variant)
   ```

3. **Error Rate Comparison (Time Series)**
   ```promql
   sum(rate(zai_proxy_requests_total{status_code=~"5.."}[5m])) by (variant) /
   sum(rate(zai_proxy_requests_total[5m])) by (variant)
   ```

4. **Tokenization Performance Comparison (Time Series)**
   ```promql
   histogram_quantile(0.95, rate(zai_proxy_token_rate_seconds_bucket[5m])) by (variant)
   ```

5. **Request Rate Comparison (Time Series)**
   ```promql
   sum(rate(zai_proxy_requests_total[5m])) by (variant)
   ```

### Dashboard 4: Rate Limiting & Capacity

**Panels:**

1. **Current Rate Limit (Gauge)**
   ```promql
   zai_proxy_rate_limit_requests_per_second{variant="stable"}
   ```

2. **Rate Limit Adjustments (Time Series)**
   ```promql
   rate(zai_proxy_rate_limit_adjustments_total{variant="stable"}[5m]) by (direction)
   ```

3. **Rate Limit Wait Time (Heatmap)**
   ```promql
   sum(rate(zai_proxy_rate_limit_wait_seconds_bucket{variant="stable"}[5m])) by (le)
   ```

4. **Retry Attempts (Time Series)**
   ```promql
   rate(zai_proxy_retry_attempts_total{variant="stable"}[5m]) by (reason)
   ```

## Alerting Rules

### Critical Alerts

```yaml
# High error rate
- alert: HighErrorRate
  expr: |
    sum(rate(zai_proxy_requests_total{status_code=~"5.."}[5m])) /
    sum(rate(zai_proxy_requests_total[5m])) > 0.05
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "High error rate detected (>5%)"

# Worker capacity exhausted
- alert: WorkerCapacityExhausted
  expr: zai_proxy_worker_utilization_ratio > 0.9
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Worker utilization above 90%"

# Slow tokenization
- alert: SlowTokenization
  expr: |
    histogram_quantile(0.95,
      rate(zai_proxy_token_rate_seconds_bucket[5m])) > 0.01
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "P95 tokenization latency above 10ms"
```

### Warning Alerts

```yaml
# Frequent rate limit adjustments
- alert: FrequentRateLimitAdjustments
  expr: |
    rate(zai_proxy_rate_limit_adjustments_total{direction="decrease"}[10m]) > 0.1
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "Frequent rate limit decreases detected"

# High retry rate
- alert: HighRetryRate
  expr: rate(zai_proxy_retry_attempts_total[5m]) > 1
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "High retry attempt rate"
```

## Configuration

Token counting metrics can be configured via environment variables:

```bash
# Enable/disable token counting (default: true)
TOKEN_COUNTING_ENABLED=true

# Tokenizer model name for metrics labels (default: glm-4)
TOKENIZER_MODEL=glm-4

# Deployment variant (default: production)
DEPLOYMENT_VARIANT=stable  # or "canary"
```

## Notes

- All histograms use carefully tuned bucket ranges for optimal query performance
- Metrics are designed to support dual-deployment monitoring (stable + canary)
- Token metrics track both count and processing rate for comprehensive analysis
- Labels allow filtering by deployment variant to isolate canary testing from production
- Build info metric enables version tracking across deployments
