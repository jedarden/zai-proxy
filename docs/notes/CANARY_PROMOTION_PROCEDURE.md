# Canary to Production Promotion Procedure

This document describes the procedure to promote a canary deployment to production after successful testing.

## Overview

The zai-proxy deployment uses a dual-deployment strategy:
- **Production deployment** (`zai-proxy`): Live traffic
- **Canary deployment** (`zai-proxy-test`): Testing new versions

After a canary version has been validated, it can be promoted to production using this procedure.

(As of 2026-10-11 no canary manifest is checked in — a canary run starts by
adding its manifest under `k8s/apexalgo-iad/mcp/` in declarative-config.)

**Everything on the cluster is ArgoCD-managed.** The only documented change
path is declarative-config → commit to `main` → push → ArgoCD sync. There is
no kubectl mutation path: `kubectl set image`, `edit`, `scale`, `patch`,
`rollout undo/restart` are prohibited org-wide, mechanically blocked by the
org-rule-guard hook, and ArgoCD `selfHeal` would revert them anyway. Rollback
is a `git revert` in declarative-config (see below).

## Prerequisites

Before promoting canary to production, ensure:

1. **Canary is healthy**: No alerts firing in Prometheus
2. **Testing complete**: Functional tests pass on canary endpoint
3. **Metrics validated**: Token counting, rate limiting, and error rates are acceptable
4. **Workers tested**: At least one worker has successfully used the canary endpoint
5. **Version ready**: Canary image tag is known and available
6. **declarative-config checkout clean**: you will be committing to
   `/home/coding/declarative-config` on `main`

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
                    └──────────────┬──────────────────────┘
                                   │ declared by
                                   ▼
                    ┌─────────────────────────────────────┐
                    │ jedarden/declarative-config         │
                    │ k8s/apexalgo-iad/mcp/zai-proxy.yml  │
                    │  → ArgoCD app mcp-ns-apexalgo-iad   │
                    └─────────────────────────────────────┘
```

Read-only cluster access from codinghome (no kubeconfig, no token):

```bash
kubectl --server=http://traefik-apexalgo-iad:8001 ...   # get/describe/logs/rollout status|history
```

The proxy identity cannot `exec`, `port-forward`, or read Secrets — and no
runbook step needs to, because nothing here writes to the cluster.

Production is directly reachable over the tailnet (self-signed VPN cert, use `-k`):

```bash
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health
```

## Procedure

### Step 1: Update the Production Manifest (the only change path)

**Example: Promoting 1.2.1-canary to 1.2.1**

```bash
cd /home/coding/declarative-config

# Edit k8s/apexalgo-iad/mcp/zai-proxy.yml — change BOTH the image tag and the
# pod-template version label so they stay in step:
#   image: ronaldraygun/zai-proxy:1.2.1        (was 1.2.0)
#   labels:
#     version: "1.2.1"                          (was "1.2.0")
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy.yml

git add k8s/apexalgo-iad/mcp/zai-proxy.yml
git commit -m "chore: promote zai-proxy to v1.2.1"
git push origin main
```

Push goes to Forgejo `origin`; the server-side push mirror carries the commit
to GitHub (`github.com/jedarden/declarative-config`), which is the repo the
ArgoCD Application tracks. ArgoCD then syncs automatically
(app `mcp-ns-apexalgo-iad`: automated sync, `selfHeal`, `prune`).

If the sync lags, hurry it **from the repo** — `argocd app sync
mcp-ns-apexalgo-iad` (or the Sync button in the ArgoCD UI) applies Git; it
does not bypass it:

```bash
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd
# SYNC STATUS should return to Synced and health to Healthy
```

### Step 2: Watch the Rollout

The manifest itself defines the rollout strategy (RollingUpdate,
`maxUnavailable: 1`, `maxSurge: 0`, anti-affinity spread across nodes) —
Kubernetes performs it automatically once ArgoCD applies the new template.

```bash
# Monitor the rollout (read-only)
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp

# Watch pods being replaced
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production -w
```

Characteristics of the declared strategy:
- Gradual replacement of pods (one at a time — `maxUnavailable: 1`,
  `maxSurge: 0`; the fleet has no retry budget for zero endpoints)
- Old pods serve traffic until new pods are ready
- Rollback is a Git revert (see "Rollback Procedure") — there is no
  `kubectl rollout undo` path

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

Runnable via the Prometheus HTTP API (tailnet, `-k` for the VPN cert):
`https://prometheus-apexalgo-iad-ts.ardenone.com:8444/api/v1/query`.
Note: as of 2026-10-11 no ServiceMonitor for zai-proxy is wired up, so these
return empty until one lands — the direct /metrics endpoint works today.

**Real-time monitoring:**

```bash
# Watch pod status
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production -w

# Stream production logs
kubectl --server=http://traefik-apexalgo-iad:8001 logs -f -n mcp deployment/zai-proxy --tail=100

# Check metrics endpoint directly (this is the URL fleet workers use)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | grep zai_proxy
```

### Step 4: Verify Workers Successfully Use New Version

After rollout completes, verify workers are using the new version:

**1. Check pod versions:**

```bash
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp \
  -l app=zai-proxy,variant=production \
  -o custom-columns=NAME:.metadata.name,VERSION:.metadata.labels.version

# Expected output:
# NAME                             VERSION
# zai-proxy-7d8f9c6b5-x2k4p        1.2.1
```

**2. Verify worker connectivity:**

```bash
kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy --tail=100 | grep "Token usage"

# The logs should show activity from workers
# Expected: Token usage: input=X, output=Y
```

**3. Check worker token counting metrics:**

```bash
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | \
  grep zai_proxy_tokens_total | \
  grep 'variant="production"'
```

**4. Verify image tag in deployment:**

```bash
kubectl --server=http://traefik-apexalgo-iad:8001 get deployment/zai-proxy -n mcp \
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
- [ ] Canary deployment is healthy (`kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l variant=test`)
- [ ] No Prometheus alerts firing for canary
- [ ] Functional tests pass on canary endpoint
- [ ] Token counting verified on canary
- [ ] At least one worker tested with canary endpoint
- [ ] Canary image tag is confirmed (e.g., `1.2.1-canary`)

### Promotion
- [ ] Production manifest updated in declarative-config (image + `version:` label)
- [ ] Commit pushed to `main` (origin)
- [ ] ArgoCD app `mcp-ns-apexalgo-iad` back to `Synced`
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
- [ ] Canary stood down via its manifest (optional)

### Cleanup (Optional)
- [ ] Stand down the canary if no longer needed: set `replicas: 0` (or remove
  the manifest) in `k8s/apexalgo-iad/mcp/zai-proxy-test.yml` and push
- [ ] Update canary manifest to next development version (same GitOps path)

## Rollback Procedure

If issues are detected after promotion, roll back **through Git**:

### Rollback (GitOps — the only path)

```bash
# 1. Revert the promotion commit
cd /home/coding/declarative-config
git log --oneline -5                     # identify the promotion commit
git revert <promotion-commit-sha>
git push origin main

# 2. Hurry a lagging sync from the repo (applies Git; does not bypass it)
argocd app sync mcp-ns-apexalgo-iad      # or the ArgoCD UI Sync button

# 3. Monitor the rollback (read-only)
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production -w

# 4. Verify rollback completed
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production
```

### Rollback to a Specific Older Version

`kubectl rollout undo --to-revision` is not available (and would be reverted).
Instead, edit the manifest to the exact known-good tag and push:

```bash
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy.yml   # image + version label -> known-good tag
git add k8s/apexalgo-iad/mcp/zai-proxy.yml
git commit -m "fix: roll zai-proxy back to vKNOWN_GOOD"
git push origin main
```

For reference, `kubectl rollout history` still works read-only to see what
revisions ran:
```bash
kubectl --server=http://traefik-apexalgo-iad:8001 rollout history deployment/zai-proxy -n mcp
```

## Read-Only kubectl Reference

Every command below works through the credential-free proxy. Anything that
mutates the deployment does not — make the change in declarative-config.

### Inspection

```bash
# View current image
kubectl --server=http://traefik-apexalgo-iad:8001 get deployment/zai-proxy -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# List production pods
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production

# List canary pods
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=test

# Watch pod changes
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy -w

# Get deployment details
kubectl --server=http://traefik-apexalgo-iad:8001 describe deployment/zai-proxy -n mcp
```

### Rollouts (read-only verbs only)

```bash
# Check rollout status
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp

# View rollout history
kubectl --server=http://traefik-apexalgo-iad:8001 rollout history deployment/zai-proxy -n mcp
```

(`rollout pause/resume/undo/restart` are mutations — not available, not
needed: the declared strategy in the manifest governs the rollout, and
rollback is `git revert`.)

### Logs

```bash
kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy --tail=100
kubectl --server=http://traefik-apexalgo-iad:8001 logs -f -n mcp deployment/zai-proxy
```

### Metrics and Health

```bash
# Check service endpoints
kubectl --server=http://traefik-apexalgo-iad:8001 get endpoints -n mcp | grep zai-proxy

# Health / metrics over the tailnet (what workers use; -k for the VPN cert)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics

# ArgoCD sync state for the mcp namespace
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd
```

## Troubleshooting

### Rollout Stuck

```bash
# Check rollout status (read-only)
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp

# Inspect what ArgoCD thinks
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd

# Inspect the deployment and its pods
kubectl --server=http://traefik-apexalgo-iad:8001 describe deployment/zai-proxy -n mcp
kubectl --server=http://traefik-apexalgo-iad:8001 describe pod <pod-name> -n mcp

# If the new version is bad, roll back through Git (see Rollback Procedure) —
# there is no pause/resume lever, by policy.
```

### Pods Not Ready

```bash
# Describe pod to see events
kubectl --server=http://traefik-apexalgo-iad:8001 describe pod <pod-name> -n mcp

# Check pod logs
kubectl --server=http://traefik-apexalgo-iad:8001 logs <pod-name> -n mcp

# Common issues:
# - Image pull errors: Check image name and registry secrets (both in declarative-config)
# - Resource limits: Check pod resource requests/limits (in the manifest)
# - Health check failures: Check /health endpoint
```

### Workers Not Connecting

```bash
# Check service is accessible
kubectl --server=http://traefik-apexalgo-iad:8001 get svc -n mcp | grep zai-proxy

# Test the endpoint workers actually use (tailnet URL)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health

# Check which endpoint workers are configured with
grep -n "ANTHROPIC_BASE_URL" ~/.config/needle/adapters/*.yaml | head

# Watch the worker's tmux session output (worker sessions are tmux sessions)
tmux capture-pane -p -t <worker-session-name> | tail -50
```

## Related Documentation

- [DEPLOYMENT.md](DEPLOYMENT.md) - Worker configuration and dual-deployment workflow
- [TOKEN_COUNTING.md](TOKEN_COUNTING.md) - Token counting implementation and monitoring
- [REGRESSION_TESTING.md](REGRESSION_TESTING.md) - Running regression tests before promotion
