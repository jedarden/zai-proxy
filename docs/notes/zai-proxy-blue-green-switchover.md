# Z.AI Proxy Blue-Green Deployment - Traffic Switchover

## Current Status

- **V1 (Old)**: `zai-proxy` deployment running `ronaldraygun/zai-proxy:1.1.0`
- **V2 (New)**: `zai-proxy-v2` deployment running `ronaldraygun/zai-proxy:1.3.0`
- **Service**: Currently routes to V1 (`selector: app=zai-proxy` without version label)

## Switchover Procedure

### Step 1: Verify V2 is Running and Healthy

```bash
kubectl get deployment zai-proxy-v2 -n devpod
kubectl get pods -n devpod -l version=v2
kubectl logs -n devpod -l version=v2 --tail=20

# Test V2 directly (bypass service)
POD_IP=$(kubectl get pod -n devpod -l version=v2 -o jsonpath='{.items[0].status.podIP}')
curl http://$POD_IP:8080/health
curl http://$POD_IP:8080/metrics | grep zai_proxy_rate_limit
```

### Step 2: Update Service Selector to Route to V2

```bash
kubectl patch service zai-proxy -n devpod --type=merge -p '
{
  "spec": {
    "selector": {
      "app": "zai-proxy",
      "version": "v2"
    }
  }
}'
```

### Step 3: Verify Traffic is Flowing to V2

```bash
# Check service endpoints
kubectl get endpoints zai-proxy -n devpod

# Test through service
curl http://zai-proxy.devpod.svc.cluster.local:8080/health
curl http://zai-proxy.devpod.svc.cluster.local:8080/metrics | grep "deployment_variant"

# Should see: deployment_variant="v2"
```

### Step 4: Monitor Metrics in Grafana

Check that new metrics are now available:
- Current Rate Limit
- Token counting metrics
- Adaptive rate limit adjustments

### Step 5: Delete Old V1 Deployment (Optional - Keep for Rollback)

**Option A: Keep V1 for Quick Rollback (Recommended for 24h)**
```bash
# Scale V1 to 0 replicas but keep deployment
kubectl scale deployment zai-proxy -n devpod --replicas=0
```

**Option B: Delete V1 Completely**
```bash
kubectl delete deployment zai-proxy -n devpod
```

## Rollback Procedure (If Needed)

If V2 has issues, instantly rollback to V1:

```bash
# If V1 is scaled to 0
kubectl scale deployment zai-proxy -n devpod --replicas=1

# Switch service back to V1
kubectl patch service zai-proxy -n devpod --type=merge -p '
{
  "spec": {
    "selector": {
      "app": "zai-proxy"
    }
  }
}'

# Or directly update to no version label
kubectl patch service zai-proxy -n devpod --type=json -p='[
  {"op": "remove", "path": "/spec/selector/version"}
]'
```

## Benefits of This Approach

1. **Zero Downtime**: V2 starts before V1 stops
2. **Instant Rollback**: Keep V1 running or scaled to 0
3. **Gradual Verification**: Test V2 directly before switching traffic
4. **Safe**: Can test without affecting users

## Worker Impact

- Workers will continue using the proxy without interruption
- Existing connections may be briefly reset during service selector change
- Rate limiting will reset to initial values on V2 (RATE_LIMIT_INITIAL=2)

## Monitoring Checklist

- [ ] V2 pod is Running
- [ ] V2 health check passes
- [ ] V2 metrics endpoint accessible
- [ ] Service endpoints point to V2 pod
- [ ] Workers can make requests successfully
- [ ] Grafana shows new metrics
- [ ] No 429 or 502 errors in V2 logs
