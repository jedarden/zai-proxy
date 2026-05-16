# Canary to Production Promotion Checklist

Use this checklist when promoting a canary deployment to production.

## Quick Reference Commands

```bash
# Set kubeconfig for apexalgo-iad cluster
export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig

# Production deployment update
kubectl set image deployment/zai-proxy proxy=ronaldraygun/zai-proxy:VERSION -n mcp

# Monitor rollout
kubectl rollout status deployment/zai-proxy -n mcp

# Rollback if needed
kubectl rollout undo deployment/zai-proxy -n mcp
```

---

## Phase 1: Pre-Promotion Validation

### Canary Health Check
- [ ] Canary pods are Running and Ready
  ```bash
  kubectl get pods -n mcp -l app=zai-proxy,variant=test
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
  curl -s http://zai-proxy-test.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_requests_total
  ```

### Functional Testing
- [ ] Health endpoint responds
  ```bash
  curl -s http://zai-proxy-test.mcp.svc.cluster.local:8080/health
  ```
  Expected: `{"status":"ok"}`

- [ ] Token counting is working
  ```bash
  kubectl logs -n mcp deployment/zai-proxy-test --tail=50 | grep "Token usage"
  ```
  Expected: Log entries showing `Token usage: input=X, output=Y`

- [ ] Metrics are being exported
  ```bash
  curl -s http://zai-proxy-test.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_tokens_total
  ```
  Expected: Metrics with `variant="test"` label

### Worker Testing
- [ ] At least one worker has tested canary endpoint
  ```bash
  # Check worker logs for canary endpoint usage
  grep -r "zai-proxy-test" ~/.beads-workers/*.log
  ```

- [ ] Worker token counting verified
  ```bash
  # Query Prometheus for test variant token metrics
  # Should show token counts from worker activity
  ```

### Version Confirmation
- [ ] Canary version is confirmed
  ```bash
  kubectl get deployment/zai-proxy-test -n mcp \
    -o jsonpath='{.spec.template.spec.containers[0].image}'
  ```
  Note the version (e.g., `1.2.1-canary`)

- [ ] Stable version is determined
  Example: `1.2.1-canary` → `1.2.1`

---

## Phase 2: Promotion Execution

### Image Update
- [ ] Production image updated to canary version (without -canary suffix)
  ```bash
  kubectl set image deployment/zai-proxy \
    proxy=ronaldraygun/zai-proxy:VERSION -n mcp
  ```
  Replace `VERSION` with the stable version number

- [ ] Image update verified
  ```bash
  kubectl get deployment/zai-proxy -n mcp \
    -o jsonpath='{.spec.template.spec.containers[0].image}' && echo
  ```
  Expected: New version tag

### Rollout Initiation
- [ ] Rollout status checked
  ```bash
  kubectl rollout status deployment/zai-proxy -n mcp
  ```

- [ ] Pod replacement monitored
  ```bash
  kubectl get pods -n mcp -l app=zai-proxy,variant=production -w
  ```
  Watch until all pods are new version

---

## Phase 3: During Rollout Monitoring

### Pod Health
- [ ] New pods are becoming Ready
  ```bash
  kubectl get pods -n mcp -l app=zai-proxy,variant=production
  ```
  Expected: All pods `Running` and `1/1` Ready

- [ ] No pod crash loops
  ```bash
  kubectl get pods -n mcp -l app=zai-proxy,variant=production \
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

- [ ] No spike in 5xx errors
  Check logs: `kubectl logs -n mcp deployment/zai-proxy --tail=100`

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
  curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | \
    grep zai_proxy_tokens_total | grep production
  ```

- [ ] Token counting latency is acceptable (<100ms p99)
  ```promql
  histogram_quantile(0.99,
    sum(rate(zai_proxy_token_count_duration_seconds_bucket{variant="production"}[5m])) by (le)
  )
  ```
  Expected: < 0.1 seconds

---

## Phase 4: Post-Rollout Verification

### Version Verification
- [ ] All pods running new version
  ```bash
  kubectl get pods -n mcp -l app=zai-proxy,variant=production \
    -o custom-columns=NAME:.metadata.name,VERSION:.metadata.labels.version
  ```
  Expected: All pods show new version

- [ ] Deployment shows updated image
  ```bash
  kubectl describe deployment/zai-proxy -n mcp | grep Image:
  ```
  Expected: New image tag

### Worker Verification
- [ ] Workers are successfully making requests
  ```bash
  # Check production logs for worker activity
  kubectl logs -n mcp deployment/zai-proxy --tail=100 | grep "Token usage"
  ```
  Expected: Token usage log entries

- [ ] Token counting is working for workers
  ```bash
  curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | \
    grep zai_proxy_tokens_total | grep production
  ```
  Expected: Incrementing counters

### Log Verification
- [ ] No errors in production logs
  ```bash
  kubectl logs -n mcp deployment/zai-proxy --tail=100 | grep -i error
  ```
  Expected: No error entries (or only expected transient errors)

- [ ] Startup logs show correct configuration
  ```bash
  kubectl logs -n mcp deployment/zai-proxy --tail=100 | grep -E "(Token counting|DEPLOYMENT_VARIANT)"
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
  cd /home/coder/ardenone-cluster/containers/zai-proxy
  echo "VERSION" > VERSION
  cat VERSION
  ```
  Replace `VERSION` with stable version number

### Git Commit and Tag
- [ ] VERSION change committed
  ```bash
  cd /home/coder/ardenone-cluster/containers/zai-proxy
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
- [ ] Canary deployment scaled down (if no longer needed)
  ```bash
  kubectl scale deployment/zai-proxy-test -n mcp --replicas=0
  ```

- [ ] Canary manifest updated to next development version (if applicable)
  ```bash
  cd /home/coder/ardenone-cluster/cluster-configuration/apexalgo-iad/mcp
  # Edit zai-proxy-test.yml to next version
  ```

---

## Rollback Triggers

**Immediately rollback if ANY of these conditions occur:**

- [ ] Error rate exceeds 10% for more than 2 minutes
- [ ] P95 latency increases by >100% for more than 2 minutes
- [ ] More than 50% of pods are NotReady
- [ ] Pods are crash looping
- [ ] Token counting stops working
- [ ] Workers cannot connect or experience high failure rates

### Rollback Commands

```bash
# Quick rollback to previous version
kubectl rollout undo deployment/zai-proxy -n mcp

# Monitor rollback
kubectl rollout status deployment/zai-proxy -n mcp

# If rollback fails, scale to 0 and back up
kubectl scale deployment/zai-proxy -n mcp --replicas=0
kubectl scale deployment/zai-proxy -n mcp --replicas=1
```

---

## Post-Promotion Monitoring (First Hour)

Monitor these metrics for the first hour after promotion:

- [ ] Request rate remains stable
- [ ] Error rate stays below 5%
- [ ] P95 latency doesn't increase by >20%
- [ ] Token counting metrics are incrementing
- [ ] No new Prometheus alerts firing
- [ ] Worker logs show no unexpected errors

### Commands for Post-Promotion Monitoring

```bash
# Watch pod status
watch kubectl get pods -n mcp -l app=zai-proxy,variant=production

# Stream logs
kubectl logs -f -n mcp deployment/zai-proxy

# Check metrics every minute
watch -n 60 'curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_requests_total'
```

---

## Sign-off

**Promotion completed by:** _____________________ **Date:** ____________

**Verification completed by:** _____________________ **Date:** ____________

**Notes/Issues:**

_________________________________________________________________________

_________________________________________________________________________

_________________________________________________________________________
