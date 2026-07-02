# Canary to Production Promotion Procedure

This document describes the procedure to promote a canary deployment to production after successful testing.

## Overview

The zai-proxy deployment uses a dual-deployment strategy:
- **Production deployment** (`zai-proxy`): Live traffic
- **Canary deployment** (`zai-proxy-test`): Testing new versions

After a canary version has been validated, it can be promoted to production using this procedure.

## Prerequisites

Before promoting canary to production, ensure:

1. **Canary is healthy**: No alerts firing in Prometheus
2. **Testing complete**: Functional tests pass on canary endpoint
3. **Metrics validated**: Token counting, rate limiting, and error rates are acceptable
4. **Workers tested**: At least one worker has successfully used the canary endpoint
5. **Version ready**: Canary image tag is known and available

## Architecture Reference

```
                    ┌─────────────────────────────────────┐
                    │    Canary (zai-proxy-test)          │
                    │    Image: X.Y.Z-canary              │
                    └──────────────┬──────────────────────┘
                                   │ Promote
                                   ▼
                    ┌─────────────────────────────────────┐
                    │    Production (zai-proxy)           │
                    │    Image: X.Y.Z (no -canary)        │
                    └─────────────────────────────────────┘
```

## Procedure

### Step 1: Update Production Deployment Image Tag

The production deployment image tag needs to be updated to match the canary version (without `-canary` suffix if applicable).

**Example: Promoting 1.2.1-canary to 1.2.1**

```bash
# Using kubectl set image (fastest method)
kubectl set image deployment/zai-proxy \
  proxy=ronaldraygun/zai-proxy:1.2.1 \
  -n mcp
```

**Alternative: Edit deployment manifest**

```bash
# Edit the deployment directly
kubectl edit deployment/zai-proxy -n mcp

# Change: image: ronaldraygun/zai-proxy:1.2.0
# To:    image: ronaldraygun/zai-proxy:1.2.1
```

**For GitOps (ArgoCD):**

1. Update the manifest in Git:
```bash
cd /home/coding/declarative-config
```

2. Edit `zai-proxy.yml`:
```yaml
# Change from:
image: ronaldraygun/zai-proxy:1.2.0

# To:
image: ronaldraygun/zai-proxy:1.2.1
```

3. Commit and push:
```bash
git add zai-proxy.yml
git commit -m "chore: promote zai-proxy to v1.2.1"
git push origin main
```

4. ArgoCD will automatically sync the change

### Step 2: Choose Rollout Strategy

#### Option A: Rolling Update (Default)

Kubernetes performs a rolling update automatically when the image changes:

```bash
# Monitor the rollout
kubectl rollout status deployment/zai-proxy -n mcp

# Watch pods being replaced
kubectl get pods -n mcp -l app=zai-proxy,variant=production -w
```

**Characteristics:**
- Gradual replacement of pods
- Zero downtime (old pods serve traffic until new pods are ready)
- Automatic rollback if failure: `kubectl rollout undo deployment/zai-proxy -n mcp`

#### Option B: Blue-Green Switch

For an instant cutover (requires manual scaling):

```bash
# Step 1: Scale production to 0 (cut off traffic)
kubectl scale deployment/zai-proxy -n mcp --replicas=0

# Step 2: Scale canary to production replica count
kubectl scale deployment/zai-proxy -n mcp --replicas=1

# Step 3: Update production image (with no traffic)
kubectl set image deployment/zai-proxy proxy=ronaldraygun/zai-proxy:1.2.1 -n mcp

# Step 4: Scale production back up
kubectl scale deployment/zai-proxy -n mcp --replicas=1
```

**Note:** This approach is not recommended for zai-proxy as it causes brief downtime.

### Step 3: Monitor Production Metrics During Rollout

While the rollout is in progress, monitor these metrics:

**Prometheus Queries:**

```promql
# 1. Check new pods are serving traffic
sum by (variant) (rate(zai_proxy_requests_total{variant="production"}[1m]))

# 2. Verify error rate is not elevated
sum by (variant) (
  rate(zai_proxy_requests_total{variant="production",status=~"5.."}[5m])
  /
  rate(zai_proxy_requests_total{variant="production"}[5m])
)

# 3. Check latency hasn't regressed
histogram_quantile(0.95,
  sum(rate(zai_proxy_request_duration_seconds_bucket{variant="production"}[5m])) by (le)
)

# 4. Verify token counting is working
sum by (variant) (rate(zai_proxy_tokens_total{variant="production"}[5m]))
```

**Real-time monitoring:**

```bash
# Watch pod status
kubectl get pods -n mcp -l app=zai-proxy,variant=production -w

# Stream production logs
kubectl logs -f -n mcp deployment/zai-proxy --tail=100

# Check metrics endpoint directly
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | grep zai_proxy
```

### Step 4: Verify Workers Successfully Use New Version

After rollout completes, verify workers are using the new version:

**1. Check pod versions:**

```bash
# Get current version label
kubectl get pods -n mcp \
  -l app=zai-proxy,variant=production \
  -o custom-columns=NAME:.metadata.name,VERSION:.metadata.labels.version

# Expected output:
# NAME                             VERSION
# zai-proxy-7d8f9c6b5-x2k4p        1.2.1
```

**2. Verify worker connectivity:**

```bash
# Check which pods workers are connecting to
kubectl logs -n mcp deployment/zai-proxy --tail=100 | grep "Token usage"

# The logs should show activity from workers
# Expected: Token usage: input=X, output=Y
```

**3. Check worker token counting metrics:**

```bash
# Query Prometheus for token usage by workers
# This verifies workers are successfully using the new version
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | \
  grep zai_proxy_tokens_total | \
  grep 'variant="production"'
```

**4. Verify image tag in deployment:**

```bash
kubectl get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# Expected output: ronaldraygun/zai-proxy:1.2.1
```

### Step 5: Update VERSION File

Remove the `-canary` suffix from the VERSION file in the repository:

```bash
cd /home/coding/zai-proxy/proxy

# View current version
cat VERSION
# Current: 1.2.1-canary

# Update to stable version
echo "1.2.1" > VERSION

# Verify
cat VERSION
# Should show: 1.2.1
```

### Step 6: Tag Git Commit as Release

Create a git tag for the release:

```bash
cd /home/coding/zai-proxy

# Ensure all changes are committed
git status
git add VERSION
git commit -m "chore: release zai-proxy v1.2.1"

# Create annotated tag
git tag -a v1.2.1 -m "Release zai-proxy v1.2.1

- [Description of changes in this version]
- Validated via canary testing
- Promoted from canary to production"

# Push tag to remote
git push origin v1.2.1

# Optionally push all tags
git push origin --tags
```

**View release tags:**

```bash
# List all tags
git tag -l

# Show tag details
git show v1.2.1
```

## Rollout Checklist

Use this checklist during the promotion process:

### Pre-Promotion
- [ ] Canary deployment is healthy (`kubectl get pods -n mcp -l variant=test`)
- [ ] No Prometheus alerts firing for canary
- [ ] Functional tests pass on canary endpoint
- [ ] Token counting verified on canary
- [ ] At least one worker tested with canary endpoint
- [ ] Canary image tag is confirmed (e.g., `1.2.1-canary`)

### Promotion
- [ ] Production deployment image updated to new version
- [ ] Rollout initiated (`kubectl set image` or manifest update)
- [ ] Rollout status monitored (`kubectl rollout status`)

### During Rollout
- [ ] New pods are becoming Ready
- [ ] Old pods are terminating gracefully
- [ ] Error rate remains below threshold (<5%)
- [ ] P95 latency hasn't regressed significantly
- [ ] Token counting metrics are being exported

### Post-Rollout
- [ ] All production pods running new version (`kubectl describe deployment`)
- [ ] Workers are successfully making requests (check logs/metrics)
- [ ] Token counting is working (check logs for "Token usage")
- [ ] No errors in production logs
- [ ] Metrics show healthy traffic patterns

### Finalization
- [ ] VERSION file updated (removed `-canary` suffix)
- [ ] Git commit created for VERSION change
- [ ] Git tag created for release (e.g., `v1.2.1`)
- [ ] Tag pushed to remote repository
- [ ] Canary deployment can be scaled down (optional)

### Cleanup (Optional)
- [ ] Scale down canary deployment if no longer needed: `kubectl scale deployment/zai-proxy-test -n mcp --replicas=0`
- [ ] Update canary manifest to next development version

## Rollback Procedure

If issues are detected after promotion, use this rollback procedure:

### Immediate Rollback (kubectl)

```bash
# Rollback to previous version
kubectl rollout undo deployment/zai-proxy -n mcp

# Monitor rollback
kubectl rollout status deployment/zai-proxy -n mcp

# Verify rollback completed
kubectl get pods -n mcp -l app=zai-proxy,variant=production
```

### Rollback to Specific Version

```bash
# View rollout history
kubectl rollout history deployment/zai-proxy -n mcp

# Rollback to specific revision
kubectl rollout undo deployment/zai-proxy -n mcp --to-revision=2
```

### GitOps Rollback (ArgoCD)

```bash
# Revert the commit that changed the image tag
cd /home/coding/declarative-config
git revert HEAD
git push origin main

# ArgoCD will automatically sync the revert
```

## kubectl Commands Reference

### Image Management

```bash
# Update production image
kubectl set image deployment/zai-proxy proxy=ronaldraygun/zai-proxy:X.Y.Z -n mcp

# Update canary image
kubectl set image deployment/zai-proxy-test proxy=ronaldraygun/zai-proxy:X.Y.Z-canary -n mcp

# View current image
kubectl get deployment/zai-proxy -n mcp -o jsonpath='{.spec.template.spec.containers[0].image}'
```

### Rollout Management

```bash
# Check rollout status
kubectl rollout status deployment/zai-proxy -n mcp

# View rollout history
kubectl rollout history deployment/zai-proxy -n mcp

# Pause rollout
kubectl rollout pause deployment/zai-proxy -n mcp

# Resume rollout
kubectl rollout resume deployment/zai-proxy -n mcp

# Undo last rollout
kubectl rollout undo deployment/zai-proxy -n mcp

# Undo to specific revision
kubectl rollout undo deployment/zai-proxy -n mcp --to-revision=2
```

### Pod Management

```bash
# List production pods
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# List canary pods
kubectl get pods -n mcp -l app=zai-proxy,variant=test

# Watch pod changes
kubectl get pods -n mcp -l app=zai-proxy -w

# Delete specific pod (forces restart)
kubectl delete pod <pod-name> -n mcp

# Restart all pods in deployment
kubectl rollout restart deployment/zai-proxy -n mcp
```

### Scaling

```bash
# Scale production
kubectl scale deployment/zai-proxy -n mcp --replicas=1

# Scale canary
kubectl scale deployment/zai-proxy-test -n mcp --replicas=1

# View replica status
kubectl get deployment/zai-proxy -n mcp
```

### Verification

```bash
# Get deployment details
kubectl describe deployment/zai-proxy -n mcp

# View pod logs
kubectl logs -n mcp deployment/zai-proxy --tail=100
kubectl logs -f -n mcp deployment/zai-proxy

# Check pod versions
kubectl get pods -n mcp -l app=zai-proxy,variant=production \
  -o custom-columns=NAME:.metadata.name,VERSION:.metadata.labels.version

# Get pod resource usage
kubectl top pods -n mcp -l app=zai-proxy
```

### Metrics and Health

```bash
# Check service endpoints
kubectl get endpoints -n mcp | grep zai-proxy

# Test health endpoint
kubectl exec -n mcp deployment/zai-proxy -- curl -s http://localhost:8080/health

# Port-forward for local testing
kubectl port-forward -n mcp deployment/zai-proxy 8080:8080
curl http://localhost:8080/metrics
```

## Troubleshooting

### Rollout Stuck

```bash
# Check rollout status
kubectl rollout status deployment/zai-proxy -n mcp

# If stuck, pause and inspect
kubectl rollout pause deployment/zai-proxy -n mcp
kubectl describe deployment/zai-proxy -n mcp

# Resume when ready
kubectl rollout resume deployment/zai-proxy -n mcp
```

### Pods Not Ready

```bash
# Describe pod to see events
kubectl describe pod <pod-name> -n mcp

# Check pod logs
kubectl logs <pod-name> -n mcp

# Common issues:
# - Image pull errors: Check image name and registry secrets
# - Resource limits: Check pod resource requests/limits
# - Health check failures: Check /health endpoint
```

### Workers Not Connecting

```bash
# Check service is accessible
kubectl get svc -n mcp | grep zai-proxy

# Test from devpod
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/health

# Check worker logs
tail -f ~/.beads-workers/*.log
```

## Related Documentation

- [DEPLOYMENT.md](DEPLOYMENT.md) - Worker configuration and dual-deployment workflow
- [TOKEN_COUNTING.md](TOKEN_COUNTING.md) - Token counting implementation and monitoring
- [REGRESSION_TESTING.md](REGRESSION_TESTING.md) - Running regression tests before promotion
