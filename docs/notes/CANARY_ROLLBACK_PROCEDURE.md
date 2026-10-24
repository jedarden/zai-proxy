# Canary Rollback Procedure

This document describes the procedure to roll back a failed canary deployment or revert a promoted canary from production.

## Overview

The zai-proxy deployment uses a dual-deployment strategy:
- **Production deployment** (`zai-proxy`): Live traffic
- **Canary deployment** (`zai-proxy-test`): Testing new versions

When a canary deployment fails or a promoted version causes issues in production, use this rollback procedure to restore service.

**Both deployments are ArgoCD-managed.** Every rollback is a Git operation in
`jedarden/declarative-config` (`k8s/apexalgo-iad/mcp/`): commit to `main`,
push to origin, let ArgoCD sync (app `mcp-ns-apexalgo-iad`, automated sync
with `selfHeal` + `prune`). `kubectl rollout undo`, `scale`, `set image`,
`patch`, and `delete` are prohibited org-wide, mechanically blocked by the
org-rule-guard hook, denied to the read-only identity, and `selfHeal` would
revert them anyway — **including `--dry-run=server` variants**. A rollback
that tries to go through kubectl fails at exactly the moment it is needed.

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

  Both boxes are declared by jedarden/declarative-config
  (k8s/apexalgo-iad/mcp/) and applied by ArgoCD app mcp-ns-apexalgo-iad.
```

## Prerequisites

Before rolling back, ensure you have:
1. **Read-only cluster access** — the credential-free proxy endpoint
   (`kubectl --server=http://traefik-apexalgo-iad:8001`) needs no kubeconfig;
   it supports `get`/`describe`/`logs`/`rollout status`/`rollout history`
   (not `exec`, not `port-forward`, not Secrets reads)
2. **A clean declarative-config checkout** at `/home/coding/declarative-config`
   on `main` (all rollbacks are commits here)
3. **Current deployment status** information
4. **Root cause understanding** (if available)

## Quick Rollback Path

```bash
# Production back to previous version: revert the promotion commit
cd /home/coding/declarative-config
git log --oneline -5                     # identify the promotion commit
git revert <promotion-commit-sha>
git push origin main

# Canary stand-down: set replicas: 0 in its manifest (or git rm it)
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml   # replicas: 0
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "chore: stand down zai-proxy canary"
git push origin main

# Watch either change land (read-only)
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp
```

---

## Part 1: Canary Deployment Rollback

### Scenario: Canary Testing Reveals Critical Issues

When canary deployment fails testing, keep production unchanged and clean up canary resources.

(The canary manifest is checked in as of 2026-10-23 under
`k8s/apexalgo-iad/mcp/zai-proxy-test.yml` — a rollback stands it down there
via `replicas: 0`, or `git revert`s the commit that bumped the canary image.
If the canary you are rolling back was created some other way, first bring
it under declarative-config, then stand it down there.)

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
# Read-only, no kubeconfig needed
S=http://traefik-apexalgo-iad:8001

# Check canary pod status
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=test

# Check production pod status (should be healthy)
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=production

# Get canary deployment details
kubectl --server=$S describe deployment/zai-proxy-test -n mcp

# Check recent canary logs
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=100
```

### Step 2: Document Issues Found

Create a bead to track the issues (`bead` is the canonical CLI; the old
`br create` is retired). Beads for zai-proxy live in this repository's bead
store:

```bash
cd /home/coding/zai-proxy

# Create bead for tracking the rollback
bead create --issue-type bug --priority 0 \
  --label canary --label rollback \
  --title "Investigate canary deployment failure - vVERSION"

bead update <bead-id> --notes "Canary deployment VERSION failed testing with: [describe symptoms]

  Symptoms:
  - [List observed issues]

  Root Cause (if known):
  - [Describe root cause]

  Impact:
  - Production UNCHANGED and serving normally
  - Canary deployment isolated from traffic"

# Note the bead ID for blocking future deployments
```

### Step 3: Stand Down Canary Resources (via its manifest)

The canary, like production, is declared in declarative-config. Scale it down
there — never with `kubectl scale`:

```bash
cd /home/coding/declarative-config

# Option A: keep the manifest, zero the replicas (fast to redeploy later)
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml   # replicas: 0

# Option B: remove the canary entirely (ArgoCD prune removes it from the cluster)
git rm k8s/apexalgo-iad/mcp/zai-proxy-test.yml

git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "chore: stand down zai-proxy canary after failed vVERSION test"
git push origin main

# Verify canary is stood down
kubectl --server=http://traefik-apexalgo-iad:8001 get deployment/zai-proxy-test -n mcp
```

**Note:** Option A keeps the canary manifest for a quick redeploy after
fixing issues; Option B is a full removal — only if you're sure.

### Step 4: Revert Code Changes (If Needed)

If the canary failure was due to code issues:

```bash
cd /home/coding/zai-proxy

# View recent commits
git log --oneline -5

# Revert the problematic commit (never force-push, never reset a pushed branch)
git revert <commit-hash>

# Push the revert
git push origin main
```

### Step 5: Verify Production Unchanged

```bash
# Verify production is still serving
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production

# Check production health and metrics over the tailnet (-k: self-signed VPN cert)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | grep zai_proxy_requests_total
```

### Step 6: Clean Up Canary Resources

```bash
# Verify canary is not receiving traffic
kubectl --server=http://traefik-apexalgo-iad:8001 get svc -n mcp | grep zai-proxy

# If a canary service exists, make sure workers use the production service.
# Workers get their endpoint from ANTHROPIC_BASE_URL in needle adapter configs:
grep -n "ANTHROPIC_BASE_URL" ~/.config/needle/adapters/*.yaml | head
# Workers should use: https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444
# NOT any canary-specific URL.

# Clean up failed canary image (optional, local; only if you won't need to debug)
docker rmi ronaldraygun/zai-proxy:VERSION-canary
```

### Step 7: Collect Diagnostic Information

```bash
S=http://traefik-apexalgo-iad:8001

# Export canary logs before stand-down completes
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=1000 > \
  /tmp/canary-failure-logs-$(date +%Y%m%d-%H%M%S).txt

# Export deployment state
kubectl --server=$S get deployment/zai-proxy-test -n mcp -o yaml > \
  /tmp/canary-failure-deployment-$(date +%Y%m%d-%H%M%S).yaml

# Export pod events
kubectl --server=$S describe pods -n mcp -l app=zai-proxy,variant=test > \
  /tmp/canary-failure-events-$(date +%Y%m%d-%H%M%S).txt

# Canary /metrics snapshot (no exec available — the pod's own logs carry them,
# or fetch via an IngressRoute if one was declared for the canary):
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=500 | grep zai_proxy_ > \
  /tmp/canary-failure-metrics-$(date +%Y%m%d-%H%M%S).txt
```

---

## Part 2: Production Rollback After Promotion

### Scenario: Promoted Canary Causes Production Issues

When a canary version has been promoted to production but causes issues, roll back production immediately — through Git.

### Step 1: Rollback (GitOps — the only path)

```bash
# 1. Revert the promotion commit
cd /home/coding/declarative-config
git log --oneline -5                     # identify the promotion commit
git revert <promotion-commit-sha>
git push origin main

# 2. If the sync lags, force it FROM the repo (applies Git; does not bypass it)
argocd app sync mcp-ns-apexalgo-iad      # or the ArgoCD UI Sync button

# 3. Monitor the rollback (read-only)
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production -w
```

`kubectl rollout undo` is not the fast path here — it is blocked, and even if
it weren't, `selfHeal` would fight it. The revert commit is both the fastest
sanctioned action and the one that sticks.

### Step 2: Rollback to a Specific Version

Pin the manifest to the exact known-good tag and push:

```bash
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy.yml
# image: ronaldraygun/zai-proxy:KNOWN_GOOD   AND the pod-template version: label

git add k8s/apexalgo-iad/mcp/zai-proxy.yml
git commit -m "fix: roll zai-proxy back to vKNOWN_GOOD"
git push origin main

# Verify the rollback
kubectl --server=http://traefik-apexalgo-iad:8001 get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}' && echo

# For reference, what revisions ran before:
kubectl --server=http://traefik-apexalgo-iad:8001 rollout history deployment/zai-proxy -n mcp
```

### Step 3: Verify Rollback Complete

```bash
S=http://traefik-apexalgo-iad:8001

# Check all pods are running
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=production

# Verify image version
kubectl --server=$S get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# Check health endpoint (tailnet URL workers use)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health

# Verify metrics are being exported
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | grep zai_proxy_requests_total

# ArgoCD back in sync
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd
```

### Step 4: Document the Rollback

```bash
cd /home/coding/zai-proxy

# Create incident bead
bead create --issue-type bug --priority 0 \
  --label production --label rollback --label critical \
  --title "Production rollback after vVERSION promotion"

bead update <bead-id> --notes "Production rollback from VERSION to PREVIOUS_VERSION

  Time: $(date -u +%Y-%m-%dT%H:%M:%SZ)

  Symptoms:
  - [List observed issues in production]

  Rollback Action:
  - git revert <promotion-commit-sha> in jedarden/declarative-config
  - ArgoCD app mcp-ns-apexalgo-iad re-synced

  Impact:
  - Brief service interruption during rollback
  - Production now running on PREVIOUS_VERSION

  Next Steps:
  - Investigate root cause
  - Fix canary issues
  - Re-test before re-promotion"
```

---

## Part 3: Troubleshooting Guide

### Common Failure Scenarios

#### Scenario 1: Canary Pods CrashLoopBackOff

**Symptoms:**
- Canary pods in CrashLoopBackOff state
- Production pods healthy
- Canary logs available via `kubectl logs` (read-only proxy)

**Rollback Procedure:**

```bash
S=http://traefik-apexalgo-iad:8001

# 1. Verify production is healthy
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=production

# 2. Stand down canary via its manifest (see Part 1 Step 3)
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml   # replicas: 0
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml && git commit -m "chore: stand down zai-proxy canary" && git push origin main

# 3. Check canary deployment image (read-only)
kubectl --server=$S get deployment/zai-proxy-test -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# 4. Verify image exists on Docker Hub
curl -s "https://registry.hub.docker.com/v2/repositories/ronaldraygun/zai-proxy/tags/" | \
  jq '.results[] | select(.name == "VERSION-canary")'

# 5. If image issue, rebuild and redeploy via declarative-config
# See: /home/coding/zai-proxy/docs/notes/DEPLOYMENT.md
```

**Prevention:**
- Always test images locally before pushing
- Validate image exists on Docker Hub before deployment
- Check image pull secrets are configured (in declarative-config)

#### Scenario 2: Canary High Error Rate

**Symptoms:**
- Canary pods running but returning 5xx errors
- Prometheus alert `ZaiProxyCanaryHighErrorRate` firing
- Error rate > 5%

**Rollback Procedure:**

```bash
S=http://traefik-apexalgo-iad:8001

# 1. Check error rate (canary's own metrics; status codes on status_code label)
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=200 | grep -i error

# 2. If critical, stand down canary immediately via its manifest (Scenario 1 step 2)

# 3. Check if production can handle the load
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=production

# 4. Document the issue
cd /home/coding/zai-proxy
bead create --issue-type bug --priority 1 --label canary --label errors \
  --title "Fix canary high error rate - vVERSION"
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
S=http://traefik-apexalgo-iad:8001

# 1. Check token counting overhead from the canary's logs
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=200 | \
  grep -i "token"

# 2. If token counting is slow (>100ms p99), disable it temporarily —
#    by editing the canary manifest, never kubectl set env:
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml
#   env:
#   - name: TOKEN_COUNTING_ENABLED
#     value: "false"
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "chore: disable token counting on zai-proxy canary (latency)"
git push origin main
# ArgoCD applies it; the pod rolls automatically. No rollout restart needed.
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
S=http://traefik-apexalgo-iad:8001

# 1. Check rollout status (read-only)
kubectl --server=$S rollout status deployment/zai-proxy -n mcp

# 2. Describe deployment to see issues
kubectl --server=$S describe deployment/zai-proxy -n mcp

# 3. Describe failing pods
kubectl --server=$S describe pod <pod-name> -n mcp

# 4. If critical, roll back through Git (Part 2 Step 1): git revert + push

# 5. Monitor rollback
kubectl --server=$S rollout status deployment/zai-proxy -n mcp
```

There is deliberately no `rollout pause` lever: the declared strategy in the
manifest governs the rollout, and the fix for a bad rollout is a reverted
commit.

**Prevention:**
- Use rolling update strategy with appropriate thresholds (declared in the manifest)
- Set resource limits appropriately
- Monitor pod health during rollout

#### Scenario 5: Production Image Crash Loop

**Symptoms:**
- Production pods entering CrashLoopBackOff
- Recent image promotion caused crashes
- Service disruption

**Emergency Rollback:**

```bash
S=http://traefik-apexalgo-iad:8001

# 1. IMMEDIATE ROLLBACK — revert the promotion commit
cd /home/coding/declarative-config
git revert <promotion-commit-sha>
git push origin main
# If the sync lags: argocd app sync mcp-ns-apexalgo-iad

# 2. Verify pods are coming up
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=production -w

# 3. Check ReplicaSets to find the last working revision (read-only)
kubectl --server=$S get replicasets -n mcp -l app=zai-proxy

# 4. If a specific older version is needed, pin the manifest to it (Part 2 Step 2)
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

**Procedure:**

```bash
# 1. Check ArgoCD sync status (read-only, ArgoCD lives on ardenone-manager)
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd

# 2. Force a sync FROM the repo — this applies Git, it does not bypass it:
argocd app sync mcp-ns-apexalgo-iad
#    (or the Sync button in the ArgoCD UI)

# 3. Confirm the app is where the repo is
argocd app get mcp-ns-apexalgo-iad

# 4. Monitor rollout
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp
```

**Do not "bridge" with kubectl.** A live mutation (e.g. `kubectl rollout
undo`) would be reverted by `selfHeal` and leaves Git and the cluster
disagreeing — the exact state this procedure exists to prevent. The revert
commit in Git is already correct; the only task is getting ArgoCD to apply
it, which is what `app sync` does.

If the push itself hasn't landed: confirm the Forgejo push succeeded and the
GitHub mirror (which the Application tracks, `repoURL:
https://github.com/jedarden/declarative-config`) is current — the mirror
syncs on every commit.

**Prevention:**
- Monitor ArgoCD sync status
- Use ArgoCD sync waves if needed
- Keep the GitOps path oiled (clean checkout, push access verified)

#### Scenario 7: Workers Not Connecting After Rollback

**Symptoms:**
- Rollback completed but workers not connecting
- Worker sessions showing connection errors
- No metrics from production

**Procedure:**

```bash
S=http://traefik-apexalgo-iad:8001

# 1. Check service endpoints
kubectl --server=$S get endpoints -n mcp | grep zai-proxy

# 2. Test the endpoint workers use (tailnet URL, -k for the VPN cert)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health

# 3. Check worker configuration — the endpoint comes from the needle adapters
grep -n "ANTHROPIC_BASE_URL" ~/.config/needle/adapters/*.yaml | head

# 4. If any adapter points at a canary URL, restore it to production:
#    https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444

# 5. Restart the affected worker (workers are tmux sessions; bare NATO-named
#    sessions are the operator's — only touch needle-* fleet sessions)
tmux ls | grep needle-
tmux kill-session -t <needle-worker-session>   # the fleet relaunches workers

# 6. Verify worker connectivity by watching its output
tmux capture-pane -p -t <needle-worker-session> | tail -50
```

**Prevention:**
- Use service discovery correctly
- Document worker configuration
- Test worker connectivity after changes

---

## Part 4: Rollback Verification Checklist

Use this checklist after performing any rollback:

### Canary Rollback Verification

- [ ] Canary stood down (replicas 0 in manifest, or manifest removed)
- [ ] Production pods still healthy
- [ ] Production serving traffic normally
- [ ] No Prometheus alerts for production
- [ ] Incident bead created
- [ ] Code changes reverted (if needed)
- [ ] Root cause documented
- [ ] Fix plan created

### Production Rollback Verification

- [ ] Revert commit pushed to declarative-config `main`
- [ ] ArgoCD app `mcp-ns-apexalgo-iad` shows `Synced`
- [ ] Rollout status shows completion
- [ ] All production pods Ready
- [ ] Pods running previous image version
- [ ] Health endpoint responding (tailnet URL)
- [ ] Workers connecting successfully
- [ ] Metrics being exported
- [ ] Error rate below threshold
- [ ] Latency back to baseline
- [ ] Incident bead created
- [ ] ArgoCD synced

---

## Part 5: Rollback Dry-Run Test

### Testing the Rollback Procedure

A dry-run here means: prove you can *observe* the cluster and *move* Git —
not `kubectl scale --dry-run=server` (still a blocked mutation, still
RBAC-denied). Verify the two halves of the rollback path:

```bash
# --- Read half: everything you'll need mid-incident (read-only) ---
S=http://traefik-apexalgo-iad:8001

kubectl --server=$S get deployment/zai-proxy -n mcp -o yaml > /tmp/zai-proxy-before.yml
kubectl --server=$S get deployment/zai-proxy-test -n mcp -o yaml > /tmp/zai-proxy-test-before.yml

current_image=$(kubectl --server=$S get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}')
echo "Current image: $current_image"

kubectl --server=$S rollout history deployment/zai-proxy -n mcp
kubectl --server=$S logs -n mcp deployment/zai-proxy --tail=10
kubectl --server=$S get endpoints -n mcp zai-proxy
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | head -20
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd

# --- Write half: the Git lever you'll actually pull ---
cd /home/coding/declarative-config
git status --short              # clean checkout?
git log --oneline -3            # can you see the promotion commit?
git push --dry-run origin main  # push access works?
```

### Automated Readiness Check Script

```bash
#!/bin/bash
# Verify both halves of the rollback path are healthy.

set -e

S=http://traefik-apexalgo-iad:8001
NAMESPACE="mcp"
PRODUCTION_DEPLOYMENT="zai-proxy"

echo "=== Testing Rollback Path Readiness ==="

echo "Test 1: Read production deployment"
kubectl --server=$S get deployment/$PRODUCTION_DEPLOYMENT -n $NAMESPACE

echo "Test 2: Rollout history readable"
kubectl --server=$S rollout history deployment/$PRODUCTION_DEPLOYMENT -n $NAMESPACE | head -3

echo "Test 3: Pod status readable"
kubectl --server=$S get pods -n $NAMESPACE -l app=zai-proxy,variant=production

echo "Test 4: Logs readable"
kubectl --server=$S logs -n $NAMESPACE deployment/$PRODUCTION_DEPLOYMENT --tail=10

echo "Test 5: Metrics reachable (tailnet URL)"
curl -sk --max-time 10 https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | head -5

echo "Test 6: Health reachable"
curl -sk --max-time 10 https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health

echo "Test 7: ArgoCD app observable"
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd

echo "Test 8: declarative-config pushable"
cd /home/coding/declarative-config
git status --short || true
git push --dry-run origin main

echo "=== Rollback path ready ==="
```

---

## Part 6: Post-Rollback Actions

### After Rolling Back Canary

1. **Fix the issues:**
   - Investigate root cause
   - Fix code or configuration
   - Add regression tests

2. **Re-test canary:**
   - Deploy fixed version to canary (via declarative-config)
   - Run functional tests
   - Monitor metrics
   - Validate with worker traffic

3. **Re-promote when ready:**
   - Follow promotion procedure
   - Monitor production metrics
   - Have the revert ready

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
   - Have the revert commit ready

---

## Part 7: Change and Inspection Reference

### Making a Change (the only path)

| Change | How |
|--------|-----|
| Production image/version | Edit `k8s/apexalgo-iad/mcp/zai-proxy.yml` (image + `version:` label), commit to `main`, push |
| Canary image/version | Edit `k8s/apexalgo-iad/mcp/zai-proxy-test.yml`, same path |
| Scale either deployment | `replicas:` in its manifest, same path |
| Change env vars / probes / resources | Same manifest, same path |
| Rollback to previous version | `git revert` the change commit, push |
| Rollback to specific version | Pin the manifest to the known-good tag, push |
| Hurry a lagging sync | `argocd app sync mcp-ns-apexalgo-iad` (applies Git) |

Every one of these lands via ArgoCD app `mcp-ns-apexalgo-iad` (automated
sync, `selfHeal`, `prune`). There is no kubectl equivalent — by policy and by
mechanical enforcement.

### Read-Only kubectl Reference

```bash
S=http://traefik-apexalgo-iad:8001

# Get pods / watch pod changes
kubectl --server=$S get pods -n mcp -l app=zai-proxy
kubectl --server=$S get pods -n mcp -l app=zai-proxy -w

# Describe deployment / pod
kubectl --server=$S describe deployment/zai-proxy -n mcp
kubectl --server=$S describe pod/<pod-name> -n mcp

# View / stream logs
kubectl --server=$S logs -n mcp deployment/zai-proxy --tail=100
kubectl --server=$S logs -f -n mcp deployment/zai-proxy

# Rollout status / history (read-only verbs)
kubectl --server=$S rollout status deployment/zai-proxy -n mcp
kubectl --server=$S rollout history deployment/zai-proxy -n mcp

# Get endpoints
kubectl --server=$S get endpoints -n mcp | grep zai-proxy

# Current image
kubectl --server=$S get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# ArgoCD application state (ArgoCD runs on ardenone-manager)
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd
```

Not available from this identity — and not needed by any step above:
`exec`, `port-forward`, Secret reads, and every mutating verb.

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
                    │ Stand down canary │  │ Document & Monitor│
                    │ via manifest      │  └───────────────────┘
                    │ (replicas: 0)     │
                    └─────────┬─────────┘
                              │
              ┌───────────────┴───────────────┐
              │                               │
              ▼                               ▼
    ┌───────────────────┐          ┌───────────────────┐
    │ Remove canary     │◄─────────│ Create Failure    │
    │ manifest (git rm) │          │ Bead              │
    └─────────┬─────────┘          └─────────┬─────────┘
              │                               │
              ▼                               ▼
    ┌───────────────────┐          ┌───────────────────┐
    │ Verify Production │          │ Collect Diagnostics│
    │ Still Healthy     │          └───────────────────┘
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

### Read-Only Access from codinghome — and why it is enough

The credential-free proxy (`--server=http://traefik-apexalgo-iad:8001`)
serves the `devpod-observer` ServiceAccount: read-only by construction.

**Available (read-only):**
- `kubectl get` pods / deployments / services / events / replicasets
- `kubectl describe` any of the above
- `kubectl logs` / `logs -f`
- `kubectl rollout status` / `rollout history`
- `kubectl get application` (via the ardenone-manager endpoint, for ArgoCD state)

**Not available — by design, and by org policy:**
- `kubectl scale`, `rollout undo/restart/pause`, `set image/env`, `patch`,
  `edit`, `delete`, `apply` — prohibited org-wide for ArgoCD-managed
  resources and mechanically blocked; `selfHeal` reverts them regardless
- `kubectl exec` / `port-forward`
- Reading Secrets (the proxy identity cannot `get secrets`)

None of that is a gap to work around: **GitOps is the rollback path**, and it
needs nothing but a clean declarative-config checkout and push access.

### Rollback with Read-Only Access

**GitOps Rollback (the only option)**

```bash
cd /home/coding/declarative-config
git log --oneline -5
git revert <problematic-commit-sha>
git push origin main
# ArgoCD app mcp-ns-apexalgo-iad syncs the revert automatically;
# argocd app sync mcp-ns-apexalgo-iad if you can't wait
```

**If you cannot push** (no checkout, push denied): create a bead requesting
the rollback, with the read-only evidence attached:

```bash
cd /home/coding/zai-proxy

bead create --issue-type bug --priority 0 \
  --label critical --label rollback --label production --label human \
  --title "URGENT: Request production rollback for zai-proxy"

bead update <bead-id> --notes "CRITICAL: Production rollback requested

  Current Issues:
  - [Describe symptoms]

  Requested Action:
  - git revert <promotion-commit-sha> in jedarden/declarative-config

  Verified via read-only proxy:
  - Production pods: $(kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production -o wide)
  - Current image: $(kubectl --server=http://traefik-apexalgo-iad:8001 get deployment/zai-proxy -n mcp -o jsonpath='{.spec.template.spec.containers[0].image}')"
```

There is no "direct admin kubectl" escape hatch: even with cluster-admin
credentials, mutating an ArgoCD-managed resource is prohibited (and
`selfHeal` reverts it). The sanctioned emergency lever is
`argocd app sync`, which applies the committed repo — faster than a kubectl
mutation that gets undone.

### Verification with Read-Only Access

You CAN verify the cluster state with read-only access — see the command
list in Part 7; every verification step in this document uses only those.

---

## Related Documentation

- [CANARY_PROMOTION_PROCEDURE.md](CANARY_PROMOTION_PROCEDURE.md) - Promoting canary to production
- [CANARY_PROMOTION_CHECKLIST.md](CANARY_PROMOTION_CHECKLIST.md) - Promotion checklist
- [DEPLOYMENT.md](DEPLOYMENT.md) - Worker configuration and dual-deployment workflow
- [TOKEN_COUNTING.md](TOKEN_COUNTING.md) - Token counting implementation
- [REGRESSION_TESTING.md](REGRESSION_TESTING.md) - Running regression tests
- [MONITORING_SETUP.md](MONITORING_SETUP.md) - Metrics and alerting setup

---

## Recovery Timeline

| Action | Time | Notes |
|--------|------|-------|
| Stand canary down (manifest commit + sync) | <2min | `replicas: 0` push; `argocd app sync` to hurry |
| Verify production healthy | <1min | Confirm no impact |
| Production rollback (revert commit + sync) | <5min | Revert, push, optional `app sync` |
| Collect diagnostics | <5min | For analysis (read-only) |
| Document failure | <10min | Postmortem on the incident bead |
| **Canary rollback time** | **<15min** | Production unaffected |
| **Production rollback time** | **<5min** | Brief interruption |

**Key Point:** Production is never modified during canary rollback, so downtime is zero.

---

**Document Version:** 3.1.0
**Last Updated:** 2026-10-23
**Maintained By:** Claude Code Workers
