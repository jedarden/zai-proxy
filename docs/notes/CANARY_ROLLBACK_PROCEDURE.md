# Canary Rollback Procedure

This document describes the procedure to roll back a failed canary deployment or revert a promoted canary from production.

## Overview

The zai-proxy deployment uses a dual-deployment strategy:
- **Production deployment** (`zai-proxy`): Live traffic
- **Canary deployment** (`zai-proxy-test`): Testing new versions

When a canary deployment fails or a promoted version causes issues in production, use this rollback procedure to restore service.

## Architecture Reference

```
                    ┌─────────────────────────────────────┐
                    │    Canary (zai-proxy-test)          │
                    │    Image: X.Y.Z-canary              │
                    └──────────────┬──────────────────────┘
                                   │ Fails Testing
                                   ▼
                    ┌─────────────────────────────────────┐
                    │    Production (zai-proxy)           │
                    │    Image: X.Y.Z (no -canary)        │
                    │    UNCHANGED - Still serving        │
                    └─────────────────────────────────────┘
```

## Prerequisites

Before rolling back, ensure you have:
1. **kubectl access** to the apexalgo-iad cluster
2. **kubeconfig** mounted at `/home/coder/.kube/apexalgo-iad.kubeconfig`
3. **Current deployment status** information
4. **Root cause understanding** (if available)

## Quick Rollback Commands

```bash
# Set kubeconfig for apexalgo-iad cluster
export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig

# Quick rollback to previous version
kubectl rollout undo deployment/zai-proxy -n mcp

# Monitor rollback
kubectl rollout status deployment/zai-proxy -n mcp

# If rollback fails, scale to 0 and back up
kubectl scale deployment/zai-proxy -n mcp --replicas=0
kubectl scale deployment/zai-proxy -n mcp --replicas=1
```

---

## Part 1: Canary Deployment Rollback

### Scenario: Canary Testing Reveals Critical Issues

When canary deployment fails testing, keep production unchanged and clean up canary resources.

### Rollback Triggers

**Immediately rollback if ANY of these conditions occur:**

- [ ] Error rate exceeds 10% for more than 2 minutes
- [ ] P95 latency increases by >100% for more than 2 minutes
- [ ] More than 50% of canary pods are NotReady or CrashLoopBackOff
- [ ] Token counting stops working or shows incorrect values
- [ ] Workers report high failure rates or timeouts
- [ ] Security vulnerabilities detected in canary image
- [ ] Data corruption or incorrect behavior observed

### Step 1: Verify Current State

```bash
# Set kubeconfig
export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig

# Check canary pod status
kubectl get pods -n mcp -l app=zai-proxy,variant=test

# Check production pod status (should be healthy)
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# Get canary deployment details
kubectl describe deployment/zai-proxy-test -n mcp

# Check recent canary logs
kubectl logs -n mcp deployment/zai-proxy-test --tail=100
```

### Step 2: Document Issues Found

Create an incident report or bead to track the issues:

```bash
cd /home/coding/zai-proxy

# Create bead for tracking the rollback
br create "Investigate canary deployment failure - vVERSION" \
  --type bug \
  --priority P0 \
  --description "Canary deployment VERSION failed testing with: [describe symptoms]

  Symptoms:
  - [List observed issues]

  Root Cause (if known):
  - [Describe root cause]

  Impact:
  - Production UNCHANGED and serving normally
  - Canary deployment isolated from traffic
  " \
  --labels bug,canary,rollback,urgent

# Note the bead ID for blocking future deployments
```

### Step 3: Delete Canary Resources

```bash
# Scale canary deployment to 0
kubectl scale deployment/zai-proxy-test -n mcp --replicas=0

# Verify canary is scaled down
kubectl get deployment/zai-proxy-test -n mcp

# Optionally delete canary deployment entirely (only if you're sure)
kubectl delete deployment/zai-proxy-test -n mcp
```

**Note:** Keep the canary deployment manifest but scale to 0 if you want to quickly redeploy after fixing issues.

### Step 4: Revert Code Changes (If Needed)

If the canary failure was due to code issues:

```bash
cd /home/coding/zai-proxy

# View recent commits
git log --oneline -5

# Revert the problematic commit
git revert <commit-hash>

# OR reset to previous commit (if not yet pushed)
git reset --hard HEAD~1

# Push the revert
git push origin main
```

### Step 5: Verify Production Unchanged

```bash
# Verify production is still serving
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# Check production health
kubectl exec -n mcp deployment/zai-proxy -- curl -s http://localhost:8080/health

# Verify production metrics
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_requests_total
```

### Step 6: Clean Up Canary Resources

```bash
# Verify canary is not receiving traffic
kubectl get svc -n mcp | grep zai-proxy

# If using zai-proxy-canary service, ensure workers are using zai-proxy service instead
# Check worker configuration points to production service

# Clean up failed canary image (optional)
# Only delete if you're sure you won't need to debug
docker rmi ronaldraygun/zai-proxy:VERSION-canary
```

### Step 7: Collect Diagnostic Information

```bash
# Export canary logs before deletion
kubectl logs -n mcp deployment/zai-proxy-test --tail=1000 > \
  /tmp/canary-failure-logs-$(date +%Y%m%d-%H%M%S).txt

# Export canary metrics
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics > \
  /tmp/canary-failure-metrics-$(date +%Y%m%d-%H%M%S).txt

# Export deployment state
kubectl get deployment/zai-proxy-test -n mcp -o yaml > \
  /tmp/canary-failure-deployment-$(date +%Y%m%d-%H%M%S).yaml

# Export pod events
kubectl describe pods -n mcp -l app=zai-proxy,variant=test > \
  /tmp/canary-failure-events-$(date +%Y%m%d-%H%M%S).txt
```

---

## Part 2: Production Rollback After Promotion

### Scenario: Promoted Canary Causes Production Issues

When a canary version has been promoted to production but causes issues, roll back production immediately.

### Step 1: Immediate Rollback (kubectl)

```bash
# Set kubeconfig
export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig

# QUICK ROLLBACK - Undo to previous version
kubectl rollout undo deployment/zai-proxy -n mcp

# Monitor rollback
kubectl rollout status deployment/zai-proxy -n mcp

# Watch pods being replaced
kubectl get pods -n mcp -l app=zai-proxy,variant=production -w
```

### Step 2: Rollback to Specific Version

```bash
# View rollout history
kubectl rollout history deployment/zai-proxy -n mcp

# Rollback to specific revision
kubectl rollout undo deployment/zai-proxy -n mcp --to-revision=2

# Verify the rollback
kubectl get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}' && echo
```

### Step 3: GitOps Rollback (ArgoCD)

If using GitOps with ArgoCD:

```bash
# Navigate to cluster configuration
cd /home/coder/ardenone-cluster/cluster-configuration/apexalgo-iad/mcp

# View recent commits
git log --oneline -5

# Revert the promotion commit
git revert HEAD

# Push the revert
git add zai-proxy.yml
git commit -m "fix: rollback zai-proxy to previous stable version"
git push origin main

# ArgoCD will automatically sync the revert
```

### Step 4: Emergency Rollback (If kubectl fails)

```bash
# If standard rollback fails, scale to 0 and back up
kubectl scale deployment/zai-proxy -n mcp --replicas=0
sleep 5
kubectl scale deployment/zai-proxy -n mcp --replicas=1

# Monitor recovery
kubectl get pods -n mcp -l app=zai-proxy,variant=production -w
```

### Step 5: Verify Rollback Complete

```bash
# Check all pods are running
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# Verify image version
kubectl get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# Check health endpoint
kubectl exec -n mcp deployment/zai-proxy -- curl -s http://localhost:8080/health

# Verify metrics are being exported
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_requests_total
```

### Step 6: Document the Rollback

```bash
cd /home/coding/zai-proxy

# Create incident report
br create "Production rollback after vVERSION promotion" \
  --type bug \
  --priority P0 \
  --description "Production rollback from VERSION to PREVIOUS_VERSION

  Time: $(date -u +%Y-%m-%dT%H:%M:%SZ)

  Symptoms:
  - [List observed issues in production]

  Rollback Action:
  - kubectl rollout undo deployment/zai-proxy -n mcp
  - Rolled back to revision X

  Impact:
  - Brief service interruption during rollback
  - Production now running on PREVIOUS_VERSION

  Next Steps:
  - Investigate root cause
  - Fix canary issues
  - Re-test before re-promotion
  " \
  --labels bug,production,rollback,critical
```

---

## Part 3: Troubleshooting Guide

### Common Failure Scenarios

#### Scenario 1: Canary Pods CrashLoopBackOff

**Symptoms:**
- Canary pods in CrashLoopBackOff state
- Production pods healthy
- Can't access canary logs (RBAC blocked)

**Rollback Procedure:**

```bash
# 1. Verify production is healthy
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# 2. Scale down canary
kubectl scale deployment/zai-proxy-test -n mcp --replicas=0

# 3. Check canary deployment image
kubectl get deployment/zai-proxy-test -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# 4. Verify image exists on Docker Hub
curl -s "https://registry.hub.docker.com/v2/repositories/ronaldraygun/zai-proxy/tags/" | \
  jq '.results[] | select(.name == "VERSION-canary")'

# 5. If image issue, rebuild and redeploy
# See: /home/coding/zai-proxy/docs/DEPLOYMENT.md
```

**Prevention:**
- Always test images locally before pushing
- Validate image exists on Docker Hub before deployment
- Check image pull secrets are configured

#### Scenario 2: Canary High Error Rate

**Symptoms:**
- Canary pods running but returning 5xx errors
- Prometheus alert `ZaiProxyCanaryHighErrorRate` firing
- Error rate > 5%

**Rollback Procedure:**

```bash
# 1. Check error rate in Prometheus
# Query: sum(rate(zai_proxy_requests_total{variant="test",status_code=~"5.."}[5m])) / sum(rate(zai_proxy_requests_total{variant="test"}[5m]))

# 2. Check canary logs for errors
kubectl logs -n mcp deployment/zai-proxy-test --tail=100 | grep -i error

# 3. If critical, scale down canary immediately
kubectl scale deployment/zai-proxy-test -n mcp --replicas=0

# 4. Check if production can handle the load
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# 5. Document the issue
cd /home/coding/zai-proxy
br create "Fix canary high error rate - vVERSION" \
  --type bug --priority P1 --labels bug,canary,errors
```

**Prevention:**
- Run regression tests before deployment
- Monitor canary metrics continuously
- Set up alerts for error rates

#### Scenario 3: Canary Latency Degraded

**Symptoms:**
- Canary p90/p95 latency > 1.5x production
- Prometheus alert `ZaiProxyCanaryLatencyDegraded` firing
- Slow response times on canary endpoint

**Rollback Procedure:**

```bash
# 1. Check latency in Prometheus
# Query: histogram_quantile(0.90, sum(rate(zai_proxy_request_duration_seconds_bucket{variant="test"}[5m])) by (le))

# 2. Check token counting overhead
curl -s http://zai-proxy-test.mcp.svc.cluster.local:8080/metrics | \
  grep zai_proxy_token_count_duration_seconds

# 3. If token counting is slow (>100ms p99), disable it temporarily
kubectl set env deployment/zai-proxy-test -n mcp \
  ENABLE_TOKEN_COUNTING=false

# 4. Restart canary to pick up new config
kubectl rollout restart deployment/zai-proxy-test -n mcp

# 5. Monitor recovery
kubectl rollout status deployment/zai-proxy-test -n mcp
```

**Prevention:**
- Profile token counting performance
- Set appropriate timeouts
- Use caching for token counting results

#### Scenario 4: Production Rollout Stuck

**Symptoms:**
- Production rollout not progressing
- New pods not becoming Ready
- Old pods still serving traffic

**Rollback Procedure:**

```bash
# 1. Check rollout status
kubectl rollout status deployment/zai-proxy -n mcp

# 2. If stuck, pause rollout
kubectl rollout pause deployment/zai-proxy -n mcp

# 3. Describe deployment to see issues
kubectl describe deployment/zai-proxy -n mcp

# 4. Describe failing pods
kubectl describe pod <pod-name> -n mcp

# 5. If critical, rollback immediately
kubectl rollout undo deployment/zai-proxy -n mcp

# 6. Monitor rollback
kubectl rollout status deployment/zai-proxy -n mcp
```

**Prevention:**
- Use rolling update strategy with appropriate thresholds
- Set resource limits appropriately
- Monitor pod health during rollout

#### Scenario 5: Production Image Crash Loop

**Symptoms:**
- Production pods entering CrashLoopBackOff
- Recent image promotion caused crashes
- Service disruption

**Emergency Rollback:**

```bash
# 1. IMMEDIATE ROLLBACK - Use kubectl
kubectl rollout undo deployment/zai-proxy -n mcp

# 2. If undo fails, scale to 0 and back up
kubectl scale deployment/zai-proxy -n mcp --replicas=0
sleep 5
kubectl scale deployment/zai-proxy -n mcp --replicas=1

# 3. Verify pods are coming up
kubectl get pods -n mcp -l app=zai-proxy,variant=production -w

# 4. Check ReplicaSets to find working version
kubectl get replicasets -n mcp -l app=zai-proxy,variant=production

# 5. Patch deployment to use working version
kubectl patch deployment zai-proxy -n mcp \
  -p '{"spec":{"template":{"metadata":{"labels":{"version":"WORKING_VERSION"}}}}}'

# 6. Set image to working version
kubectl set image deployment/zai-proxy -n mcp \
  proxy=ronaldraygun/zai-proxy:WORKING_VERSION
```

**Prevention:**
- Always test canary thoroughly before promotion
- Use proper health checks
- Monitor crash counts

#### Scenario 6: ArgoCD Sync Delay

**Symptoms:**
- Git revert pushed but ArgoCD not syncing
- Production still running failed version
- Manual intervention needed

**Rollback Procedure:**

```bash
# 1. Force immediate rollback via kubectl (bypass ArgoCD)
kubectl rollout undo deployment/zai-proxy -n mcp

# 2. Check ArgoCD sync status
# In ArgoCD UI: https://argocd.<domain>/application/zai-proxy

# 3. If sync stuck, manually sync in ArgoCD UI
# Or use argocd CLI:
argocd app sync zai-proxy

# 4. Verify sync completed
argocd app get zai-proxy

# 5. Monitor rollout
kubectl rollout status deployment/zai-proxy -n mcp

# 6. Once stable, ArgoCD will reconcile with Git
# The kubectl change may be overwritten, so update Git to match:
cd /home/coder/ardenone-cluster/cluster-configuration/apexalgo-iad/mcp
# Edit zai-proxy.yml to match the rolled-back version
git add zai-proxy.yml
git commit -m "fix: sync git with rolled-back version"
git push origin main
```

**Prevention:**
- Monitor ArgoCD sync status
- Use ArgoCD sync waves if needed
- Have manual rollback ready as backup

#### Scenario 7: Workers Not Connecting After Rollback

**Symptoms:**
- Rollback completed but workers not connecting
- Worker logs showing connection errors
- No metrics from production

**Rollback Procedure:**

```bash
# 1. Check service endpoints
kubectl get endpoints -n mcp | grep zai-proxy

# 2. Test service from devpod
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/health

# 3. Check worker configuration
grep -r "zai-proxy" ~/.beads-workers/*.log

# 4. If workers pointing to canary service, update them
# Workers should use: http://zai-proxy.mcp.svc.cluster.local:8080
# NOT: http://zai-proxy-canary.mcp.svc.cluster.local:8080

# 5. Restart affected workers
# Find worker session
tlist

# Kill and restart worker
tkill <session-name>

# 6. Verify worker connectivity
tail -f ~/.beads-workers/<session-name>.log
```

**Prevention:**
- Use service discovery correctly
- Document worker configuration
- Test worker connectivity after changes

---

## Part 4: Rollback Verification Checklist

Use this checklist after performing any rollback:

### Canary Rollback Verification

- [ ] Canaries scaled to 0 (kubectl scale)
- [ ] Production pods still healthy
- [ ] Production serving traffic normally
- [ ] No Prometheus alerts for production
- [ ] Incident report/bead created
- [ ] Code changes reverted (if needed)
- [ ] Root cause documented
- [ ] Fix plan created

### Production Rollback Verification

- [ ] Rollback command executed
- [ ] Rollout status shows completion
- [ ] All production pods Ready
- [ ] Pods running previous image version
- [ ] Health endpoint responding
- [ ] Workers connecting successfully
- [ ] Metrics being exported
- [ ] Error rate below threshold
- [ ] Latency back to baseline
- [ ] Incident report/bead created
- [ ] Git revert pushed (if GitOps)
- [ ] ArgoCD synced (if applicable)

---

## Part 5: Rollback Dry-Run Test

### Testing Rollback Procedure

To verify rollback procedures work, perform a dry-run test:

```bash
# Set kubeconfig
export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig

# 1. Save current state
kubectl get deployment/zai-proxy -n mcp -o yaml > /tmp/zai-proxy-before.yml
kubectl get deployment/zai-proxy-test -n mcp -o yaml > /tmp/zai-proxy-test-before.yml

# 2. Check current image
current_image=$(kubectl get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}')
echo "Current image: $current_image"

# 3. Check rollout history
kubectl rollout history deployment/zai-proxy -n mcp

# 4. Test rollback command (dry-run)
kubectl rollout undo deployment/zai-proxy -n mcp --dry-run=server

# 5. Test scaling to 0 (don't actually do it)
kubectl scale deployment/zai-proxy-test -n mcp --replicas=0 --dry-run=server

# 6. Verify you can access logs
kubectl logs -n mcp deployment/zai-proxy --tail=10

# 7. Verify you can access metrics
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | head -20

# 8. Check service endpoints
kubectl get endpoints -n mcp zai-proxy

# 9. Restore state (if needed)
# kubectl apply -f /tmp/zai-proxy-before.yml
```

### Automated Rollback Test Script

```bash
#!/bin/bash
# Test rollback procedures

set -e

NAMESPACE="mcp"
PRODUCTION_DEPLOYMENT="zai-proxy"
CANARY_DEPLOYMENT="zai-proxy-test"

echo "=== Testing Canary Rollback Procedure ==="

# Test 1: Can we scale canary to 0?
echo "Test 1: Scale canary to 0"
kubectl scale deployment/$CANARY_DEPLOYMENT -n $NAMESPACE --replicas=0 --dry-run=server

# Test 2: Can we undo production rollout?
echo "Test 2: Undo production rollout"
kubectl rollout undo deployment/$PRODUCTION_DEPLOYMENT -n $NAMESPACE --dry-run=server

# Test 3: Can we get rollout history?
echo "Test 3: Get rollout history"
kubectl rollout history deployment/$PRODUCTION_DEPLOYMENT -n $NAMESPACE

# Test 4: Can we check pod status?
echo "Test 4: Check pod status"
kubectl get pods -n $NAMESPACE -l app=zai-proxy,variant=production

# Test 5: Can we access logs?
echo "Test 5: Access logs"
kubectl logs -n $NAMESPACE deployment/$PRODUCTION_DEPLOYMENT --tail=10

# Test 6: Can we access metrics?
echo "Test 6: Access metrics"
curl -s http://zai-proxy.$NAMESPACE.svc.cluster.local:8080/metrics | head -5

# Test 7: Can we check health?
echo "Test 7: Check health"
kubectl exec -n $NAMESPACE deployment/$PRODUCTION_DEPLOYMENT -- \
  curl -s http://localhost:8080/health

echo "=== All rollback tests passed ==="
```

---

## Part 6: Post-Rollback Actions

### After Rolling Back Canary

1. **Fix the issues:**
   - Investigate root cause
   - Fix code or configuration
   - Add regression tests

2. **Re-test canary:**
   - Deploy fixed version to canary
   - Run functional tests
   - Monitor metrics
   - Validate with worker traffic

3. **Re-promote when ready:**
   - Follow promotion procedure
   - Monitor production metrics
   - Have rollback plan ready

### After Rolling Back Production

1. **Stabilize service:**
   - Verify production is healthy
   - Monitor metrics continuously
   - Check worker connectivity

2. **Investigate failure:**
   - Analyze logs from failed version
   - Identify root cause
   - Document findings

3. **Fix and re-test:**
   - Fix issues in canary
   - Thoroughly test fixes
   - Consider extended canary testing

4. **Re-promote carefully:**
   - Use smaller traffic split initially
   - Monitor continuously
   - Have rollback command ready

---

## Part 7: kubectl Rollback Commands Reference

### Deployment Rollback

```bash
# Undo to previous version
kubectl rollout undo deployment/<name> -n <namespace>

# Undo to specific revision
kubectl rollout undo deployment/<name> -n <namespace> --to-revision=<n>

# View rollout history
kubectl rollout history deployment/<name> -n <namespace>

# Check rollout status
kubectl rollout status deployment/<name> -n <namespace>

# Pause rollout
kubectl rollout pause deployment/<name> -n <namespace>

# Resume rollout
kubectl rollout resume deployment/<name> -n <namespace>

# Restart deployment
kubectl rollout restart deployment/<name> -n <namespace>
```

### Scaling Operations

```bash
# Scale deployment to 0
kubectl scale deployment/<name> -n <namespace> --replicas=0

# Scale deployment up
kubectl scale deployment/<name> -n <namespace> --replicas=<n>

# Scale multiple deployments
kubectl scale deployment/<name1> deployment/<name2> -n <namespace> --replicas=0
```

### Image Management

```bash
# Set image
kubectl set image deployment/<name> -n <namespace> \
  <container>=<image>:<tag>

# Get current image
kubectl get deployment/<name> -n <namespace> \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# Patch deployment with new image
kubectl patch deployment/<name> -n <namespace> \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"<container>","image":"<image>:<tag>"}]}}}}'
```

### Verification Commands

```bash
# Get pods
kubectl get pods -n <namespace> -l app=<app>

# Watch pod changes
kubectl get pods -n <namespace> -l app=<app> -w

# Describe deployment
kubectl describe deployment/<name> -n <namespace>

# Describe pod
kubectl describe pod/<pod-name> -n <namespace>

# View logs
kubectl logs -n <namespace> deployment/<name> --tail=100

# Stream logs
kubectl logs -f -n <namespace> deployment/<name>

# Get endpoints
kubectl get endpoints -n <namespace> | grep <service>

# Test health endpoint
kubectl exec -n <namespace> deployment/<name> -- \
  curl -s http://localhost:8080/health
```

---

## Part 8: Rollback Decision Flowchart

```
                    ┌─────────────────────┐
                    │  Canary Testing     │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │  Issues Detected?   │
                    └──────────┬──────────┘
                               │
              ┌────────────────┴────────────────┐
              │ No                              │ Yes
              ▼                                 ▼
    ┌───────────────────┐          ┌─────────────────────┐
    │ Continue Testing  │          │ Critical Issue?     │
    └───────────────────┘          └──────────┬──────────┘
                                             │
                                   ┌─────────┴─────────┐
                                   │ Yes                │ No
                                   ▼                    ▼
                    ┌───────────────────┐  ┌───────────────────┐
                    │ Immediate Rollback│  │ Document & Monitor│
                    └─────────┬─────────┘  └───────────────────┘
                              │
              ┌───────────────┴───────────────┐
              │                               │
              ▼                               ▼
    ┌───────────────────┐          ┌───────────────────┐
    │ Scale Canary to 0 │          │ Collect Diagnostics│
    └─────────┬─────────┘          └─────────┬─────────┘
              │                               │
              ▼                               ▼
    ┌───────────────────┐          ┌───────────────────┐
    │ Delete Canary     │◄─────────│ Create Failure    │
    │ Resources         │          │ Report            │
    └─────────┬─────────┘          └───────────────────┘
              │
              ▼
    ┌───────────────────┐
    │ Verify Production │
    │ Still Healthy     │
    └─────────┬─────────┘
              │
              ▼
    ┌───────────────────┐
    │ Document Lessons  │
    │ Learned           │
    └───────────────────┘
```

---

## Part 9: RBAC Considerations

### Important: Read-Only Access from Devpods

When running rollback procedures from devpods using the `devpod-observer` ServiceAccount:

**Available Operations (Read-Only):**
- `kubectl get pods` - View pod status
- `kubectl get deployments` - View deployment status
- `kubectl get svc` - View service status
- `kubectl rollout history` - View rollout history
- `kubectl logs` - View pod logs
- `kubectl describe` - View resource details

**NOT Available (Requires Write Permissions):**
- `kubectl scale` - Cannot scale deployments
- `kubectl rollout undo` - Cannot rollback deployments
- `kubectl delete` - Cannot delete resources
- `kubectl set image` - Cannot update images
- `kubectl patch` - Cannot patch resources
- `kubectl exec` - Cannot execute commands in pods

### Rollback with Read-Only Access

When you only have read-only access (e.g., from devpods), use these alternative approaches:

**Option 1: GitOps Rollback (Recommended for ArgoCD-managed deployments)**

```bash
# Navigate to cluster configuration
cd /home/coder/ardenone-cluster/cluster-configuration/apexalgo-iad/mcp

# Revert the problematic commit
git log --oneline -5
git revert HEAD

# Push the revert
git add zai-proxy.yml
git commit -m "fix: rollback zai-proxy to previous stable version"
git push origin main

# ArgoCD will automatically sync the revert
```

**Option 2: Request Rollback via Human Intervention**

```bash
# Create a bead to request rollback
cd /home/coding/zai-proxy

br create "URGENT: Request production rollback for zai-proxy" \
  --type bug \
  --priority P0 \
  --description "CRITICAL: Production rollback requested

  Current Issues:
  - [Describe symptoms]

  Requested Action:
  - kubectl rollout undo deployment/zai-proxy -n mcp
  - OR: Scale to 0 and back up

  Verified via Read-Only:
  - Production pods: $(kubectl get pods -n mcp -l app=zai-proxy,variant=production)
  - Current image: $(kubectl get deployment/zai-proxy -n mcp -o jsonpath='{.spec.template.spec.containers[0].image}')
  - Rollout history available
  " \
  --labels critical,rollback,production,human-required
```

**Option 3: Direct Cluster Access (If Available)**

If you have direct kubectl access with admin permissions (not via devpod-observer):

```bash
# Use local kubeconfig or admin credentials
kubectl rollout undo deployment/zai-proxy -n mcp
```

### Verification with Read-Only Access

You CAN verify the cluster state even with read-only access:

```bash
# Check deployment status
kubectl get deployment/zai-proxy -n mcp

# Check pod status
kubectl get pods -n mcp -l app=zai-proxy,variant=production

# View recent logs
kubectl logs -n mcp deployment/zai-proxy --tail=50

# Check rollout history
kubectl rollout history deployment/zai-proxy -n mcp

# View service endpoints
kubectl get endpoints -n mcp zai-proxy
```

---

## Related Documentation

- [CANARY_PROMOTION_PROCEDURE.md](CANARY_PROMOTION_PROCEDURE.md) - Promoting canary to production
- [CANARY_PROMOTION_CHECKLIST.md](CANARY_PROMOTION_CHECKLIST.md) - Promotion checklist
- [DEPLOYMENT.md](DEPLOYMENT.md) - Worker configuration and dual-deployment workflow
- [TOKEN_COUNTING.md](TOKEN_COUNTING.md) - Token counting implementation
- [REGRESSION_TESTING.md](REGRESSION_TESTING.md) - Running regression tests
- [README-traffic-splitting.md](../../cluster-configuration/apexalgo-iad/mcp/README-traffic-splitting.md) - Traffic splitting options

---

## Recovery Timeline

| Action | Time | Notes |
|--------|------|-------|
| Scale canary to 0 | <10s | Immediate stop |
| Delete canary resources | <30s | Full cleanup |
| Verify production healthy | <1min | Confirm no impact |
| Production rollback | <2min | Full rollout undo |
| Collect diagnostics | <5min | For analysis |
| Document failure | <10min | Postmortem |
| **Canary rollback time** | **<15min** | Production unaffected |
| **Production rollback time** | **<5min** | Brief interruption |

**Key Point:** Production is never modified during canary rollback, so downtime is zero.

---

**Document Version:** 2.1.0
**Last Updated:** 2026-02-08
**Maintained By:** Claude Code Workers
**Related Bead:** bd-2s5

---

## Important RBAC Note

**When accessing from devpods via kubectl-proxy:** The `devpod-observer` ServiceAccount has **limited permissions** and cannot perform write operations like `kubectl rollout undo` or `kubectl scale`.

**For devpod access, use GitOps rollback (Option 1) instead of direct kubectl commands.**

**Direct kubectl rollback commands work when:**
- Running from within the apexalgo-iad cluster directly
- Using a ServiceAccount with deployment edit permissions
- The deployment is managed by ArgoCD (use Git revert instead)
