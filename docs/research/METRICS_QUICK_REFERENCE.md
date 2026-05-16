# Token Metrics Quick Reference

## Quick Access

```bash
# View metrics
curl http://zai-proxy:8080/metrics | grep zai_proxy_tokens

# Kubernetes cluster
kubectl port-forward -n mcp svc/zai-proxy 8080:8080
curl http://localhost:8080/metrics | grep zai_proxy_tokens
```

## Helper Functions Reference

### Recording Token Counts

```go
// Record input tokens
RecordInputTokens(model, version, count)
// Example: RecordInputTokens("glm-4", "stable", 150)

// Record output tokens
RecordOutputTokens(model, version, count)
// Example: RecordOutputTokens("glm-4", "stable", 420)
```

### Recording Token Rates

```go
// Record token processing rate (both time and throughput)
RecordTokenRate(direction, model, version, duration, tokenCount)
// Example: RecordTokenRate("input", "glm-4", "stable", 5*time.Millisecond, 100)

// Convenience wrappers
RecordInputTokenRate(model, version, duration, tokenCount)
RecordOutputTokenRate(model, version, duration, tokenCount)
```

## Essential Prometheus Queries

### Token Consumption

```promql
# Total tokens per second (all types)
sum(rate(zai_proxy_tokens_total{variant="stable"}[5m]))

# Input vs output
sum(rate(zai_proxy_tokens_total{variant="stable"}[5m])) by (direction)

# By model
sum(rate(zai_proxy_tokens_total{variant="stable"}[5m])) by (model)

# Total in last hour
sum(increase(zai_proxy_tokens_total{variant="stable"}[1h]))
```

### Performance

```promql
# P95 tokenization latency
histogram_quantile(0.95,
  rate(zai_proxy_token_rate_seconds_bucket{variant="stable"}[5m]))

# Average tokens per second
rate(zai_proxy_token_rate_sum{variant="stable"}[5m]) /
rate(zai_proxy_token_rate_count{variant="stable"}[5m])
```

### Canary Comparison

```promql
# Compare token rates
sum(rate(zai_proxy_tokens_total[5m])) by (variant, direction)

# Compare performance
histogram_quantile(0.95,
  rate(zai_proxy_token_rate_seconds_bucket[5m])) by (variant)
```

## Metrics Summary

| Metric | Type | Labels | Purpose |
|--------|------|--------|---------|
| `zai_proxy_tokens_total` | Counter | direction, model, variant | Total tokens processed |
| `zai_proxy_token_rate_seconds` | Histogram | direction, model, variant | Tokenization latency |
| `zai_proxy_token_rate` | Histogram | direction, model, variant | Tokens per second |

## Labels

- **direction**: `input` (prompts) or `output` (completions)
- **model**: `glm-4`, `claude-3`, etc. (set via `TOKENIZER_MODEL` env var)
- **variant**: `stable` (production) or `canary` (testing) (set via `DEPLOYMENT_VARIANT` env var)

## Common Alerts

```yaml
# Slow tokenization
- alert: SlowTokenization
  expr: |
    histogram_quantile(0.95,
      rate(zai_proxy_token_rate_seconds_bucket[5m])) > 0.01
  for: 10m
  annotations:
    summary: "P95 tokenization latency above 10ms"

# High token rate anomaly
- alert: TokenRateAnomaly
  expr: |
    abs(rate(zai_proxy_tokens_total[5m]) -
        rate(zai_proxy_tokens_total[5m] offset 1h)) > 100
  for: 15m
  annotations:
    summary: "Token processing rate changed significantly"
```

## Grafana Quick Panels

### Token Rate (Time Series)
```promql
sum(rate(zai_proxy_tokens_total{variant="stable"}[5m])) by (direction)
```

### Cost Estimate (Stat)
```promql
# $0.01/1k input, $0.03/1k output
(sum(increase(zai_proxy_tokens_total{direction="input",variant="stable"}[24h])) * 0.00001) +
(sum(increase(zai_proxy_tokens_total{direction="output",variant="stable"}[24h])) * 0.00003)
```

### Latency Heatmap
```promql
sum(rate(zai_proxy_token_rate_seconds_bucket{variant="stable"}[5m])) by (le)
```

## Environment Variables

```bash
# Enable token counting (default: true)
TOKEN_COUNTING_ENABLED=true

# Model name for metrics (default: glm-4)
TOKENIZER_MODEL=glm-4

# Deployment variant (default: production)
DEPLOYMENT_VARIANT=stable  # or "canary"
```

## Troubleshooting

### No metrics appearing

1. Check token counting is enabled: `TOKEN_COUNTING_ENABLED=true`
2. Verify tokenizer initialized: Check logs for "Token counting enabled"
3. Check metrics endpoint: `curl http://zai-proxy:8080/metrics | grep tokens_total`

### Metrics not incrementing

1. Verify requests are being processed: Check `zai_proxy_requests_total`
2. Check for tokenization errors in logs
3. Verify token counts > 0: Only non-zero counts are recorded

### Wrong labels

1. Check `TOKENIZER_MODEL` environment variable
2. Check `DEPLOYMENT_VARIANT` environment variable
3. Verify labels in Prometheus: `zai_proxy_tokens_total{}`

## Full Documentation

See `/home/coder/ardenone-cluster/containers/zai-proxy/docs/metrics.md` for:
- Complete metric descriptions
- All Prometheus query examples
- Grafana dashboard templates
- Alerting rule suggestions
- Integration examples
