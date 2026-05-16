# zai-proxy Token Consumption - Grafana Integration

**Date:** 2026-02-14
**Status:** ✅ Dashboards Updated - Pending Verification

## Summary

Token consumption metrics are **already being collected** by the zai-proxy but were **not visualized in Grafana**. This document tracks the integration of token panels into the Grafana dashboards.

---

## ✅ Step 1: Grafana Dashboard Updates (COMPLETED)

### Changes Made

Added 7 new panels to both Grafana dashboards:
- `cluster-configuration/apexalgo-iad/monitoring/grafana-dashboard-zai-proxy.yml`
- `cluster-configuration/ardenone-cluster/monitoring/grafana-dashboard-zai-proxy.yml`

### New Panels

#### Row 1: Token Consumption Stats (y=62, h=4)
1. **Total Tokens (1h)** - Stat panel showing cumulative tokens in last hour
   - Query: `sum(increase(zai_proxy_tokens_total[1h]))`
   - Thresholds: Green < 50k < Yellow < 100k < Orange

2. **Input Tokens (1h)** - Stat panel for input tokens only
   - Query: `sum(increase(zai_proxy_tokens_total{direction="input"}[1h]))`
   - Thresholds: Green < 25k < Yellow < 50k < Orange

3. **Output Tokens (1h)** - Stat panel for output tokens only
   - Query: `sum(increase(zai_proxy_tokens_total{direction="output"}[1h]))`
   - Thresholds: Green < 25k < Yellow < 50k < Orange

4. **Output/Input Token Ratio** - Stat panel showing efficiency metric
   - Query: `sum(rate(zai_proxy_tokens_total{direction="output"}[5m])) / sum(rate(zai_proxy_tokens_total{direction="input"}[5m]))`
   - Shows how many output tokens are generated per input token

#### Row 2: Token Rate Time Series (y=66, h=8)
5. **Token Rate by Direction** - Time series comparing input vs output
   - Queries:
     - Input: `sum(rate(zai_proxy_tokens_total{direction="input"}[5m]))`
     - Output: `sum(rate(zai_proxy_tokens_total{direction="output"}[5m]))`
     - Total: `sum(rate(zai_proxy_tokens_total[5m]))`
   - Shows tokens/sec over time

6. **Token Rate by Deployment Variant** - Time series by stable/canary
   - Query: `sum(rate(zai_proxy_tokens_total[5m])) by (variant)`
   - Useful for A/B testing deployments

#### Row 3: Token Throughput Performance (y=74, h=8)
7. **Token Processing Throughput (p90/p99)** - Performance metrics
   - Queries:
     - p90: `histogram_quantile(0.90, sum(rate(zai_proxy_token_rate_bucket[5m])) by (le, direction))`
     - p99: `histogram_quantile(0.99, sum(rate(zai_proxy_token_rate_bucket[5m])) by (le, direction))`
   - Shows tokenization performance (tokens/sec at percentiles)

---

## ⏳ Step 2: ServiceMonitor Verification (BLOCKED BY HOOK)

### What Needs to be Checked

The ServiceMonitor configuration exists in the repo but verification was blocked by pre-tool hooks.

**ServiceMonitor Files:**
- `cluster-configuration/apexalgo-iad/mcp/zai-proxy-servicemonitor.yml`
- `cluster-configuration/ardenone-cluster/devpod/zai-proxy-servicemonitor.yml`

**ServiceMonitor Configuration:**
```yaml
endpoints:
  - port: http
    interval: 15s
    path: /metrics
    scrapeTimeout: 10s
```

### Manual Verification Steps

Run these commands to verify ServiceMonitor is deployed and scraping:

```bash
# Check ServiceMonitor exists in apexalgo-iad
export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig
kubectl get servicemonitor -n mcp zai-proxy

# Check ServiceMonitor exists in ardenone-cluster
kubectl get servicemonitor -n devpod zai-proxy

# Verify Prometheus is scraping the targets
# Access Prometheus UI and check:
# - Status > Targets > Look for "zai-proxy" jobs
# - Should see endpoints with "UP" status
```

---

## ⏳ Step 3: Prometheus Metrics Verification (BLOCKED BY HOOK)

### What Needs to be Checked

Verify that token metrics are being scraped by Prometheus.

### Manual Verification Steps

#### Option 1: Direct Metrics Endpoint Check
```bash
# Port-forward to zai-proxy pod
kubectl port-forward -n devpod svc/zai-proxy 8080:8080

# In another terminal, check metrics
curl -s http://localhost:8080/metrics | grep "zai_proxy_tokens"

# Expected output (example):
# zai_proxy_tokens_total{direction="input",model="glm-4",variant="production"} 12345
# zai_proxy_tokens_total{direction="output",model="glm-4",variant="production"} 67890
# zai_proxy_token_count_duration_seconds_bucket{variant="production",le="0.001"} 100
# zai_proxy_token_rate_bucket{direction="input",model="glm-4",variant="production",le="1000"} 50
```

#### Option 2: Prometheus UI Query
```bash
# Access Prometheus UI (port-forward if needed)
kubectl port-forward -n monitoring svc/prometheus-operated 9090:9090

# Open browser: http://localhost:9090
# Run these queries in the UI:

# 1. Check if metric exists
zai_proxy_tokens_total

# 2. Total tokens in last hour
sum(increase(zai_proxy_tokens_total[1h]))

# 3. Token rate by direction
sum(rate(zai_proxy_tokens_total[5m])) by (direction)

# 4. Input vs Output ratio
sum(rate(zai_proxy_tokens_total{direction="output"}[5m]))
/
sum(rate(zai_proxy_tokens_total{direction="input"}[5m]))
```

#### Option 3: PromQL via kubectl
```bash
# Query Prometheus via API
POD=$(kubectl get pod -n monitoring -l app.kubernetes.io/name=prometheus -o jsonpath='{.items[0].metadata.name}')

kubectl exec -n monitoring $POD -- wget -qO- \
  'http://localhost:9090/api/v1/query?query=sum(increase(zai_proxy_tokens_total[1h]))'
```

---

## Expected Metrics

The zai-proxy exposes these token-related metrics:

### Primary Metrics
| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `zai_proxy_tokens_total` | Counter | `direction`, `model`, `variant` | Total tokens processed |
| `zai_proxy_token_count_duration_seconds` | Histogram | `variant` | Duration of token counting |
| `zai_proxy_token_rate_seconds` | Histogram | `direction`, `model`, `variant` | Time per token |
| `zai_proxy_token_rate` | Histogram | `direction`, `model`, `variant` | Tokens per second throughput |

### Label Values
- **direction**: `input`, `output`
- **model**: `glm-4` (default), others if configured
- **variant**: `production`, `canary`, `stable`, `test`

---

## Deployment Status

### apexalgo-iad (Production)
- **Pod Status:** ✅ Running (1/1) - `zai-proxy-95fc547d7-gjn7q`
- **ServiceMonitor:** ⏳ Not confirmed (verification blocked)
- **Dashboard:** ✅ Updated with token panels

### ardenone-cluster (Dev/Local)
- **Pod Status:** ⚠️ Mixed (1 running, 3 failing - image pull issues)
- **ServiceMonitor:** ⏳ Not confirmed (verification blocked)
- **Dashboard:** ✅ Updated with token panels

---

## Next Steps

### Immediate Actions
1. ✅ **DONE**: Add token panels to Grafana dashboards
2. ⏳ **TODO**: Verify ServiceMonitor is deployed and scraping
3. ⏳ **TODO**: Confirm Prometheus is collecting token metrics
4. ⏳ **TODO**: Access Grafana UI to view the new token panels

### Follow-up Actions
1. **Deploy ServiceMonitor** (if not already deployed):
   ```bash
   # For apexalgo-iad
   kubectl apply -f cluster-configuration/apexalgo-iad/mcp/zai-proxy-servicemonitor.yml

   # For ardenone-cluster
   kubectl apply -f cluster-configuration/ardenone-cluster/devpod/zai-proxy-servicemonitor.yml
   ```

2. **Fix image pull errors in ardenone-cluster**:
   - Check failing pods: `kubectl describe pod -n devpod zai-proxy-64f66d59d6-7f7g7`
   - Verify image exists and is accessible

3. **Commit and push dashboard changes**:
   ```bash
   cd /home/coder/ardenone-cluster
   git add cluster-configuration/*/monitoring/grafana-dashboard-zai-proxy.yml
   git commit -m "feat(zai-proxy): add token consumption panels to Grafana dashboard

   - Add 7 new panels for token metrics visualization
   - Total tokens (1h), Input/Output breakdown
   - Token rate time series by direction and variant
   - Token processing throughput (p90/p99)
   - Metrics already collected, now visualized

   Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
   git push origin main
   ```

4. **Wait for ArgoCD sync**:
   - ArgoCD will detect the ConfigMap changes
   - Grafana will reload the dashboard automatically
   - Check sync status: `kubectl get application -n argocd`

---

## Troubleshooting

### Dashboard doesn't show token data

**Symptom:** Panels show "No data"

**Diagnosis:**
1. Check if ServiceMonitor is deployed:
   ```bash
   kubectl get servicemonitor -n devpod zai-proxy
   ```

2. Check Prometheus targets:
   - Open Prometheus UI
   - Status > Targets
   - Look for `zai-proxy` job
   - Should be "UP" status

3. Verify metrics endpoint:
   ```bash
   kubectl exec -n devpod deploy/zai-proxy -- wget -qO- http://localhost:8080/metrics | grep tokens
   ```

**Solutions:**
- If ServiceMonitor missing: Apply it manually
- If endpoint not exposing metrics: Check proxy version (should be v1.1.0+)
- If Prometheus not scraping: Check ServiceMonitor labels match Prometheus scrape config

### Queries return no results

**Symptom:** PromQL queries return empty result

**Diagnosis:**
```bash
# Check if metric name exists in Prometheus
# (access Prometheus UI and search for "zai_proxy_tokens")
```

**Solutions:**
- Metric might be new - wait 15-30 seconds for first scrape
- Check scrape interval: 15s default
- Verify time range in Grafana (use "Last 5 minutes" for testing)

---

## References

- **Metrics Documentation:** `ardenone-cluster/docs/zai-proxy-metrics.md`
- **Proxy Source:** `ardenone-cluster/containers/zai-proxy/`
- **Metrics Implementation:** `ardenone-cluster/containers/zai-proxy/metrics.go`
- **Token Tracking Code:** `ardenone-cluster/containers/zai-proxy/main.go:323,496,521`

---

## Verification Checklist

Once hooks are resolved or manual verification is performed:

- [ ] ServiceMonitor deployed in `mcp` namespace (apexalgo-iad)
- [ ] ServiceMonitor deployed in `devpod` namespace (ardenone-cluster)
- [ ] Prometheus targets show zai-proxy as "UP"
- [ ] `/metrics` endpoint exposes `zai_proxy_tokens_total`
- [ ] Grafana dashboard shows token consumption data
- [ ] All 7 new panels render correctly
- [ ] Token metrics update in real-time (15s interval)
- [ ] Commit pushed to main branch
- [ ] ArgoCD synced the dashboard changes
