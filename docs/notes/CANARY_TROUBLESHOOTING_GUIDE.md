# Canary Troubleshooting Guide

This guide covers common failure scenarios when testing canary deployments and how to diagnose and resolve them.

## How This Guide Works

- **Observing** the cluster is read-only and needs no kubeconfig:
  `kubectl --server=http://traefik-apexalgo-iad:8001 ...` (no `exec`, no
  `port-forward`, no Secret reads from this identity).
- **Fixing** anything is a Git operation: edit the manifest in
  `jedarden/declarative-config` (`k8s/apexalgo-iad/mcp/`), commit to `main`,
  push, let ArgoCD sync. `kubectl edit/set/scale/patch/rollout restart` are
  prohibited org-wide and mechanically blocked — a fix that tries to go
  through kubectl fails at the moment you need it.
- Production is directly reachable over the tailnet (self-signed VPN cert,
  use `-k`): `https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444`. The
  canary has no VPN route today — observe it through logs and Prometheus, or
  declare an IngressRoute for it in declarative-config.

## Quick Diagnosis Checklist

When canary issues occur, run these commands first:

```bash
S=http://traefik-apexalgo-iad:8001

# 1. Check canary pod status
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=test

# 2. Check recent canary events
kubectl --server=$S get events -n mcp --field-selector involvedObject.name=zai-proxy-test --sort-by='.lastTimestamp'

# 3. Stream canary logs
kubectl --server=$S logs -f -n mcp deployment/zai-proxy-test

# 4. Check canary metrics (from its logs — no exec from this identity;
#    status codes and token counters appear in log lines)
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=200 | grep -E "zai_proxy|error|Token usage"

# 5. Confirm production is unaffected (tailnet URL)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health
```

## Failure Scenarios

### Scenario 1: Canary Pods CrashLoopBackOff

**Symptoms:**
- Pods show `CrashLoopBackOff` status
- Pod restarts repeatedly
- No logs or only startup logs visible

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Check pod status and restart count
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=test \
  -o custom-columns=NAME:.metadata.name,RESTARTS:.status.containerStatuses[0].restartCount,STATUS:.status.phase

# Describe pod to see events
kubectl --server=$S describe pod -n mcp -l app=zai-proxy,variant=test

# Check previous container logs (if crash occurred)
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --previous

# Check termination reason
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=test \
  -o jsonpath='{.items[0].status.containerStatuses[0].lastState.terminated.reason}'
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Missing environment variable | Add missing env var to the canary manifest in declarative-config |
| Invalid API key | Update the OpenBao value behind the `zai-api-key` ExternalSecret (see Scenario 3) |
| Port conflict | Verify containerPort matches application port (manifest) |
| Out of memory (OOMKilled) | Increase memory limits in the manifest |
| Application panic/error | Fix code issue and rebuild image |

**Example Fix - Missing Environment Variable:**
```bash
# Check what's missing
kubectl --server=$S describe pod -n mcp -l app=zai-proxy,variant=test | grep -A 10 "Environment"

# Add the missing variable to the canary manifest and push
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml   # add to env: section
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: add missing env var to zai-proxy canary"
git push origin main
# ArgoCD applies it and the pod rolls — no rollout restart needed
```

**Example Fix - OOMKilled:**
```bash
# Check if OOMKilled
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=test \
  -o jsonpath='{.items[0].status.containerStatuses[0].lastState.terminated.reason}'
# If output is "OOMKilled", increase memory limit

# Raise the limit in the manifest
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml
#   resources:
#     limits:
#       memory: "128Mi"  # Increased from 64Mi
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: raise zai-proxy canary memory limit"
git push origin main
```

---

### Scenario 2: Canary Pods Not Ready

**Symptoms:**
- Pods show `Running` but `0/1` Ready
- Readiness probe failing
- Pod accepts no traffic

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Check readiness probe status
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=test \
  -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready

# Describe pod to see probe details
kubectl --server=$S describe pod -n mcp -l app=zai-proxy,variant=test | grep -A 5 "Readiness"

# Readiness probes hit /health — a NotReady pod is failing /health;
# check its logs for the reason (no exec from this identity)
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=50
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Health endpoint not responding | Fix /health endpoint in code |
| Port mismatch | Fix containerPort or application port (manifest) |
| Slow startup (not ready before probe) | Increase initialDelaySeconds (manifest) |
| Dependency not available | Fix external service connectivity |

**Example Fix - Slow Startup:**
```bash
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml

# Adjust readiness probe:
#   readinessProbe:
#     httpGet:
#       path: /health
#       port: 8080
#     initialDelaySeconds: 10  # Increased from 3
#     periodSeconds: 5
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: give zai-proxy canary more startup time"
git push origin main
```

**Example Fix - Port Mismatch:**
```bash
# Logs or events will show the probe failing; confirm which port the app
# listens on from its startup logs
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=50

# If listening on 8081 but probe checking 8080, correct the manifest:
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml
#   ports:
#   - containerPort: 8081  # Corrected from 8080
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: correct zai-proxy canary container port"
git push origin main
```

---

### Scenario 3: High Error Rate (>5%)

**Symptoms:**
- 5xx errors increasing
- Workers report failures
- Prometheus alert firing

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Check error rate from canary logs (status codes appear per request)
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=200 | grep -iE "error|5[0-9][0-9]"

# Stream logs for errors
kubectl --server=$S logs -f -n mcp deployment/zai-proxy-test | grep -i error

# Check for rate limiting (429 errors)
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=100 | grep "429\|rate limit"
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Rate limiting from z.ai | Reduce RATE_LIMIT_INITIAL/MAX values (manifest) |
| Invalid API key | Update the OpenBao value behind the ExternalSecret (below) |
| Upstream API changes | Check z.ai API status/docs |
| Token counting errors | Fix tokenizer logic |
| Request timeout errors | Increase client timeout settings (manifest) |

**Example Fix - Rate Limiting:**
```bash
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml

# Adjust rate limit env vars:
#   env:
#   - name: RATE_LIMIT_INITIAL
#     value: "1"  # Reduced from 2
#   - name: RATE_LIMIT_MAX
#     value: "3"  # Reduced from 5

git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: reduce zai-proxy canary rate limits"
git push origin main
# ArgoCD applies it and rolls the pod — no rollout restart needed
```

**Example Fix - Invalid API Key:**

`mcp/zai-api-key` is not a hand-made Secret: it is an ExternalSecret
(`k8s/apexalgo-iad/mcp/zai-api-key-externalsecret.yml`) syncing from
rs-manager's OpenBao at
`secret/rs-manager/apexalgo-iad/mcp/zai/api-key`, refreshed hourly. The
deployment carries `reloader.stakater.com/auto: "true"`, so pods roll
automatically when the Secret changes. Fix the value at its source:

```bash
# Write the new key to OpenBao — value via stdin, never as an argument
# (provisioning identity cannot read it back; verify by metadata version):
echo 'NEW_API_KEY' | bao-as rs-manager-provision bao kv put -cas=<current-version> \
  secret/rs-manager/apexalgo-iad/mcp/zai/api-key api_key=-

# Then wait for the ExternalSecret refresh (refreshInterval: 1h) — Reloader
# rolls the deployment when the Secret updates. Confirm recovery:
kubectl --server=http://traefik-apexalgo-iad:8001 logs -n mcp deployment/zai-proxy-test --tail=50
```

Do NOT `kubectl create secret ... | kubectl apply` — the ExternalSecret owns
that Secret and would overwrite the hand-made value on the next refresh.

---

### Scenario 4: Token Counting Broken

**Symptoms:**
- No token metrics in /metrics
- Logs show "Token counting failed"
- Token values are zero or incorrect

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Check logs for token errors
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=100 | grep -i "token"

# Check DEPLOYMENT_VARIANT is what the manifest declares
kubectl --server=$S get deployment/zai-proxy-test -n mcp -o yaml | grep -A1 DEPLOYMENT_VARIANT

# Verify token activity (production, direct /metrics over the tailnet)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | grep zai_proxy_tokens
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| DEPLOYMENT_VARIANT not set | Add DEPLOYMENT_VARIANT to the manifest env |
| Tokenizer initialization failed | Fix tokenizer code and rebuild |
| Tokenizer config invalid | Update tokenizer configuration (manifest) |
| Memory pressure (tokenizer OOM) | Increase memory limits (manifest) |

**Example Fix - Missing DEPLOYMENT_VARIANT:**
```bash
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml

# Add to env section:
#   - name: DEPLOYMENT_VARIANT
#     value: "test"
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: set DEPLOYMENT_VARIANT on zai-proxy canary"
git push origin main
```

---

### Scenario 5: Latency Degradation

**Symptoms:**
- P95 latency >2x baseline
- Slow response times
- Prometheus latency alert firing

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Check for slow-request log lines on the canary
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=200 | grep -iE "slow|duration|latency"

# Production comparison — direct /metrics (histograms + token count duration):
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | \
  grep -E "zai_proxy_request_duration_seconds|zai_proxy_token_count_duration_seconds" | head -20

# Check resource usage
kubectl --server=$S top pods -n mcp -l app=zai-proxy,variant=test

# Check declared limits
kubectl --server=$S get deployment/zai-proxy-test -n mcp -o yaml | grep -A 6 "resources:"
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| CPU throttling | Increase CPU limits (manifest) |
| Token counting slow | Optimize tokenizer or increase resources |
| Upstream API slow | Check z.ai API status |
| Network latency | Check cluster network issues |
| Memory pressure (GC) | Increase memory limits (manifest) |

**Example Fix - CPU Throttling:**
```bash
# Check current CPU limits (read-only)
kubectl --server=$S get deployment/zai-proxy-test -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}'

# If low (e.g., 100m), raise it in the manifest
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml
#   resources:
#     limits:
#       cpu: "200m"  # Increased from 100m
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: raise zai-proxy canary CPU limit"
git push origin main
```

---

### Scenario 6: Image Pull Errors

**Symptoms:**
- Pods stuck in `ImagePullBackOff` or `ErrImagePull`
- Pod events show "Failed to pull image"

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Check pod events for pull errors
kubectl --server=$S describe pod -n mcp -l app=zai-proxy,variant=test | grep -A 5 "Events:"

# Check what image is being pulled
kubectl --server=$S get deployment/zai-proxy-test -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}' && echo

# Verify image exists (from codinghome)
curl -s https://hub.docker.com/v2/repositories/ronaldraygun/zai-proxy/tags/ | \
  jq '.results[].name' | grep VERSION

# Confirm the pull secret is declared where the deployment expects it
# (imagePullSecrets: docker-hub-registry — a SealedSecret in declarative-config;
# the read-only identity cannot read Secrets directly)
grep -rn "docker-hub-registry" /home/coding/declarative-config/k8s/apexalgo-iad/kubernetes-reflector/docker-hub-registry-sealedsecret.yml | head -3
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Image tag doesn't exist | Build and push image with correct tag, then bump the manifest |
| Wrong registry/username | Fix image name in the manifest |
| Expired registry credentials | Re-seal `docker-hub-registry` and commit (below) |
| Registry rate limiting | Wait or use authenticated pulls |
| Network issue | Check cluster egress to Docker Hub |

**Example Fix - Image Tag Doesn't Exist:**
```bash
# List available tags
curl -s https://hub.docker.com/v2/repositories/ronaldraygun/zai-proxy/tags/ | \
  jq '.results[].name'

# If tag doesn't exist, build it
cd /home/coding/zai-proxy
docker build -t ronaldraygun/zai-proxy:VERSION .
docker push ronaldraygun/zai-proxy:VERSION

# Then point the manifest at it and push
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml   # image: ronaldraygun/zai-proxy:VERSION
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: point zai-proxy canary at VERSION image"
git push origin main
```

**Example Fix - Update Registry Credentials:**

`docker-hub-registry` is a SealedSecret in declarative-config
(`k8s/apexalgo-iad/kubernetes-reflector/docker-hub-registry-sealedsecret.yml`).
Re-seal the new credentials with kubeseal and commit the updated
SealedSecret — the same GitOps path as every other change. Do NOT
`kubectl create secret docker-registry ... | kubectl apply`: ArgoCD owns the
object and `selfHeal` will restore the sealed version.

---

### Scenario 7: Workers Can't Connect

**Symptoms:**
- Workers report connection refused
- No requests in canary logs
- Workers fall back to production

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Check canary service exists
kubectl --server=$S get svc zai-proxy-test -n mcp

# Check service endpoints
kubectl --server=$S get endpoints zai-proxy-test -n mcp

# Check service selector matches pods
kubectl --server=$S get svc zai-proxy-test -n mcp \
  -o jsonpath='{.spec.selector}' && echo

# Check pod labels
kubectl --server=$S get pods -n mcp -l app=zai-proxy,variant=test \
  -o jsonpath='{.items[0].metadata.labels}' && echo

# Check which endpoint workers are configured with (needle adapters)
grep -n "ANTHROPIC_BASE_URL" ~/.config/needle/adapters/*.yaml | head
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Service doesn't exist | Declare it in declarative-config |
| Selector mismatch | Fix service selector or pod labels (manifest) |
| No ready pods | Fix pod issues first |
| Network policy blocking | Update network policies (manifest) |
| DNS not resolving | Check CoreDNS |
| Workers pointed at the wrong URL | Update the needle adapter config |

**Example Fix - Selector Mismatch:**
```bash
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml

# Fix the Service selector to match pod labels:
#   selector:
#     app: zai-proxy
#     variant: test  # Ensure this matches pod labels
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "fix: correct zai-proxy canary service selector"
git push origin main
```

---

### Scenario 8: Prometheus Alerts Firing

**Symptoms:**
- Alerts visible in Grafana/AlertManager
- Alertmanager notifications sent
- Metrics show threshold violations

**Diagnosis:**
```bash
S=http://traefik-apexalgo-iad:8001

# Compare canary and production error behavior
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=200 | grep -iE "error|5[0-9][0-9]"
kubectl --server=$S logs -n mcp deployment/zai-proxy --tail=200 | grep -iE "error|5[0-9][0-9]"

# Production metrics directly (tailnet URL)
curl -sk https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | \
  grep -E "(zai_proxy_requests_total|zai_proxy_errors)"

# Check alert status in the Prometheus UI / AlertManager (tailnet VPN):
# https://prometheus-apexalgo-iad-ts.ardenone.com:8444
```

**Common Alerts & Fixes:**

| Alert | Meaning | Fix |
|-------|---------|-----|
| ZaiProxyCanaryDeploymentDown | Canary pods not ready | Fix pod health issues |
| ZaiProxyCanaryErrorRateHigherThanProduction | Canary error rate > production | Fix canary errors or stand it down |
| ZaiProxyCanaryHighErrorRate | Error rate >5% | Fix upstream issues |
| ZaiProxyCanaryLatencyDegraded | P95 latency >2x baseline | Optimize or increase resources (manifest) |

**Example Fix - Error Rate Higher Than Production:**
```bash
S=http://traefik-apexalgo-iad:8001

# If canary significantly worse, investigate logs
kubectl --server=$S logs -n mcp deployment/zai-proxy-test --tail=500 | grep -i error

# If canary issue, stand it down via its manifest — the sanctioned "emergency
# stop" is a Git commit, not kubectl scale:
cd /home/coding/declarative-config
$EDITOR k8s/apexalgo-iad/mcp/zai-proxy-test.yml   # replicas: 0
git add k8s/apexalgo-iad/mcp/zai-proxy-test.yml
git commit -m "chore: stand down zai-proxy canary (error rate)"
git push origin main
```

---

## Diagnostic Scripts

### Full Health Check Script

```bash
#!/bin/bash
# canary-health-check.sh - Comprehensive canary health check (read-only)

S=http://traefik-apexalgo-iad:8001
NAMESPACE="mcp"
DEPLOYMENT="zai-proxy-test"

echo "=== Canary Health Check ==="
echo

# 1. Pod Status
echo "1. Pod Status:"
kubectl --server=$S get pods -n $NAMESPACE -l app=zai-proxy,variant=test
echo

# 2. Recent Events
echo "2. Recent Events (last 5):"
kubectl --server=$S get events -n $NAMESPACE --field-selector involvedObject.name=$DEPLOYMENT \
  --sort-by='.lastTimestamp' | tail -5
echo

# 3. Error Rate (from logs; no exec available)
echo "3. Errors in recent logs:"
kubectl --server=$S logs -n $NAMESPACE deployment/$DEPLOYMENT --tail=200 | grep -ciE "error|5[0-9][0-9]" || echo "0"
echo

# 4. Token Counting
echo "4. Token Counting (log evidence):"
kubectl --server=$S logs -n $NAMESPACE deployment/$DEPLOYMENT --tail=100 | grep "Token usage" | tail -5
echo

# 5. Recent Errors in Logs
echo "5. Recent Errors (last 5):"
kubectl --server=$S logs -n $NAMESPACE deployment/$DEPLOYMENT --tail=100 | grep -i error | tail -5
echo

# 6. Resource Usage
echo "6. Resource Usage:"
kubectl --server=$S top pods -n $NAMESPACE -l app=zai-proxy,variant=test
echo

# 7. Production comparison (tailnet URL)
echo "7. Production health:"
curl -sk --max-time 10 https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/health | head -c 300
echo; echo

echo "=== Health Check Complete ==="
```

### Compare Canary vs Production

```bash
#!/bin/bash
# compare-variants.sh - Compare canary vs production (read-only)

S=http://traefik-apexalgo-iad:8001
NAMESPACE="mcp"

echo "=== Canary vs Production Comparison ==="
echo

# Production: direct /metrics over the tailnet
echo "Production (direct /metrics):"
curl -sk --max-time 10 https://zai-proxy-mcp-apexalgo-iad-ts.ardenone.com:8444/metrics | \
  grep -E "^zai_proxy_(requests|tokens)_total" | head -10
echo

# Canary: no VPN route — logs carry the same counters
echo "Canary (from logs):"
kubectl --server=$S logs -n $NAMESPACE deployment/zai-proxy-test --tail=200 | \
  grep -E "Token usage|zai_proxy" | tail -10
echo

# Pod-level comparison
echo "Pods:"
kubectl --server=$S get pods -n $NAMESPACE -l app=zai-proxy -o wide
echo

echo "=== Comparison Complete ==="
```

## Quick Reference Card

```
┌─────────────────────────────────────────────────────────────────┐
│                    CANARY TROUBLESHOOTING                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  STATUS          CHECK                     FIX (GitOps only)    │
│  ─────────────────────────────────────────────────────────────  │
│  CrashLoop      kubectl logs --previous   Fix app / rebuild     │
│  NotReady       kubectl describe pod      Fix probes / port     │
│  ImagePullBack  docker hub tags list      Push image / fix auth │
│  5xx Errors     kubectl logs | grep err   Fix rate limit / key  │
│  High Latency   kubectl top pods          Raise limits (Git)    │
│  No Metrics     logs / prod /metrics      Fix DEPLOYMENT_VARIANT│
│  No Conn        kubectl get endpoints     Fix service selector  │
│                                                                  │
│  Every fix = edit manifest in declarative-config, commit, push. │
│  kubectl set/scale/edit/patch/rollout are blocked by policy.    │
│                                                                  │
├─────────────────────────────────────────────────────────────────┤
│  EMERGENCY CANARY STAND-DOWN:                                   │
│  k8s/apexalgo-iad/mcp/zai-proxy-test.yml -> replicas: 0         │
│  git commit + git push origin main (ArgoCD does the rest)       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Related Documentation

- [CANARY_ROLLBACK_PROCEDURE.md](CANARY_ROLLBACK_PROCEDURE.md) - Rollback procedure
- [CANARY_PROMOTION_PROCEDURE.md](CANARY_PROMOTION_PROCEDURE.md) - Promotion procedure
- [CANARY_PROMOTION_CHECKLIST.md](CANARY_PROMOTION_CHECKLIST.md) - Promotion checklist
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) - General troubleshooting
- [MONITORING_SETUP.md](MONITORING_SETUP.md) - Metrics and alerting setup

## Getting Help

If issues persist after troubleshooting:

1. Check Grafana dashboard: `grafana-dashboard-zai-proxy-canary-comparison`
2. Review Prometheus alerts in AlertManager
3. Check recent deployment changes: `kubectl --server=http://traefik-apexalgo-iad:8001 rollout history deployment/zai-proxy-test -n mcp`
4. Contact on-call for zai-proxy service
5. Create a bead in this repository with diagnostic output:
   ```bash
   cd /home/coding/zai-proxy
   bead create --issue-type bug --priority 1 --label canary \
     --title "Canary issue: <one-line summary>"
   ```
