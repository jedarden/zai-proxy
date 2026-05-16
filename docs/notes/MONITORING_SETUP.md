# ZAI-Proxy Monitoring Setup - Dual Deployment

## Overview

This document describes the monitoring configuration for zai-proxy dual deployment (production + canary). The setup includes ServiceMonitors for Prometheus scraping, PrometheusRules for alerting, and a Grafana dashboard for visualization.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    apexalgo-iad Cluster                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────────┐         ┌──────────────────┐             │
│  │  Production      │         │     Canary       │             │
│  │  (mcp namespace) │         │ (devpod namespace)│             │
│  │                  │         │                  │             │
│  │  zai-proxy:1.0.0 │         │zai-proxy:1.2.0   │             │
│  │  TOKEN_COUNTING  │         │TOKEN_COUNTING    │             │
│  │  = false         │         │= true            │             │
│  └────────┬─────────┘         └────────┬─────────┘             │
│           │                             │                        │
│           │ /metrics                    │ /metrics               │
│           │ variant="production"        │ variant="canary"       │
│           ▼                             ▼                        │
│  ┌────────────────────────────────────────────────────┐        │
│  │         Prometheus (monitoring namespace)           │        │
│  │                                                     │        │
│  │  ServiceMonitor: zai-proxy-production              │        │
│  │    selector: app=zai-proxy, version=production     │        │
│  │    namespace: mcp                                  │        │
│  │    relabels: deployment_variant=production         │        │
│  │                                                     │        │
│  │  ServiceMonitor: zai-proxy-canary                  │        │
│  │    selector: app=zai-proxy-canary, version=canary  │        │
│  │    namespace: devpod                               │        │
│  │    relabels: deployment_variant=canary             │        │
│  │                                                     │        │
│  │  PrometheusRules: zai-proxy-canary-alerts          │        │
│  │    - Canaries-specific alerts                      │        │
│  │    - Comparison alerts vs production               │        │
│  └────────────────────────────────────────────────────┘        │
│           │                             │                        │
│           ▼                             ▼                        │
│  ┌────────────────────────────────────────────────────┐        │
│  │          Grafana Dashboard                         │        │
│  │  zai-proxy-dual-deployment.json                    │        │
│  │                                                     │        │
│  │  Panels:                                            │        │
│  │    - Worker Utilization (gauge)                    │        │
│  │    - Request Rate (timeseries)                     │        │
│  │    - Error Rate (timeseries)                       │        │
│  │    - Latency Comparison (P50/P95)                  │        │
│  │    - Current Rate Limit                            │        │
│  │    - Upstream Errors                               │        │
│  │    - Concurrent Requests (gauge)                   │        │
│  │    - Token Throughput (canary only)                │        │
│  │    - Token Counting Duration (canary only)         │        │
│  │    - Rate Limit Adjustments (canary only)          │        │
│  └────────────────────────────────────────────────────┘        │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

## File Structure

```
k8s/
├── production/
│   ├── deployment.yml       # Production deployment (mcp namespace)
│   └── service.yml          # Production service with version label
├── canary/
│   ├── deployment.yml       # Canary deployment (devpod namespace)
│   └── service.yml          # Canary service with version label
└── monitoring/
    ├── servicemonitor-production.yml    # Prometheus scraping for production
    ├── servicemonitor-canary.yml        # Prometheus scraping for canary
    ├── prometheus-rules.yml             # Canary-specific alerts
    └── grafana-dashboard-configmap.yml  # Grafana dashboard JSON
```

## ServiceMonitor Configuration

### Production ServiceMonitor

**File**: `k8s/monitoring/servicemonitor-production.yml`

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: zai-proxy-production
  namespace: monitoring
  labels:
    app: zai-proxy
    release: kube-prometheus-stack-arde
    variant: production
spec:
  selector:
    matchLabels:
      app: zai-proxy
      version: production
  namespaceSelector:
    matchNames:
    - mcp
  endpoints:
  - port: http
    path: /metrics
    interval: 30s
    relabelings:
    - sourceLabels: [__meta_kubernetes_service_label_version]
      targetLabel: deployment_variant
```

### Canary ServiceMonitor

**File**: `k8s/monitoring/servicemonitor-canary.yml`

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: zai-proxy-canary
  namespace: monitoring
  labels:
    app: zai-proxy
    release: kube-prometheus-stack-arde
    variant: canary
spec:
  selector:
    matchLabels:
      app: zai-proxy-canary
      version: canary
  namespaceSelector:
    matchNames:
    - devpod
  endpoints:
  - port: http
    path: /metrics
    interval: 30s
    relabelings:
    - sourceLabels: [__meta_kubernetes_service_label_version]
      targetLabel: deployment_variant
```

**Key Points**:
- Both ServiceMonitors add `deployment_variant` label via relabeling
- Production scrapes from `mcp` namespace
- Canary scrapes from `devpod` namespace
- Scrape interval: 30 seconds

## Metrics

### Application Metrics (from `metrics.go`)

| Metric Name | Type | Labels | Description |
|------------|------|--------|-------------|
| `zai_proxy_requests_total` | Counter | method, path, status_code, variant | Total requests |
| `zai_proxy_request_duration_seconds` | Histogram | method, path, status_code, variant | Request latency |
| `zai_proxy_concurrent_requests` | Gauge | variant | Active requests |
| `zai_proxy_worker_utilization_ratio` | Gauge | variant | Worker utilization % |
| `zai_proxy_rate_limit_requests_per_second` | Gauge | variant | Current rate limit |
| `zai_proxy_tokens_total` | Counter | direction, model, variant | Token counts |
| `zai_proxy_token_count_duration_seconds` | Histogram | variant | Token counting time |
| `zai_proxy_build_info` | Gauge | version, variant, commit, build_time | Build metadata |

### Label Mapping

| Application Label | ServiceMonitor Relabel | Dashboard Query |
|------------------|----------------------|-----------------|
| `variant="production"` | `deployment_variant="production"` | `deployment_variant="production"` |
| `variant="canary"` | `deployment_variant="canary"` | `deployment_variant="canary"` |

## Grafana Dashboard

**File**: `k8s/monitoring/grafana-dashboard-configmap.yml`

**Dashboard**: "ZAI Proxy - Production vs Canary"

### Panels

1. **Worker Utilization** (Gauge)
   - Shows concurrent requests vs max workers
   - Separate gauges for production and canary

2. **Request Rate** (Time Series)
   - `sum(rate(zai_proxy_requests_total{deployment_variant="production"}[5m]))`
   - `sum(rate(zai_proxy_requests_total{deployment_variant="canary"}[5m]))`

3. **Error Rate** (Time Series)
   - 5xx errors as percentage of total requests
   - Separate lines for production and canary

4. **Latency Comparison** (Time Series)
   - P50 and P95 percentiles
   - Separate lines for each deployment

5. **Current Rate Limit** (Time Series)
   - `zai_proxy_rate_limit_requests_per_second`

6. **Upstream Errors** (Time Series)
   - `zai_proxy_upstream_errors_total` by error_type

7. **Concurrent Requests** (Gauge)
   - Current active requests per deployment

8. **Token Throughput** (Time Series) - Canary Only
   - `zai_proxy_tokens_total` by direction and model
   - Only for canary since production has token counting disabled

9. **Token Counting Duration** (Time Series) - Canary Only
   - P95 of `zai_proxy_token_count_duration_seconds`

10. **Rate Limit Adjustments** (Time Series) - Canary Only
    - `zai_proxy_rate_limit_adjustments_total` by direction

### Dashboard Labels

```json
"tags": ["zai-proxy", "canary", "production", "monitoring"]
```

## Prometheus Rules - Canary Alerts

**File**: `k8s/monitoring/prometheus-rules.yml`

### Alert Rules

| Alert Name | Severity | Condition | Duration |
|-----------|----------|-----------|----------|
| `ZaiProxyCanaryHighErrorRate` | warning | Error rate > 5% | 5min |
| `ZaiProxyCanaryHighLatency` | warning | P95 > 10s | 5min |
| `ZaiProxyCanaryCrashLooping` | critical | Restart rate > 0 | 5min |
| `ZaiProxyCanaryNotReady` | critical | 0 ready pods | 2min |
| `ZaiProxyCanaryDegradedVsProduction` | warning | 2x error rate vs production | 10min |
| `ZaiProxyCanarySlowerThanProduction` | warning | 50% higher P95 vs production | 10min |
| `ZaiProxyCanaryTokenCountingSlow` | warning | Token counting P95 > 100ms | 5min |
| `ZaiProxyCanaryRateLimitAdjustingDown` | info | Rate limit decreasing | 5min |

### Alert Examples

**High Error Rate**:
```promql
(
  sum(rate(zai_proxy_requests_total{deployment_variant="canary",status_code=~"5.."}[5m]))
  /
  sum(rate(zai_proxy_requests_total{deployment_variant="canary"}[5m]))
) > 0.05
```

**Degraded vs Production**:
```promql
(
  sum(rate(zai_proxy_requests_total{deployment_variant="canary",status_code=~"5.."}[10m]))
  /
  sum(rate(zai_proxy_requests_total{deployment_variant="canary"}[10m]))
) > 2 * (
  sum(rate(zai_proxy_requests_total{deployment_variant="production",status_code=~"5.."}[10m]))
  /
  sum(rate(zai_proxy_requests_total{deployment_variant="production"}[10m])) + 0.01
)
```

## Deployment Labels

### Production Deployment

```yaml
# k8s/production/deployment.yml
spec:
  template:
    metadata:
      labels:
        app: zai-proxy
        version: production
    spec:
      containers:
      - name: proxy
        env:
        - name: DEPLOYMENT_VARIANT
          value: "production"
        - name: TOKEN_COUNTING_ENABLED
          value: "false"
```

### Canary Deployment

```yaml
# k8s/canary/deployment.yml
spec:
  template:
    metadata:
      labels:
        app: zai-proxy-canary
        version: canary
    spec:
      containers:
      - name: proxy
        env:
        - name: DEPLOYMENT_VARIANT
          value: "canary"
        - name: TOKEN_COUNTING_ENABLED
          value: "true"
```

## Verification

Run the verification script to check the monitoring setup:

```bash
./scripts/verify-monitoring.sh
```

This will check:
- Monitoring namespace exists
- ServiceMonitors are configured correctly
- PrometheusRules are deployed
- Grafana dashboard exists
- Relabel configs are correct

## Manual Metrics Testing

To verify metrics are being exported correctly:

```bash
# Production metrics endpoint
kubectl port-forward -n mcp deployment/zai-proxy 8080:8080
curl http://localhost:8080/metrics | grep zai_proxy

# Canary metrics endpoint
kubectl port-forward -n devpod deployment/zai-proxy-canary 8080:8080
curl http://localhost:8080/metrics | grep zai_proxy
```

## Prometheus Queries

### Compare request rates

```promql
sum(rate(zai_proxy_requests_total{deployment_variant="production"}[5m]))
sum(rate(zai_proxy_requests_total{deployment_variant="canary"}[5m]))
```

### Check token counting (canary only)

```promql
sum(rate(zai_proxy_tokens_total{deployment_variant="canary"}[5m])) by (direction, model)
```

### Compare error rates

```promql
sum(rate(zai_proxy_requests_total{deployment_variant="production",status_code=~"5.."}[5m])) /
sum(rate(zai_proxy_requests_total{deployment_variant="production"}[5m]))
```

## Troubleshooting

### Metrics not appearing

1. Check ServiceMonitor exists:
   ```bash
   kubectl get servicemonitor -n monitoring | grep zai-proxy
   ```

2. Check Service labels match ServiceMonitor selector:
   ```bash
   kubectl get service -n mcp zai-proxy -o jsonpath='{.metadata.labels}'
   kubectl get service -n devpod zai-proxy-canary -o jsonpath='{.metadata.labels}'
   ```

3. Check Prometheus is scraping:
   ```bash
   kubectl get configmap -n monitoring prometheus-kube-prometheus-prometheus-targets -o jsonpath='{.data}'
   ```

### Dashboard not loading

1. Check ConfigMap exists:
   ```bash
   kubectl get configmap -n monitoring zai-proxy-grafana-dashboard
   ```

2. Verify dashboard JSON is valid:
   ```bash
   kubectl get configmap -n monitoring zai-proxy-grafana-dashboard -o jsonpath='{.data.*}' | jq .
   ```

### Alerts not firing

1. Check PrometheusRule exists:
   ```bash
   kubectl get prometheusrule -n monitoring zai-proxy-canary-alerts
   ```

2. Verify rules are loaded:
   ```bash
   kubectl port-forward -n monitoring prometheus-kube-prometheus-prometheus-0 9090:9090
   curl http://localhost:9090/api/v1/rules | jq '.data.groups[] | select(.name=="zai_proxy_canary_alerts")'
   ```

## Summary

The monitoring setup provides:
- ✅ Separate scraping for production and canary deployments
- ✅ Version labels for metric filtering
- ✅ Grafana dashboard with side-by-side comparison
- ✅ Token counting metrics (canary only)
- ✅ Request rates, error rates, latency for both
- ✅ Canary-specific alerts that don't affect production
- ✅ Comparison alerts (canary vs production)

All monitoring resources are managed via GitOps (ArgoCD) by committing manifests to the repository.
