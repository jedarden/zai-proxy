# Canary Troubleshooting Guide

This guide covers common failure scenarios when testing canary deployments and how to diagnose and resolve them.

## Quick Diagnosis Checklist

When canary issues occur, run these commands first:

```bash
export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig

# 1. Check canary pod status
kubectl get pods -n mcp -l app=zai-proxy,variant=test

# 2. Check recent canary events
kubectl get events -n mcp --field-selector involvedObject.name=zai-proxy-test --sort-by='.lastTimestamp'

# 3. Stream canary logs
kubectl logs -f -n mcp deployment/zai-proxy-test

# 4. Check canary metrics
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | grep -E "zai_proxy_(requests|errors|tokens)"

# 5. Test canary health endpoint
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/health
```

## Failure Scenarios

### Scenario 1: Canary Pods CrashLoopBackOff

**Symptoms:**
- Pods show `CrashLoopBackOff` status
- Pod restarts repeatedly
- No logs or only startup logs visible

**Diagnosis:**
```bash
# Check pod status and restart count
kubectl get pods -n mcp -l app=zai-proxy,variant=test \
  -o custom-columns=NAME:.metadata.name,RESTARTS:.status.containerStatuses[0].restartCount,STATUS:.status.phase

# Describe pod to see events
kubectl describe pod -n mcp -l app=zai-proxy,variant=test

# Check previous container logs (if crash occurred)
kubectl logs -n mcp deployment/zai-proxy-test --previous

# Check termination reason
kubectl get pods -n mcp -l app=zai-proxy,variant=test \
  -o jsonpath='{.items[0].status.containerStatuses[0].lastState.terminated.reason}'
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Missing environment variable | Add missing env var to deployment manifest |
| Invalid API key | Update `zai-api-key` secret with valid key |
| Port conflict | Verify containerPort matches application port |
| Out of memory (OOMKilled) | Increase memory limits in deployment |
| Application panic/error | Fix code issue and rebuild image |

**Example Fix - Missing Environment Variable:**
```bash
# Check what's missing
kubectl describe pod -n mcp -l app=zai-proxy,variant=test | grep -A 10 "Environment"

# Edit deployment to add missing env var
kubectl edit deployment/zai-proxy-test -n mcp

# Add the missing variable to env: section
# Then rollout restart
kubectl rollout restart deployment/zai-proxy-test -n mcp
```

**Example Fix - OOMKilled:**
```bash
# Check if OOMKilled
kubectl get pods -n mcp -l app=zai-proxy,variant=test \
  -o jsonpath='{.items[0].status.containerStatuses[0].lastState.terminated.reason}'
# If output is "OOMKilled", increase memory limit

# Edit deployment
kubectl edit deployment/zai-proxy-test -n mcp

# Change resources:
#   limits:
#     memory: "128Mi"  # Increased from 64Mi
```

---

### Scenario 2: Canary Pods Not Ready

**Symptoms:**
- Pods show `Running` but `0/1` Ready
- Readiness probe failing
- Pod accepts no traffic

**Diagnosis:**
```bash
# Check readiness probe status
kubectl get pods -n mcp -l app=zai-proxy,variant=test \
  -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready

# Describe pod to see probe details
kubectl describe pod -n mcp -l app=zai-proxy,variant=test | grep -A 5 "Readiness"

# Check if health endpoint responds
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/health

# Check if application is listening on port
kubectl exec -n mcp deployment/zai-proxy-test -- \
  netstat -tlnp | grep 8080
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Health endpoint not responding | Fix /health endpoint in code |
| Port mismatch | Fix containerPort or application port |
| Slow startup (not ready before probe) | Increase initialDelaySeconds |
| Dependency not available | Fix external service connectivity |

**Example Fix - Slow Startup:**
```bash
# Edit deployment
kubectl edit deployment/zai-proxy-test -n mcp

# Adjust readiness probe:
#   readinessProbe:
#     httpGet:
#       path: /health
#       port: 8080
#     initialDelaySeconds: 10  # Increased from 3
#     periodSeconds: 5
```

**Example Fix - Port Mismatch:**
```bash
# Check what port app is listening on
kubectl exec -n mcp deployment/zai-proxy-test -- \
  netstat -tlnp

# If listening on 8081 but probe checking 8080:
kubectl edit deployment/zai-proxy-test -n mcp

# Change containerPort to match actual port:
#   ports:
#   - containerPort: 8081  # Corrected from 8080
```

---

### Scenario 3: High Error Rate (>5%)

**Symptoms:**
- 5xx errors increasing
- Workers report failures
- Prometheus alert firing

**Diagnosis:**
```bash
# Check error rate from metrics
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | \
  grep -E "zai_proxy_requests_total.*status=\"5" | sort -t'"' -k5

# Stream logs for errors
kubectl logs -f -n mcp deployment/zai-proxy-test | grep -i error

# Check for rate limiting (429 errors)
kubectl logs -n mcp deployment/zai-proxy-test --tail=100 | grep "429\|rate limit"

# Check z.ai API connectivity
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s -w "\n%{http_code}\n" -H "Authorization: Bearer $ZAI_API_KEY" \
  https://api.z.ai/v1/chat/completions -d '{"model":"glm-4.7","messages":[],"max_tokens":1}'
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Rate limiting from z.ai | Reduce RATE_LIMIT_INITIAL/MAX values |
| Invalid API key | Update zai-api-key secret |
| Upstream API changes | Check z.ai API status/docs |
| Token counting errors | Fix tokenizer logic |
| Request timeout errors | Increase client timeout settings |

**Example Fix - Rate Limiting:**
```bash
# Check current rate limit settings
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | grep rate_limit

# Edit deployment to reduce rate limits
kubectl edit deployment/zai-proxy-test -n mcp

# Adjust rate limit env vars:
#   env:
#   - name: RATE_LIMIT_INITIAL
#     value: "1"  # Reduced from 2
#   - name: RATE_LIMIT_MAX
#     value: "3"  # Reduced from 5

# Rollout restart to apply
kubectl rollout restart deployment/zai-proxy-test -n mcp
```

**Example Fix - Invalid API Key:**
```bash
# Test if API key works
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s https://api.z.ai/v1/models -H "Authorization: Bearer $ZAI_API_KEY"

# If returns 401, update the secret
kubectl create secret generic zai-api-key -n mcp \
  --from-literal=api-key='NEW_API_KEY_HERE' \
  --dry-run=client -o yaml | kubectl apply -f -

# Rollout restart to pick up new key
kubectl rollout restart deployment/zai-proxy-test -n mcp
```

---

### Scenario 4: Token Counting Broken

**Symptoms:**
- No token metrics in /metrics
- Logs show "Token counting failed"
- Token values are zero or incorrect

**Diagnosis:**
```bash
# Check for token metrics
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | grep zai_proxy_tokens

# Check logs for token errors
kubectl logs -n mcp deployment/zai-proxy-test --tail=100 | grep -i "token"

# Check if DEPLOYMENT_VARIANT is set
kubectl exec -n mcp deployment/zai-proxy-test -- \
  printenv DEPLOYMENT_VARIANT

# Verify tokenizer is working
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | grep token_count_duration
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| DEPLOYMENT_VARIANT not set | Add DEPLOYMENT_VARIANT=canary to env |
| Tokenizer initialization failed | Fix tokenizer code and rebuild |
| Tokenizer config invalid | Update tokenizer configuration |
| Memory pressure (tokenizer OOM) | Increase memory limits |

**Example Fix - Missing DEPLOYMENT_VARIANT:**
```bash
# Check if env var is set
kubectl exec -n mcp deployment/zai-proxy-test -- printenv DEPLOYMENT_VARIANT

# If not set, edit deployment
kubectl edit deployment/zai-proxy-test -n mcp

# Add to env section:
#   - name: DEPLOYMENT_VARIANT
#     value: "canary"
```

---

### Scenario 5: Latency Degradation

**Symptoms:**
- P95 latency >2x baseline
- Slow response times
- Prometheus latency alert firing

**Diagnosis:**
```bash
# Check request duration metrics
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | \
  grep zai_proxy_request_duration_seconds | grep -v "0\." | sort -t'"' -k5

# Check token counting duration
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | \
  grep token_count_duration_seconds | sort -t'"' -k5

# Check resource usage
kubectl top pods -n mcp -l app=zai-proxy,variant=test

# Check for high CPU throttling
kubectl describe pod -n mcp -l app=zai-proxy,variant=test | grep -A 3 "Limits"
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| CPU throttling | Increase CPU limits |
| Token counting slow | Optimize tokenizer or increase resources |
| Upstream API slow | Check z.ai API status |
| Network latency | Check cluster network issues |
| Memory pressure (GC) | Increase memory limits |

**Example Fix - CPU Throttling:**
```bash
# Check current CPU limits
kubectl get deployment/zai-proxy-test -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}'

# If low (e.g., 100m), increase it
kubectl edit deployment/zai-proxy-test -n mcp

# Change:
#   resources:
#     limits:
#       cpu: "200m"  # Increased from 100m
```

---

### Scenario 6: Image Pull Errors

**Symptoms:**
- Pods stuck in `ImagePullBackOff` or `ErrImagePull`
- Pod events show "Failed to pull image"

**Diagnosis:**
```bash
# Check pod events for pull errors
kubectl describe pod -n mcp -l app=zai-proxy,variant=test | grep -A 5 "Events:"

# Check what image is being pulled
kubectl get deployment/zai-proxy-test -n mcp \
  -o jsonpath='{.spec.template.spec.containers[0].image}' && echo

# Verify image exists (from devpod)
curl -s https://hub.docker.com/v2/repositories/ronaldraygun/zai-proxy/tags/ | \
  jq '.results[].name' | grep VERSION

# Check if image pull secret exists
kubectl get secret docker-hub-registry -n mcp
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Image tag doesn't exist | Build and push image with correct tag |
| Wrong registry/username | Fix image name in deployment |
| Expired registry credentials | Update docker-hub-registry secret |
| Registry rate limiting | Wait or use authenticated pulls |
| Network issue | Check cluster egress to Docker Hub |

**Example Fix - Image Tag Doesn't Exist:**
```bash
# List available tags
curl -s https://hub.docker.com/v2/repositories/ronaldraygun/zai-proxy/tags/ | \
  jq '.results[].name'

# If tag doesn't exist, build it
cd /home/coder/ardenone-cluster/containers/zai-proxy
docker build -t ronaldraygun/zai-proxy:VERSION .
docker push ronaldraygun/zai-proxy:VERSION

# Or update deployment to use existing tag
kubectl set image deployment/zai-proxy-test \
  proxy=ronaldraygun/zai-proxy:EXISTING_TAG -n mcp
```

**Example Fix - Update Credentials:**
```bash
# Create new docker-registry secret
kubectl create secret docker-registry docker-hub-registry -n mcp \
  --docker-server=https://index.docker.io/v1/ \
  --docker-username=USERNAME \
  --docker-password=PASSWORD \
  --docker-email=EMAIL \
  --dry-run=client -o yaml | kubectl apply -f -

# Rollout restart to use new secret
kubectl rollout restart deployment/zai-proxy-test -n mcp
```

---

### Scenario 7: Workers Can't Connect

**Symptoms:**
- Workers report connection refused
- No requests in canary logs
- Workers fall back to production

**Diagnosis:**
```bash
# Check canary service exists
kubectl get svc zai-proxy-test -n mcp

# Check service endpoints
kubectl get endpoints zai-proxy-test -n mcp

# Test from devpod
curl -s http://zai-proxy-test.mcp.svc.cluster.local:8080/health

# Check service selector matches pods
kubectl get svc zai-proxy-test -n mcp \
  -o jsonpath='{.spec.selector}' && echo

# Check pod labels
kubectl get pods -n mcp -l app=zai-proxy,variant=test \
  -o jsonpath='{.items[0].metadata.labels}' && echo
```

**Common Causes & Fixes:**

| Cause | Fix |
|-------|-----|
| Service doesn't exist | Create service |
| Selector mismatch | Fix service selector or pod labels |
| No ready pods | Fix pod issues first |
| Network policy blocking | Update network policies |
| DNS not resolving | Check CoreDNS |

**Example Fix - Selector Mismatch:**
```bash
# Check service selector
kubectl get svc zai-proxy-test -n mcp -o jsonpath='{.spec.selector}'

# Check pod labels
kubectl get pods -n mcp -l app=zai-proxy,variant=test \
  -o jsonpath='{.items[0].metadata.labels}'

# If mismatch, edit service
kubectl edit svc zai-proxy-test -n mcp

# Fix selector to match pod labels:
#   selector:
#     app: zai-proxy
#     variant: test  # Ensure this matches pod labels
```

---

### Scenario 8: Prometheus Alerts Firing

**Symptoms:**
- Alerts visible in Grafana/AlertManager
- Alertmanager notifications sent
- Metrics show threshold violations

**Diagnosis:**
```bash
# Check current alert values
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | \
  grep -E "(zai_proxy_requests_total|zai_proxy_errors)"

# Compare with production
kubectl exec -n mcp deployment/zai-proxy -- \
  curl -s http://localhost:8080/metrics | \
  grep -E "(zai_proxy_requests_total|zai_proxy_errors)"

# Check alert rules
kubectl get prometheusrule -n mcp zai-proxy-canary -o yaml

# Check alert status in Prometheus
# (Access Prometheus UI and check alerts)
```

**Common Alerts & Fixes:**

| Alert | Meaning | Fix |
|-------|---------|-----|
| ZaiProxyCanaryDeploymentDown | Canary pods not ready | Fix pod health issues |
| ZaiProxyCanaryErrorRateHigherThanProduction | Canary error rate > production | Fix canary errors or rollback |
| ZaiProxyCanaryHighErrorRate | Error rate >5% | Fix upstream issues |
| ZaiProxyCanaryLatencyDegraded | P95 latency >2x baseline | Optimize or increase resources |

**Example Fix - Error Rate Higher Than Production:**
```bash
# Check canary error rate
kubectl exec -n mcp deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics | grep error

# Check production error rate
kubectl exec -n mcp deployment/zai-proxy -- \
  curl -s http://localhost:8080/metrics | grep error

# If canary significantly worse, investigate logs
kubectl logs -n mcp deployment/zai-proxy-test --tail=500 | grep -i error

# If canary issue, rollback to production
kubectl scale deployment/zai-proxy-test -n mcp --replicas=0
```

---

## Diagnostic Scripts

### Full Health Check Script

```bash
#!/bin/bash
# canary-health-check.sh - Comprehensive canary health check

export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig
NAMESPACE="mcp"
DEPLOYMENT="zai-proxy-test"

echo "=== Canary Health Check ==="
echo

# 1. Pod Status
echo "1. Pod Status:"
kubectl get pods -n $NAMESPACE -l app=zai-proxy,variant=test
echo

# 2. Recent Events
echo "2. Recent Events (last 5):"
kubectl get events -n $NAMESPACE --field-selector involvedObject.name=$DEPLOYMENT \
  --sort-by='.lastTimestamp' | tail -5
echo

# 3. Error Rate
echo "3. Error Rate (last 5 min):"
ERROR_RATE=$(kubectl exec -n $NAMESPACE deployment/$DEPLOYMENT -- \
  curl -s http://localhost:8080/metrics | \
  grep 'zai_proxy_requests_total.*status="5"' | awk '{sum+=$2} END {print sum+0}')
TOTAL_RATE=$(kubectl exec -n $NAMESPACE deployment/$DEPLOYMENT -- \
  curl -s http://localhost:8080/metrics | \
  grep 'zai_proxy_requests_total' | awk '{sum+=$2} END {print sum+0}')
if [ "$TOTAL_RATE" -gt 0 ]; then
  echo "Error rate: $(echo "scale=2; $ERROR_RATE * 100 / $TOTAL_RATE" | bc)%"
else
  echo "No requests recorded"
fi
echo

# 4. Token Counting
echo "4. Token Counting:"
TOKEN_METRICS=$(kubectl exec -n $NAMESPACE deployment/$DEPLOYMENT -- \
  curl -s http://localhost:8080/metrics | grep zai_proxy_tokens_total)
if [ -n "$TOKEN_METRICS" ]; then
  echo "$TOKEN_METRICS"
else
  echo "WARNING: No token metrics found"
fi
echo

# 5. Recent Errors in Logs
echo "5. Recent Errors (last 10):"
kubectl logs -n $NAMESPACE deployment/$DEPLOYMENT --tail=100 | grep -i error | tail -5
echo

# 6. Resource Usage
echo "6. Resource Usage:"
kubectl top pods -n $NAMESPACE -l app=zai-proxy,variant=test
echo

echo "=== Health Check Complete ==="
```

### Compare Canary vs Production

```bash
#!/bin/bash
# compare-variants.sh - Compare canary vs production metrics

export KUBECONFIG=/home/coder/.kube/apexalgo-iad.kubeconfig
NAMESPACE="mcp"

echo "=== Canary vs Production Comparison ==="
echo

# Get metrics from both
CANARY_METRICS=$(kubectl exec -n $NAMESPACE deployment/zai-proxy-test -- \
  curl -s http://localhost:8080/metrics)
PROD_METRICS=$(kubectl exec -n $NAMESPACE deployment/zai-proxy -- \
  curl -s http://localhost:8080/metrics)

# Compare error rates
echo "Error Rates:"
CANARY_ERR=$(echo "$CANARY_METRICS" | grep 'zai_proxy_requests_total.*status="5"' | awk '{sum+=$2} END {print sum+0}')
PROD_ERR=$(echo "$PROD_METRICS" | grep 'zai_proxy_requests_total.*status="5"' | awk '{sum+=$2} END {print sum+0}')
echo "  Canary:   $CANARY_ERR errors"
echo "  Production: $PROD_ERR errors"
echo

# Compare P95 latency
echo "P95 Latency:"
CANARY_P95=$(echo "$CANARY_METRICS" | grep 'quantile="0.95"' | grep request_duration | awk '{print $2}')
PROD_P95=$(echo "$PROD_METRICS" | grep 'quantile="0.95"' | grep request_duration | awk '{print $2}')
echo "  Canary:   ${CANARY_P95}s"
echo "  Production: ${PROD_P95}s"
echo

# Compare token counting
echo "Token Counting:"
CANARY_TOK=$(echo "$CANARY_METRICS" | grep 'zai_proxy_tokens_total' | awk '{sum+=$2} END {print sum+0}')
PROD_TOK=$(echo "$PROD_METRICS" | grep 'zai_proxy_tokens_total' | awk '{sum+=$2} END {print sum+0}')
echo "  Canary:   $CANARY_TOK tokens"
echo "  Production: $PROD_TOK tokens"
echo

echo "=== Comparison Complete ==="
```

## Quick Reference Card

```
┌─────────────────────────────────────────────────────────────────┐
│                    CANARY TROUBLESHOOTING                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  STATUS          CHECK                    FIX                    │
│  ─────────────────────────────────────────────────────────────  │
│  CrashLoop      kubectl logs --previous   Fix app / rebuild     │
│  NotReady       kubectl describe pod      Fix probes / port     │
│  ImagePullBack  kubectl get secret        Push image / fix auth │
│  5xx Errors     kubectl logs | grep error  Fix rate limit / key │
│  High Latency   kubectl top pods          Increase CPU/mem      │
│  No Metrics     kubectl exec -- curl /m   Fix DEPLOYMENT_VARIANT│
│  No Conn        kubectl get endpoints     Fix service selector  │
│                                                                  │
├─────────────────────────────────────────────────────────────────┤
│  EMERGENCY ROLLBACK:                                            │
│  kubectl scale deployment/zai-proxy-test -n mcp --replicas=0    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Related Documentation

- [CANARY_ROLLBACK_PROCEDURE.md](CANARY_ROLLBACK_PROCEDURE.md) - Rollback procedure
- [CANARY_PROMOTION_PROCEDURE.md](CANARY_PROMOTION_PROCEDURE.md) - Promotion procedure
- [CANARY_PROMOTION_CHECKLIST.md](CANARY_PROMOTION_CHECKLIST.md) - Promotion checklist
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) - General troubleshooting

## Getting Help

If issues persist after troubleshooting:

1. Check Grafana dashboard: `grafana-dashboard-zai-proxy-canary-comparison`
2. Review Prometheus alerts in AlertManager
3. Check recent deployment changes: `kubectl rollout history deployment/zai-proxy-test -n mcp`
4. Contact on-call for zai-proxy service
5. Create issue in repository with diagnostic output
