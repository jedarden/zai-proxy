# Token Counting Troubleshooting Guide

Quick reference for diagnosing and fixing token counting issues.

## Table of Contents

1. [Quick Diagnostics](#quick-diagnostics)
2. [Common Issues](#common-issues)
3. [Advanced Debugging](#advanced-debugging)

---

## Quick Diagnostics

### 1. Check if Token Counting is Enabled

**Command:**
```bash
kubectl logs -n mcp deployment/zai-proxy --tail=100 | grep -i "token counting"
```

**Expected Output (enabled):**
```
Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)
```

**Expected Output (disabled):**
```
Token counting disabled (TOKEN_COUNTING_ENABLED=false)
```

**Expected Output (fallback mode):**
```
Warning: Failed to initialize TikToken counter: <error>
Falling back to SimpleTokenCounter
Token counting enabled (fallback mode, model: glm-4)
```

### 2. Check Token Metrics Availability

**Command:**
```bash
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_tokens_total
```

**Expected Output:**
```
zai_proxy_tokens_total{direction="input",model="glm-4"} 1234
zai_proxy_tokens_total{direction="output",model="glm-4"} 5678
```

**If no output:** Token counting is disabled or no requests have been processed yet.

### 3. Check Configuration

**Command:**
```bash
kubectl get deployment zai-proxy -n mcp -o jsonpath='{.spec.template.spec.containers[0].env}' | jq
```

**Look for:**
```json
[
  {
    "name": "TOKEN_COUNTING_ENABLED",
    "value": "true"
  },
  {
    "name": "TOKENIZER_MODEL",
    "value": "glm-4"
  }
]
```

### 4. Live Token Usage Monitoring

**Command:**
```bash
kubectl logs -f -n mcp deployment/zai-proxy | grep "Token usage"
```

**Expected Output (per request):**
```
Token usage: input=123, output=456
```

---

## Common Issues

### Issue 1: No Token Metrics in Prometheus

**Symptoms:**
- `zai_proxy_tokens_total` metric is missing
- No "Token usage" logs

**Diagnosis:**
```bash
# Step 1: Check if token counting is enabled
kubectl logs -n mcp deployment/zai-proxy --tail=50 | grep "Token counting"

# Step 2: Check environment variable
kubectl get deployment zai-proxy -n mcp -o yaml | grep -A 2 TOKEN_COUNTING_ENABLED
```

**Solution:**
```bash
# Enable token counting
kubectl set env deployment/zai-proxy -n mcp TOKEN_COUNTING_ENABLED=true

# Restart deployment
kubectl rollout restart deployment/zai-proxy -n mcp

# Wait for rollout
kubectl rollout status deployment/zai-proxy -n mcp

# Verify
kubectl logs -n mcp deployment/zai-proxy --tail=10 | grep "Token counting"
```

---

### Issue 2: Inaccurate Token Counts (>10% variance)

**Symptoms:**
- Token counts significantly differ from expected
- Large variance compared to Anthropic's counts

**Diagnosis:**
```bash
# Check if fallback tokenizer is active
kubectl logs -n mcp deployment/zai-proxy --tail=100 | grep -i fallback
```

**If fallback mode detected:**
```
Falling back to SimpleTokenCounter
Token counting enabled (fallback mode, model: glm-4)
```

**Root Cause:** Tiktoken initialization failed. Fallback uses word count approximation (~30% variance).

**Solution:**

**Option 1: Rebuild with tiktoken dependencies**
```bash
cd /home/coder/ardenone-cluster/containers/zai-proxy

# Check tiktoken dependency
go list -m github.com/tiktoken-go/tokenizer

# If missing, download
go mod download

# Rebuild
go build -o zai-proxy main.go tokenizer.go

# Test locally
./zai-proxy
# Should see: "Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)"
```

**Option 2: Rebuild Docker image**
```bash
cd /home/coder/ardenone-cluster/containers/zai-proxy

# Rebuild image
docker build -t zai-proxy:latest .

# Push to registry
docker tag zai-proxy:latest ghcr.io/ardenone/zai-proxy:latest
docker push ghcr.io/ardenone/zai-proxy:latest

# Update deployment
kubectl rollout restart deployment/zai-proxy -n mcp
```

**Verification:**
```bash
kubectl logs -n mcp deployment/zai-proxy --tail=20 | grep "Token counting"
# Should see: "Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)"
# Should NOT see: "fallback mode"
```

---

### Issue 3: High Token Counting Latency (>5ms)

**Symptoms:**
- Token counting is slow
- `zai_proxy_token_count_duration_seconds` > 5ms

**Diagnosis:**
```promql
# Query 99th percentile latency
histogram_quantile(0.99, rate(zai_proxy_token_count_duration_seconds_bucket[5m]))
```

**Expected:** <1ms for 99th percentile
**Problem threshold:** >5ms

**Root Causes:**

1. **High CPU usage**
   ```bash
   kubectl top pod -n mcp -l app=zai-proxy
   ```

   **If CPU is throttled (near limits):**
   ```yaml
   # Increase CPU limits
   resources:
     limits:
       cpu: 2000m  # Increase from 1000m
   ```

2. **High concurrent requests (mutex contention)**
   ```promql
   # Check concurrent requests
   zai_proxy_concurrent_requests
   ```

   **If >50 concurrent requests:**
   ```bash
   # Increase MAX_WORKERS
   kubectl set env deployment/zai-proxy -n mcp MAX_WORKERS=100
   ```

3. **Large response bodies**
   - Token counting processes entire response
   - Large outputs (>10K tokens) may take longer

   **Workaround:** Disable token counting for specific endpoints if needed

**Verification:**
```promql
# Check latency after changes
histogram_quantile(0.99, rate(zai_proxy_token_count_duration_seconds_bucket[5m]))
```

---

### Issue 4: Token Usage Not in Response Body

**Expected Behavior:** Token counts are **NOT** included in response bodies (feature not implemented yet).

**Current Behavior:**
- ✅ Token counts logged: `Token usage: input=X, output=Y`
- ✅ Prometheus metrics: `zai_proxy_tokens_total`
- ❌ **No** `usage` field in response JSON/SSE

**Workaround 1: Use Logs**
```bash
# Monitor token usage in real-time
kubectl logs -f -n mcp deployment/zai-proxy | grep "Token usage"
```

**Workaround 2: Use Prometheus**
```promql
# Total tokens per request (approximation)
rate(zai_proxy_tokens_total[5m]) / rate(zai_proxy_requests_total[5m])
```

**Future Enhancement:** Usage injection planned (see `InjectTokenUsage()` in `tokenizer.go`)

---

### Issue 5: Metrics Not Reset After Deployment

**Symptoms:**
- Token counts seem to persist across pod restarts
- Metrics show large cumulative numbers

**Explanation:** Prometheus metrics are cumulative counters (lifetime totals).

**Expected Behavior:**
- Counters reset to 0 when pod restarts
- Use `rate()` or `increase()` for meaningful queries

**Incorrect Query:**
```promql
# DON'T: Raw counter (lifetime total since pod start)
zai_proxy_tokens_total
```

**Correct Queries:**
```promql
# Tokens per minute (rolling 5m window)
rate(zai_proxy_tokens_total[5m]) * 60

# Total tokens in last hour
increase(zai_proxy_tokens_total[1h])

# Tokens per request
rate(zai_proxy_tokens_total[5m]) / rate(zai_proxy_requests_total[5m])
```

---

## Advanced Debugging

### Enable Debug Logging

**Modify deployment:**
```yaml
env:
- name: LOG_LEVEL
  value: "debug"  # Enable debug logs
```

**Apply:**
```bash
kubectl apply -f deployment.yaml
kubectl rollout status deployment/zai-proxy -n mcp
```

**Watch logs:**
```bash
kubectl logs -f -n mcp deployment/zai-proxy
```

### Inspect Request/Response Bodies

**Add temporary debug logging in code:**

```go
// In main.go, after CountRequestTokens()
log.Printf("DEBUG: Request body: %s", string(requestBody))
log.Printf("DEBUG: Input tokens: %d", inputTokens)

// After CountOutputTokens()
log.Printf("DEBUG: Response body: %s", string(bodyCapture.GetCapturedContent()))
log.Printf("DEBUG: Output tokens: %d", outputTokens)
```

**Rebuild and deploy:**
```bash
go build -o zai-proxy main.go tokenizer.go
docker build -t zai-proxy:debug .
```

⚠️ **Warning:** Only use in development. Large logs will impact performance.

### Test Token Counting Locally

**Create test request:**
```bash
cat > test_request.json <<EOF
{
  "model": "claude-3-sonnet",
  "messages": [
    {"role": "user", "content": "What is 2+2?"}
  ],
  "max_tokens": 50
}
EOF
```

**Run proxy locally:**
```bash
export ZAI_API_KEY="your-api-key"
export TOKEN_COUNTING_ENABLED=true
go run main.go tokenizer.go
```

**Send test request:**
```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: $ZAI_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d @test_request.json
```

**Check logs:**
```
Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)
Z.AI proxy listening on :8080
Token usage: input=8, output=5
```

### Verify Tiktoken Installation

**Check dependencies:**
```bash
cd /home/coder/ardenone-cluster/containers/zai-proxy

# List tiktoken dependency
go list -m github.com/tiktoken-go/tokenizer

# Expected output:
# github.com/tiktoken-go/tokenizer v0.2.0
```

**Test tiktoken directly:**
```bash
cat > test_tiktoken.go <<EOF
package main

import (
    "fmt"
    "github.com/tiktoken-go/tokenizer"
)

func main() {
    enc, err := tokenizer.Get(tokenizer.Cl100kBase)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    text := "Hello, world!"
    ids, _, _ := enc.Encode(text)
    fmt.Printf("Text: %s\n", text)
    fmt.Printf("Tokens: %d\n", len(ids))
}
EOF

go run test_tiktoken.go

# Expected output:
# Text: Hello, world!
# Tokens: 4
```

### Compare Token Counts with Anthropic

**Send same request to both proxies:**
```bash
# Request via zai-proxy
curl -X POST http://zai-proxy:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: $ZAI_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-3-sonnet","messages":[{"role":"user","content":"Hello"}],"max_tokens":10}' \
  -o zai_response.json

# Request directly to Anthropic (if you have API key)
curl -X POST https://api.anthropic.com/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-3-sonnet-20240229","messages":[{"role":"user","content":"Hello"}],"max_tokens":10}' \
  -o anthropic_response.json

# Compare usage fields
cat anthropic_response.json | jq '.usage'
# zai-proxy logs: Token usage: input=X, output=Y
```

**Expected variance:** <3% for Claude models, <10% for GLM-4

---

## Diagnostic Checklist

Use this checklist when troubleshooting token counting issues:

- [ ] Token counting enabled in environment variables
- [ ] Startup logs show "tiktoken cl100k_base encoding" (not "fallback mode")
- [ ] Prometheus metrics `zai_proxy_tokens_total` are present
- [ ] "Token usage" logs appear for each request
- [ ] Token counting latency <1ms (99th percentile)
- [ ] Token counts within expected variance (<10%)
- [ ] CPU usage not throttled
- [ ] Sufficient MAX_WORKERS for concurrent requests

---

## Getting Help

If you've tried all troubleshooting steps and still have issues:

1. **Collect diagnostic information:**
   ```bash
   # Startup logs
   kubectl logs -n mcp deployment/zai-proxy --tail=100 > startup_logs.txt

   # Recent request logs
   kubectl logs -n mcp deployment/zai-proxy --tail=500 | grep "Token usage" > token_logs.txt

   # Configuration
   kubectl get deployment zai-proxy -n mcp -o yaml > deployment.yaml

   # Metrics snapshot
   curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_token > metrics.txt
   ```

2. **Check documentation:**
   - [TOKENIZATION.md](./TOKENIZATION.md) - Comprehensive guide
   - [ENVIRONMENT_VARIABLES.md](./ENVIRONMENT_VARIABLES.md) - Configuration reference

3. **File an issue:**
   - Include diagnostic files above
   - Describe expected vs actual behavior
   - Include relevant logs and metrics

---

## Quick Reference Commands

**Enable token counting:**
```bash
kubectl set env deployment/zai-proxy -n mcp TOKEN_COUNTING_ENABLED=true
kubectl rollout restart deployment/zai-proxy -n mcp
```

**Disable token counting:**
```bash
kubectl set env deployment/zai-proxy -n mcp TOKEN_COUNTING_ENABLED=false
kubectl rollout restart deployment/zai-proxy -n mcp
```

**Watch token usage logs:**
```bash
kubectl logs -f -n mcp deployment/zai-proxy | grep "Token usage"
```

**Query token metrics:**
```bash
curl -s http://zai-proxy.mcp.svc.cluster.local:8080/metrics | grep zai_proxy_tokens
```

**Check latency:**
```promql
histogram_quantile(0.99, rate(zai_proxy_token_count_duration_seconds_bucket[5m]))
```

**Restart proxy:**
```bash
kubectl rollout restart deployment/zai-proxy -n mcp
kubectl rollout status deployment/zai-proxy -n mcp
```
