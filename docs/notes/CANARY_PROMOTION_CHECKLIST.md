# Canary to Production Promotion Checklist

Use this checklist when promoting a canary deployment to production.

## How Changes Reach the Cluster (read this first)

zai-proxy on apexalgo-iad is **ArgoCD-managed**. There is no kubectl path that
changes it: `kubectl set image`, `scale`, `edit`, `patch`, `rollout undo` and
friends are prohibited org-wide, mechanically blocked by the org-rule-guard
hook, denied to the read-only identity anyway, and ArgoCD `selfHeal` would
revert them moments later. The **only** documented change path is:

1. Edit the manifest in `jedarden/declarative-config` at
   `k8s/apexalgo-iad/mcp/zai-proxy.yml`
2. Commit to `main` and push to origin (Forgejo — the push mirror carries the
   commit to GitHub, which the ArgoCD Application tracks)
3. Let ArgoCD sync (app `mcp-ns-apexalgo-iad`, automated sync with
   `selfHeal` + `prune`)

(As of 2026-10-11 no canary deployment is checked in or running — bringing
one up means adding its manifest under `k8s/apexalgo-iad/mcp/` first, via
this same GitOps path. The steps below assume that has happened.)

Read-only kubectl from codinghome needs no kubeconfig — use the
credential-free proxy endpoint:

```bash
kubectl --server=http://traefik-apexalgo-iad:8001 <get | describe | logs | rollout status | rollout history> ...
```

It cannot `exec`, `port-forward`, or write; it also cannot read Secrets.
(`~/.kube/apexalgo-iad.kubeconfig` exists on this box, but nothing in this
checklist needs it — every change goes through Git.)

Production can be reached directly over the tailnet (self-signed VPN cert,
hence `-k`):

```bash
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics
```

## Quick Reference Commands

```bash
# --- Promotion (the only change path) ---
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy.yml   # image: tag AND the pod-template `version:` label
git add k8s/apexalgo-iad/mcp/zai-proxy.yml
git commit -m "chore: promote zai-proxy to vVERSION"
git push origin main                          # Forgejo -> GitHub mirror -> ArgoCD

# --- Watch it land (read-only) ---
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp
kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd

# --- Rollback if needed: revert the commit, never kubectl ---
cd /home/coding/declarative-config
git revert <promotion-commit-sha>
git push origin main
```

---

## Phase 1: Pre-Promotion Validation

### Canary Health Check
- [ ] Canary pods are Running and Ready
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=test
  ```
  Expected: All pods `Running` and `1/1` Ready

- [ ] No canary-specific Prometheus alerts firing
  ```bash
  # Check Grafana or AlertManager for:
  # - ZaiProxyCanaryDeploymentDown
  # - ZaiProxyCanaryErrorRateHigherThanProduction
  # - ZaiProxyCanaryHighErrorRate
  # - ZaiProxyCanaryLatencyDegraded
  ```

- [ ] Canary is receiving traffic (if using split traffic)
  ```bash
  # No VPN route exists for the canary; its own /metrics output is visible in
  # its logs, or via Prometheus once a ServiceMonitor is wired up:
  kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy-test --tail=100
  ```

### Functional Testing
- [ ] Health endpoint responds
  ```bash
  # Either add an IngressRoute for the canary in declarative-config, or check
  # readiness through the cluster's own view of it:
  kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=test
  ```
  Expected: pods Ready (readiness probes hit `/health`)

- [ ] Token counting is working
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy-test --tail=50 | grep "Token usage"
  ```
  Expected: Log entries showing `Token usage: input=X, output=Y`

- [ ] Metrics are being exported
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy-test --tail=200 | grep zai_proxy_tokens_total
  ```
  Expected: `variant="test"` labelled metrics

### Worker Testing
Workers are NEEDLE fleet workers. Their endpoint comes from
`ANTHROPIC_BASE_URL` in `~/.config/needle/adapters/*.yaml` (today all point at
the production URL above). To canary-test with a worker, point one adapter at
the canary's URL and relaunch that worker.

- [ ] At least one worker has tested the canary endpoint
  ```bash
  # Confirm which endpoint workers would use:
  grep -l "zai-proxy-test" ~/.config/needle/adapters/*.yaml || echo "no adapter points at the canary yet"

  # Confirm the canary actually saw worker traffic (from the canary's own view):
  kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy-test --tail=100 | grep "Token usage"
  ```

- [ ] Worker token counting verified
  ```promql
  sum by (model) (increase(zai_proxy_tokens_total{variant="test"}[30m]))
  ```
  Expected: non-zero series for each model the worker used (labels:
  `variant`, `model`, `direction`, `pricing_tier`).
  Runnable today via the Prometheus HTTP API:
  ```bash
  curl -sk --get 'https://prometheus-apexalgo-iad-ts.ardenone.com:8444/api/v1/query' \
    --data-urlencode 'query=sum by (model) (increase(zai_proxy_tokens_total{variant="test"}[30m]))'
  ```
  Note: as of 2026-10-11 no ServiceMonitor for zai-proxy is wired up, so this
  returns empty until one lands — the log check above is the reliable
  verification today.

### Version Confirmation
- [ ] Canary version is confirmed
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 get deployment/zai-proxy-test -n mcp \
    -o jsonpath='{.spec.template.spec.containers[0].image}'
  ```
  Note the version (e.g., `1.2.1-canary`)

- [ ] Stable version is determined
  Example: `1.2.1-canary` → `1.2.1`

---

## Phase 2: Promotion Execution

### Image Update (GitOps — the only path)
- [ ] Production manifest updated in declarative-config
  ```bash
  cd /home/coding/declarative-config
  $EDITOR k8s/apexalgo-iad/mcp/zai-proxy.yml
  # Change BOTH:
  #   spec.template.spec.containers[0].image: ronaldraygun/zai-proxy:VERSION
  #   spec.template.metadata.labels.version: "VERSION"     # keep in step with the image
  git add k8s/apexalgo-iad/mcp/zai-proxy.yml
  git commit -m "chore: promote zai-proxy to vVERSION"
  git push origin main
  ```
  (The manifest also carries the pod-template `version:` label — the old
  `kubectl set image` path never updated it, which made the Phase 4
  version-label check lie. Editing the manifest fixes both at once.)

- [ ] Image update verified after ArgoCD sync
  ```bash
  kubectl --server=http://traefik-ardenone-manager:8001 get application mcp-ns-apexalgo-iad -n argocd
  # SYNC STATUS: Synced
  kubectl --server=http://traefik-apexalgo-iad:8001 get deployment/zai-proxy -n mcp \
    -o jsonpath='{.spec.template.spec.containers[0].image}' && echo
  ```
  Expected: New version tag

### Rollout Initiation
- [ ] Rollout status checked
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp
  ```

- [ ] Pod replacement monitored
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production -w
  ```
  Watch until all pods are new version

---

## Phase 3: During Rollout Monitoring

### Pod Health
- [ ] New pods are becoming Ready
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production
  ```
  Expected: All pods `Running` and `1/1` Ready

- [ ] No pod crash loops
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production \
    -o custom-columns=NAME:.metadata.name,RESTARTS:.status.containerStatuses[0].restartCount
  ```
  Expected: RESTARTS = 0 or 1

### Error Rate
- [ ] Error rate below threshold (<5%)
  ```promql
  sum(rate(zai_proxy_requests_total{variant="production",status=~"5.."}[5m]))
  /
  sum(rate(zai_proxy_requests_total{variant="production"}[5m]))
  ```
  Expected: < 0.05 (5%)
  (Direct, always-available source: the proxy's own /metrics via the tailnet
  URL — status codes are on the `status_code` label there.)

- [ ] No spike in 5xx errors
  Check logs: `kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy --tail=100`

### Latency
- [ ] P95 latency hasn't regressed (>50% increase)
  ```promql
  histogram_quantile(0.95,
    sum(rate(zai_proxy_request_duration_seconds_bucket{variant="production"}[5m])) by (le)
  )
  ```
  Expected: Similar to pre-rollout baseline

### Token Counting
- [ ] Token metrics are being exported
  ```bash
  curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | \
    grep zai_proxy_tokens_total | grep production
  ```

- [ ] Token counting latency is acceptable (<100ms p99)
  ```promql
  histogram_quantile(0.99,
    sum(rate(zai_proxy_token_count_duration_seconds_bucket{variant="production"}[5m])) by (le)
  )
  ```
  Expected: < 0.1 seconds
  (Direct source: `curl -sk .../metrics | grep zai_proxy_token_count_duration_seconds_bucket`)

---

## Phase 4: Post-Rollout Verification

### Version Verification
- [ ] All pods running new version
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production \
    -o custom-columns=NAME:.metadata.name,VERSION:.metadata.labels.version
  ```
  Expected: All pods show new version

- [ ] Deployment shows updated image
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 describe deployment/zai-proxy -n mcp | grep Image:
  ```
  Expected: New image tag

### Worker Verification
- [ ] Workers are successfully making requests
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy --tail=100 | grep "Token usage"
  ```
  Expected: Token usage log entries

- [ ] Token counting is working for workers
  ```bash
  curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | \
    grep zai_proxy_tokens_total | grep production
  ```
  Expected: Incrementing counters

### Log Verification
- [ ] No errors in production logs
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy --tail=100 | grep -i error
  ```
  Expected: No error entries (or only expected transient errors)

- [ ] Startup logs show correct configuration
  ```bash
  kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy --tail=100 | grep -E "(Token counting|DEPLOYMENT_VARIANT)"
  ```
  Expected: `DEPLOYMENT_VARIANT=production`, `Token counting enabled`

### Metrics Verification
- [ ] Request rate is healthy
  ```promql
  sum(rate(zai_proxy_requests_total{variant="production"}[5m]))
  ```
  Expected: Non-zero, similar to pre-rollout

- [ ] Success rate is high (>95%)
  ```promql
  sum(rate(zai_proxy_requests_total{variant="production",status=~"2.."}[5m]))
  /
  sum(rate(zai_proxy_requests_total{variant="production"}[5m]))
  ```
  Expected: > 0.95

---

## Phase 5: Finalization

### VERSION File Update
- [ ] VERSION file updated (removed -canary suffix)
  ```bash
  cd /home/coding/zai-proxy
  echo "VERSION" > VERSION
  cat VERSION
  ```
  Replace `VERSION` with stable version number

### Git Commit and Tag
- [ ] VERSION change committed
  ```bash
  cd /home/coding/zai-proxy
  git add VERSION
  git commit -m "chore: release zai-proxy vVERSION"
  ```

- [ ] Git tag created
  ```bash
  git tag -a vVERSION -m "Release zai-proxy vVERSION"
  ```

- [ ] Tag pushed to remote
  ```bash
  git push origin vVERSION
  ```

### Optional Cleanup
- [ ] Canary deployment stood down (if no longer needed) — via its manifest,
  never `kubectl scale`
  ```bash
  cd /home/coding/declarative-config
  $EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml   # replicas: 0  (or git rm the file)
  git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
  git commit -m "chore: stand down zai-proxy canary after vVERSION promotion"
  git push origin main
  ```

- [ ] Canary manifest updated to next development version (if applicable) —
  same file, same GitOps path.

---

## Rollback Triggers

**Immediately rollback if ANY of these conditions occur:**

- [ ] Error rate exceeds 10% for more than 2 minutes
- [ ] P95 latency increases by >100% for more than 2 minutes
- [ ] More than 50% of pods are NotReady
- [ ] Pods are crash looping
- [ ] Token counting stops working
- [ ] Workers cannot connect or experience high failure rates

### Rollback Procedure (GitOps only)

```bash
# 1. Revert the promotion commit in declarative-config
cd /home/coding/declarative-config
git log --oneline -5                     # find the promotion commit
git revert <promotion-commit-sha>
git push origin main

# 2. If the sync lags, force it from the repo (this APPLIES Git, it does not bypass it):
argocd app sync mcp-ns-apexalgo-iad      # or the equivalent button in the ArgoCD UI

# 3. Watch the rollback land (read-only)
kubectl --server=http://traefik-apexalgo-iad:8001 rollout status deployment/zai-proxy -n mcp
kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production -w
```

Do **not** reach for `kubectl rollout undo` / `scale` — they are blocked, and
`selfHeal` would revert them anyway.

---

## Post-Promotion Monitoring (First Hour)

Monitor these metrics for the first hour after promotion:

- [ ] Request rate remains stable
- [ ] Error rate stays below 5%
- [ ] P95 latency doesn't increase by >20%
- [ ] Token counting metrics are incrementing
- [ ] No new Prometheus alerts firing
- [ ] Worker sessions show no unexpected errors

### Commands for Post-Promotion Monitoring

```bash
# Watch pod status
watch 'kubectl --server=http://traefik-apexalgo-iad:8001 get pods -n mcp -l app=zai-proxy,variant=production'

# Stream logs
kubectl --server=http://traefik-apexalgo-iad:8001 logs -f -n mcp deployment/zai-proxy

# Check metrics every minute
watch -n 60 'curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | grep zai_proxy_requests_total'
```

---

## Sign-off

**Promotion completed by:** _____________________ **Date:** ____________

**Verification completed by:** _____________________ **Date:** ____________

**Notes/Issues:**

_________________________________________________________________________

_________________________________________________________________________

_________________________________________________________________________
