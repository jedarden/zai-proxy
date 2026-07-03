# Z.AI Proxy - Dual Deployment Workflow Guide

This guide covers managing workers across production and canary deployments of the zai-proxy service.

## Overview

The zai-proxy service supports **dual deployment mode** for safe testing and gradual rollout:

| Deployment | Service Name | Purpose | Endpoint URL |
|------------|--------------|---------|--------------|
| **Production** | `zai-proxy.devpod.svc.cluster.local:8080` | Live traffic | `http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic` |
| **Canary** | `zai-proxy-test.devpod.svc.cluster.local:8080` | Testing new versions | `http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic` |
| **Split Traffic** | `zai-proxy-canary.devpod.svc.cluster.local:8080` | Weighted traffic split | `http://zai-proxy-canary.devpod.svc.cluster.local:8080/api/anthropic` |

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │       Workers (claude-code-glm)      │
                    └──────────────┬──────────────────────┘
                                   │
                    ┌──────────────▼──────────────────────┐
                    │      ANTHROPIC_BASE_URL Setting      │
                    │  (selects production or canary)      │
                    └──────────────┬──────────────────────┘
                                   │
                ┌──────────────────┼──────────────────┐
                │                  │                  │
                ▼                  ▼                  ▼
    ┌───────────────────┐ ┌──────────────┐ ┌─────────────────────┐
    │   Production      │ │   Canary     │ │  Split Traffic      │
    │  zai-proxy:8080   │ │zai-proxy-test│ │ zai-proxy-canary    │
    │  (variant=prod)   │ │ (variant=test)│ │ (weighted by pods)  │
    └───────────────────┘ └──────────────┘ └─────────────────────┘
```

## 1. Configuring Workers for Production vs Canary

### Method 1: Direct Endpoint Configuration

Workers are configured via their `settings.json` file in the agent directory.

**Production Endpoint (Default):**
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic"
  }
}
```

**Canary Endpoint:**
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic"
  }
}
```

**Split Traffic Endpoint:**
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://zai-proxy-canary.devpod.svc.cluster.local:8080/api/anthropic"
  }
}
```

### Method 2: Override via Environment Variable

When launching workers, override the endpoint without modifying settings:

```bash
# Launch worker with canary endpoint
export ANTHROPIC_BASE_URL="http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic"
./agents/claude-code-glm-47/launch.sh claude-glm-canary-test

# Launch worker with production endpoint
export ANTHROPIC_BASE_URL="http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic"
./agents/claude-code-glm-47/launch.sh claude-glm-prod
```

### Example: claude-code-glm-47 Configuration

**Location:** `/home/coder/claude-config/agents/claude-code-glm-47/settings.json`

**Current (Production):**
```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "proxy-handles-auth",
    "ANTHROPIC_BASE_URL": "http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "glm-4.7",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "glm-4.7",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-4.7"
  }
}
```

## 2. Verifying Worker Endpoint Configuration

### Check Active Worker Configuration

```bash
# Attach to a worker session
tmux attach -t claude-code-glm-47-alpha

# Inside the session, check environment
echo $ANTHROPIC_BASE_URL
# Expected output for production:
# http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic

# Check settings.json
cat $CLAUDE_CONFIG_DIR/settings.json | grep ANTHROPIC_BASE_URL
```

### Verify Service Availability

```bash
# Test production endpoint
curl -s http://zai-proxy.devpod.svc.cluster.local:8080/health
# Expected: {"status":"ok"}

# Test canary endpoint
curl -s http://zai-proxy-test.devpod.svc.cluster.local:8080/health
# Expected: {"status":"ok"}

# Test split traffic endpoint
curl -s http://zai-proxy-canary.devpod.svc.cluster.local:8080/health
# Expected: {"status":"ok"}
```

### Check Which Pods Worker Is Using

```bash
# From within a devpod, check DNS resolution
nslookup zai-proxy.devpod.svc.cluster.local
nslookup zai-proxy-test.devpod.svc.cluster.local

# Check service endpoints
kubectl get endpoints -n mcp | grep zai-proxy
```

### Monitor Metrics with Deployment Variant Labels

The proxy exports metrics with the `deployment_variant` label:

```promql
# Check requests per variant
sum by (deployment_variant) (rate(zai_proxy_requests_total[5m]))

# Check token usage per variant
sum by (deployment_variant) (rate(zai_proxy_tokens_total[5m]))
```

## 3. Testing Procedure: Canary Deployments

### Step 1: Launch Test Worker Against Canary

```bash
cd /home/coder/claude-config

# Launch worker configured for canary
export ANTHROPIC_BASE_URL="http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic"
./agents/claude-code-glm-47/launch.sh claude-glm-canary-test
```

### Step 2: Verify Canary Configuration

```bash
# Attach to verify
tmux attach -t claude-glm-canary-test

# In the worker session, verify
echo "Using endpoint: $ANTHROPIC_BASE_URL"
# Should show: http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic

# Detach: Ctrl+B, D
```

### Step 3: Run Test Tasks

```bash
# In the worker session or via bead assignment
# Test simple task
br create "Test canary endpoint connectivity" \
  --description "Verify worker can successfully make API calls through canary endpoint" \
  --labels testing,canary

# Worker should process this and make API calls through canary
```

### Step 4: Monitor Canary Metrics

```bash
# Check canary proxy metrics
curl -s http://zai-proxy-test.devpod.svc.cluster.local:8080/metrics | grep zai_proxy_requests_total

# Verify variant label in metrics
curl -s http://zai-proxy-test.devpod.svc.cluster.local:8080/metrics | grep deployment_variant
# Should show: deployment_variant="canary"
```

### Step 5: Verify Functionality

**Checklist:**
- [ ] Worker makes successful API calls through canary
- [ ] Token counting works (check logs for "Token usage")
- [ ] No errors in worker logs
- [ ] Metrics show `deployment_variant="canary"`
- [ ] Response times are acceptable

## 4. Migration Checklist: Production to Canary

### Pre-Migration

- [ ] **Verify canary deployment is healthy**
  ```bash
  kubectl get pods -n mcp -l app=zai-proxy,variant=test
  kubectl logs -n mcp deployment/zai-proxy-test --tail=50
  ```

- [ ] **Run smoke tests on canary endpoint**
  ```bash
  curl -X POST http://zai-proxy-test.devpod.svc.cluster.local:8080/v1/messages \
    -H "Content-Type: application/json" \
    -H "x-api-key: $ZAI_API_KEY" \
    -d '{"model":"claude-3-sonnet","messages":[{"role":"user","content":"test"}],"max_tokens":10}'
  ```

- [ ] **Review canary metrics for baseline**
  ```bash
  curl -s http://zai-proxy-test.devpod.svc.cluster.local:8080/metrics
  ```

### Migration

- [ ] **Stop workers using production endpoint**
  ```bash
  # List active workers
  tlist

  # Kill production workers (one by one or all)
  tkill claude-code-glm-47-alpha
  tkill claude-code-glm-47-bravo
  ```

- [ ] **Update worker settings.json to use canary**
  ```bash
  # Backup current config
  cp /home/coder/claude-config/agents/claude-code-glm-47/settings.json \
     /home/coder/claude-config/agents/claude-code-glm-47/settings.json.bak

  # Update ANTHROPIC_BASE_URL (use Edit tool or manual edit)
  # Change from: http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic
  # Change to:   http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic
  ```

- [ ] **Launch workers with new canary configuration**
  ```bash
  cd /home/coder/claude-config
  ./scripts/spawn-workers.sh --workspace=/path/to/project --executor=claude-code-glm-47 --workers=3
  ```

### Post-Migration Verification

- [ ] **Verify workers are using canary endpoint**
  ```bash
  # Attach to each worker and check
  tmux attach -t <worker-name>
  echo $ANTHROPIC_BASE_URL
  ```

- [ ] **Monitor canary metrics for increased load**
  ```promql
  rate(zai_proxy_requests_total{deployment_variant="canary"}[5m])
  ```

- [ ] **Check worker logs for errors**
  ```bash
  tail -f ~/.beads-workers/*.log
  ```

- [ ] **Verify production metrics show decreased load**
  ```promql
  rate(zai_proxy_requests_total{deployment_variant="production"}[5m])
  ```

## 5. Emergency Fallback to Production

### Quick Fallback (Single Worker)

```bash
# Kill worker using problematic endpoint
tkill <worker-name>

# Relaunch with production endpoint override
export ANTHROPIC_BASE_URL="http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic"
cd /home/coder/claude-config
./agents/claude-code-glm-47/launch.sh <worker-name>-fallback
```

### Bulk Fallback (All Workers)

```bash
# 1. Kill all affected workers
tkill \$(tmux list-sessions | grep claude-code-glm | cut -d: -f1)

# 2. Restore production settings from backup
cp /home/coder/claude-config/agents/claude-code-glm-47/settings.json.bak \
   /home/coder/claude-config/agents/claude-code-glm-47/settings.json

# 3. Relaunch all workers
cd /home/coder/claude-config
./scripts/spawn-workers.sh --workspace=/path/to/project --executor=claude-code-glm-47 --workers=3
```

### Temporary Override (Without Config Change)

```bash
# For immediate testing or debugging
export ANTHROPIC_BASE_URL="http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic"
./agents/claude-code-glm-47/launch.sh emergency-prod-worker
```

## 6. Traffic Splitting (Gradual Rollout)

For gradual rollout, use the `zai-proxy-canary` service which splits traffic based on replica counts:

### Configure Traffic Split

```bash
# Current configuration: 90% production, 10% canary
# 9 production pods + 1 canary pod = 90/10 split

# To change to 50/50 split:
kubectl scale deployment/zai-proxy -n mcp --replicas=5
kubectl scale deployment/zai-proxy-test -n mcp --replicas=5

# To change to 100% canary (full cutover):
kubectl scale deployment/zai-proxy -n mcp --replicas=0
kubectl scale deployment/zai-proxy-test -n mcp --replicas=10
```

### Configure Workers for Split Traffic

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://zai-proxy-canary.devpod.svc.cluster.local:8080/api/anthropic"
  }
}
```

Workers will then receive a mix of production and canary responses based on the traffic split.

## 7. Monitoring and Troubleshooting

### Check Deployment Status

```bash
# Production pods
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# Canary pods
kubectl get pods -n mcp -l app=zai-proxy,variant=test

# All zai-proxy pods
kubectl get pods -n mcp -l app=zai-proxy
```

### View Logs

```bash
# Production logs
kubectl logs -f -n mcp deployment/zai-proxy

# Canary logs
kubectl logs -f -n mcp deployment/zai-proxy-test

# Worker logs
tail -f ~/.beads-workers/<worker-name>.log
```

### Prometheus Queries

```promql
# Request rate by deployment variant
sum by (deployment_variant) (
  rate(zai_proxy_requests_total[5m])
)

# Error rate by deployment variant
sum by (deployment_variant) (
  rate(zai_proxy_requests_total{status=~"5.."}[5m])
)

# Token usage by deployment variant
sum by (deployment_variant) (
  rate(zai_proxy_tokens_total[5m])
)

# P95 latency by deployment variant
histogram_quantile(0.95,
  sum by (deployment_variant, le) (
    rate(zai_proxy_request_duration_seconds_bucket[5m])
  )
)
```

### Common Issues

**Issue: Workers getting connection errors**
- Verify service is running: `kubectl get svc -n mcp | grep zai-proxy`
- Check DNS resolution from devpod: `nslookup zai-proxy.devpod.svc.cluster.local`
- Verify endpoint URL in worker settings

**Issue: Workers using wrong deployment**
- Check `ANTHROPIC_BASE_URL` in worker session
- Verify settings.json configuration
- Look for environment variable overrides

**Issue: High error rate on canary**
- Check canary deployment logs: `kubectl logs -n mcp deployment/zai-proxy-test`
- Compare metrics between production and canary
- Consider rollback to production

## 8. Reference: Service Endpoints

| Service | URL | Use Case |
|---------|-----|----------|
| Production | `http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic` | All production traffic |
| Canary | `http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic` | Testing new versions |
| Split | `http://zai-proxy-canary.devpod.svc.cluster.local:8080/api/anthropic` | Weighted traffic splitting |
| Metrics (Prod) | `http://zai-proxy.devpod.svc.cluster.local:8080/metrics` | Production metrics |
| Metrics (Canary) | `http://zai-proxy-test.devpod.svc.cluster.local:8080/metrics` | Canary metrics |
| Health (Prod) | `http://zai-proxy.devpod.svc.cluster.local:8080/health` | Production health check |
| Health (Canary) | `http://zai-proxy-test.devpod.svc.cluster.local:8080/health` | Canary health check |

## Summary

1. **Configure** workers via `settings.json` or environment variable
2. **Verify** endpoint configuration before launching workers
3. **Test** canary endpoint with isolated workers first
4. **Migrate** gradually using traffic split or full cutover
5. **Monitor** metrics for both deployments during migration
6. **Fallback** to production if issues arise

For questions or issues, check:
- Worker logs: `~/.beads-workers/*.log`
- Proxy logs: `kubectl logs -n mcp deployment/zai-proxy*`
- Metrics: Service `/metrics` endpoints
