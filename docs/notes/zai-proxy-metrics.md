# Z.AI Proxy Metrics and Autoscaling

## Overview

The zai-proxy has been enhanced with comprehensive Prometheus metrics and autoscaling capabilities to maximize utilization of the z.ai coding subscription.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Z.AI Proxy Cluster                      │
│                                                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐          │
│  │  zai-proxy  │  │  zai-proxy  │  │  zai-proxy  │          │
│  │   Pod 1     │  │   Pod 2     │  │   Pod N     │          │
│  │             │  │             │  │             │          │
│  │ MAX_WORKERS │  │ MAX_WORKERS │  │ MAX_WORKERS │          │
│  │     = 20    │  │     = 20    │  │     = 20    │          │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘          │
│         │                │                │                  │
│         └────────────────┴────────────────┘                  │
│                          │                                   │
│                   /metrics endpoint                          │
│                          │                                   │
│         ┌────────────────▼────────────────┐                  │
│         │      ServiceMonitor             │                  │
│         │   (scrapes every 15s)           │                  │
│         └────────────────┬────────────────┘                  │
│                          │                                   │
│         ┌────────────────▼────────────────┐                  │
│         │       Prometheus                │                  │
│         │  (stores time-series data)      │                  │
│         └────────────────┬────────────────┘                  │
│                          │                                   │
│         ┌────────────────▼────────────────┐                  │
│         │  HorizontalPodAutoscaler        │                  │
│         │  - CPU > 70%: scale up          │                  │
│         │  - Memory > 80%: scale up       │                  │
│         │  - Worker util > 80%: scale up  │                  │
│         │  Min: 1, Max: 5 replicas        │                  │
│         └─────────────────────────────────┘                  │
│                                                               │
│  ┌───────────────────────────────────────────────────────┐   │
│  │              Grafana Dashboard                        │   │
│  │  - Worker utilization gauge                           │   │
│  │  - Request rate by status code                        │   │
│  │  - Concurrent requests vs max workers                 │   │
│  │  - Request duration percentiles (p50, p90, p99)       │   │
│  │  - Request/response size metrics                      │   │
│  │  - Upstream error tracking                            │   │
│  └───────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Metrics Exposed

### Request Metrics

1. **`zai_proxy_requests_total`** (Counter)
   - Total number of requests by method, path, and status code
   - Labels: `method`, `path`, `status_code`
   - Example: `zai_proxy_requests_total{method="POST",path="/v1/messages",status_code="200"}`

2. **`zai_proxy_request_duration_seconds`** (Histogram)
   - Request duration in seconds
   - Buckets: 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s, 30s, 60s, 120s, 300s
   - Labels: `method`, `path`, `status_code`
   - Useful for: p50, p90, p99 latency calculations

3. **`zai_proxy_request_size_bytes`** (Histogram)
   - Request payload size in bytes
   - Exponential buckets: 100, 1000, 10000, ...
   - Labels: `method`, `path`

4. **`zai_proxy_response_size_bytes`** (Histogram)
   - Response payload size in bytes
   - Exponential buckets: 100, 1000, 10000, ...
   - Labels: `method`, `path`, `status_code`

### Worker Metrics

5. **`zai_proxy_concurrent_requests`** (Gauge)
   - Number of requests currently being processed
   - Real-time view of active connections

6. **`zai_proxy_max_workers`** (Gauge)
   - Maximum number of concurrent workers allowed per pod
   - Set via `MAX_WORKERS` environment variable (default: 20)

7. **`zai_proxy_worker_utilization_ratio`** (Gauge)
   - Current worker utilization ratio (concurrent_requests / max_workers)
   - Range: 0.0 to 1.0+
   - **Key metric for autoscaling decisions**

### Error Metrics

8. **`zai_proxy_upstream_errors_total`** (Counter)
   - Total number of upstream errors by type
   - Labels: `error_type`
   - Error types:
     - `request_creation` - Failed to create upstream request
     - `upstream_connection` - Failed to connect to z.ai API
     - `read_error` - Error reading response from z.ai
     - `write_error` - Error writing response to client

## Configuration

### Environment Variables

Both deployments (`ardenone-cluster/devpod` and `apexalgo-iad/mcp`) support:

```yaml
env:
  - name: ZAI_API_KEY
    valueFrom:
      secretKeyRef:
        name: zai-api-key
        key: api-key

  - name: MAX_WORKERS
    value: "20"  # Adjust based on subscription limits
```

**MAX_WORKERS**: Controls the maximum number of concurrent requests a single pod will handle. When exceeded, the proxy returns `503 Service Unavailable` to trigger autoscaling.

### Autoscaling Behavior

**Scale Up:**
- Stabilization window: 30 seconds
- Policies:
  - Can double pod count instantly (100% increase)
  - Or add 2 pods at a time
  - Uses the most aggressive policy

**Scale Down:**
- Stabilization window: 300 seconds (5 minutes)
- Policies:
  - Maximum 25% reduction at a time
  - Slow scale-down to avoid thrashing

**Replica Limits:**
- Minimum: 1 pod
- Maximum: 5 pods

### Scaling Triggers

1. **CPU Utilization > 70%**
2. **Memory Utilization > 80%**
3. **Worker Utilization > 80%** (requires prometheus-adapter - see below)

## Prometheus Adapter Configuration (Optional)

To enable custom metric-based autoscaling (worker utilization), configure prometheus-adapter:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-adapter-config
  namespace: monitoring
data:
  config.yaml: |
    rules:
    - seriesQuery: 'zai_proxy_worker_utilization_ratio'
      resources:
        overrides:
          namespace: {resource: "namespace"}
          pod: {resource: "pod"}
      name:
        matches: "^(.*)$"
        as: "zai_proxy_worker_utilization_ratio"
      metricsQuery: 'avg_over_time(zai_proxy_worker_utilization_ratio[2m])'
```

Then uncomment the custom metric section in the HPA manifests.

## Querying Metrics

### Useful PromQL Queries

**Request rate (req/s):**
```promql
sum(rate(zai_proxy_requests_total[5m]))
```

**Request rate by status code:**
```promql
sum(rate(zai_proxy_requests_total[5m])) by (status_code)
```

**p99 latency:**
```promql
histogram_quantile(0.99, sum(rate(zai_proxy_request_duration_seconds_bucket[5m])) by (le))
```

**Worker utilization (current):**
```promql
sum(zai_proxy_worker_utilization_ratio)
```

**Total concurrent capacity:**
```promql
sum(zai_proxy_max_workers)
```

**Error rate:**
```promql
sum(rate(zai_proxy_upstream_errors_total[5m])) by (error_type)
```

**Success rate (non-5xx):**
```promql
sum(rate(zai_proxy_requests_total{status_code!~"5.."}[5m]))
/
sum(rate(zai_proxy_requests_total[5m]))
```

## Grafana Dashboard

A pre-configured Grafana dashboard is deployed to `monitoring` namespace:

**Panels:**
1. **Worker Utilization Gauge** - Real-time utilization percentage
2. **Request Rate by Status Code** - Time-series of req/s grouped by HTTP status
3. **Concurrent Requests vs Max Workers** - Visual capacity tracking
4. **Request Duration Percentiles** - p50, p90, p99 latency trends
5. **Request/Response Size (p90)** - Bandwidth usage
6. **Upstream Errors** - Error rate by type

**Access:**
- Navigate to Grafana (check IngressRoute for URL)
- Search for "Z.AI Proxy Metrics" dashboard

## Deployment Workflow

### 1. Build New Container Image

```bash
cd /home/coder/ardenone-cluster

# Version is already bumped to 1.1.0
git add containers/zai-proxy/
git commit -m "feat(zai-proxy): add Prometheus metrics and worker pool management

- Add comprehensive Prometheus metrics for requests, durations, sizes
- Track concurrent requests and worker utilization
- Add MAX_WORKERS environment variable for capacity control
- Expose /metrics endpoint for Prometheus scraping

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"

git push origin main
```

**Wait for GitHub Actions to complete** (~5 minutes). Check:
- https://github.com/ardenone/ardenone-cluster/actions

### 2. Deploy ServiceMonitors and HPAs

```bash
git add cluster-configuration/
git commit -m "feat(zai-proxy): add ServiceMonitors, HPAs, and Grafana dashboard

- Add ServiceMonitor for both ardenone-cluster and apexalgo-iad
- Configure HorizontalPodAutoscaler with CPU/memory/worker metrics
- Deploy Grafana dashboard for visualization
- Update deployments with MAX_WORKERS=20 and metrics port

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"

git push origin main
```

ArgoCD will automatically sync these changes.

### 3. Update Deployment to v1.1.0

**ONLY AFTER GitHub Actions build succeeds:**

```bash
# Update image version in both deployments
sed -i 's|ronaldraygun/zai-proxy:1.0.0|ronaldraygun/zai-proxy:1.1.0|g' \
  cluster-configuration/ardenone-cluster/devpod/zai-proxy.yml \
  cluster-configuration/apexalgo-iad/mcp/zai-proxy.yml

git add cluster-configuration/
git commit -m "chore(zai-proxy): bump to v1.1.0 with metrics support

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"

git push origin main
```

### 4. Verify Deployment

**Check pods:**
```bash
kubectl get pods -n devpod -l app=zai-proxy
kubectl get pods -n mcp -l app=zai-proxy --kubeconfig=/home/coder/.kube/apexalgo-iad.kubeconfig
```

**Check metrics endpoint:**
```bash
kubectl port-forward -n devpod svc/zai-proxy 8080:8080 &
curl http://localhost:8080/metrics | grep zai_proxy
```

**Check HPA status:**
```bash
kubectl get hpa -n devpod zai-proxy
kubectl describe hpa -n devpod zai-proxy
```

**Check ServiceMonitor:**
```bash
kubectl get servicemonitor -n devpod zai-proxy
```

## Tuning for Maximum Subscription Utilization

### Strategy 1: Fixed Worker Pool

Set `MAX_WORKERS` based on your z.ai subscription limits:

- **If subscription allows 50 concurrent requests:**
  - Set `MAX_WORKERS=10` with `maxReplicas=5` (10 * 5 = 50 total)
  - Or `MAX_WORKERS=25` with `maxReplicas=2` (25 * 2 = 50 total)

### Strategy 2: Dynamic Scaling

1. Monitor `zai_proxy_worker_utilization_ratio` in Grafana
2. If consistently below 0.5 (50%), reduce `MAX_WORKERS` or `maxReplicas`
3. If frequently hitting 1.0 (100%), increase `MAX_WORKERS` or `maxReplicas`

### Strategy 3: Cost Optimization

- **Low-traffic periods:** Set `minReplicas=1`
- **High-traffic periods:** Use aggressive scale-up policies
- **Balance:** Slow scale-down (5 min stabilization) prevents over-provisioning

## Alerting Rules (Optional)

Add Prometheus alerting rules to get notified of issues:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: zai-proxy-alerts
  namespace: monitoring
spec:
  groups:
  - name: zai-proxy
    interval: 30s
    rules:
    - alert: ZaiProxyHighErrorRate
      expr: |
        sum(rate(zai_proxy_requests_total{status_code=~"5.."}[5m]))
        /
        sum(rate(zai_proxy_requests_total[5m]))
        > 0.05
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Z.AI Proxy error rate > 5%"

    - alert: ZaiProxyHighUtilization
      expr: sum(zai_proxy_worker_utilization_ratio) > 0.9
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "Z.AI Proxy worker utilization > 90% for 10 minutes"

    - alert: ZaiProxyAtMaxCapacity
      expr: |
        sum(zai_proxy_concurrent_requests)
        >=
        sum(zai_proxy_max_workers)
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "Z.AI Proxy at maximum capacity - requests being rejected"
```

## Troubleshooting

### Metrics not appearing in Prometheus

1. Check ServiceMonitor is deployed:
   ```bash
   kubectl get servicemonitor -n devpod
   ```

2. Check Prometheus is scraping:
   ```bash
   kubectl logs -n monitoring -l app=prometheus
   ```

3. Verify metrics endpoint is accessible:
   ```bash
   kubectl exec -n devpod deploy/zai-proxy -- wget -O- http://localhost:8080/metrics
   ```

### HPA not scaling

1. Check HPA status:
   ```bash
   kubectl describe hpa -n devpod zai-proxy
   ```

2. Verify metrics-server is running:
   ```bash
   kubectl get pods -n kube-system -l k8s-app=metrics-server
   ```

3. Check current metrics:
   ```bash
   kubectl get hpa -n devpod zai-proxy -o yaml
   ```

### Pods stuck at capacity (503 errors)

1. Check worker utilization:
   ```promql
   sum(zai_proxy_worker_utilization_ratio)
   ```

2. Increase `MAX_WORKERS` or `maxReplicas` in HPA

3. Verify HPA is allowed to scale up:
   ```bash
   kubectl get hpa -n devpod zai-proxy
   # Current replicas should be < maxReplicas
   ```

## Next Steps

1. **Monitor for 1-2 weeks** - Collect baseline metrics
2. **Tune MAX_WORKERS** - Adjust based on actual utilization
3. **Enable custom metrics** - Configure prometheus-adapter for worker-based autoscaling
4. **Set up alerts** - Get notified of capacity issues
5. **Cost analysis** - Measure subscription utilization vs pod costs

## References

- Prometheus Operator: https://prometheus-operator.dev/
- HPA documentation: https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/
- Prometheus adapter: https://github.com/kubernetes-sigs/prometheus-adapter
