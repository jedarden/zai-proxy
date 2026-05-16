# Z.AI Rate Limit Testing & Validation

## Claimed Rate Limit: 2400 requests / 5 hours

**Effective rate**: 2400 / (5 × 60 × 60) = 2400 / 18000 = **0.133 requests/second**

This is quite restrictive! Let's validate and optimize.

## Testing Strategy

### Phase 1: Validate Actual Limits (Conservative)

Start with **very conservative** settings to avoid account suspension:

```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "0.1"    # Start at 0.1 req/s (360/hour, well below limit)
  - name: RATE_LIMIT_MIN
    value: "0.05"   # Minimum 0.05 req/s (180/hour)
  - name: RATE_LIMIT_MAX
    value: "0.2"    # Max 0.2 req/s (720/hour)
  - name: MAX_RETRIES
    value: "5"      # More retries to handle bursts
```

**Expected behavior:**
- Should run smoothly with no 429s
- If we get 429s at 0.1 req/s, the limit is real
- If no 429s, we can gradually increase

### Phase 2: Find True Limit (Incremental Testing)

Run a **load test** with incrementally increasing rates:

```bash
# Create a test script
cat > /tmp/test-zai-limits.sh << 'EOF'
#!/bin/bash

PROXY_URL="http://zai-proxy.devpod.svc.cluster.local:8080"
TEST_DURATION=300  # 5 minutes per test

for rate in 0.1 0.15 0.2 0.3 0.5 1.0; do
  echo "========================================"
  echo "Testing rate: $rate req/s"
  echo "========================================"

  # Use Apache Bench (ab) or similar
  total_requests=$(echo "$rate * $TEST_DURATION" | bc)
  concurrency=$(echo "$rate * 2" | bc | cut -d. -f1)
  concurrency=${concurrency:-1}

  echo "Total requests: $total_requests"
  echo "Concurrency: $concurrency"

  # Run test
  ab -n $total_requests -c $concurrency \
    -H "Content-Type: application/json" \
    -p /tmp/test-payload.json \
    "$PROXY_URL/v1/messages" | tee /tmp/test-rate-$rate.log

  # Check for 429s
  count_429=$(grep -c "429" /tmp/test-rate-$rate.log || echo 0)

  echo "429 errors: $count_429"

  if [ $count_429 -gt 0 ]; then
    echo "❌ Hit rate limit at $rate req/s"
    echo "True limit is between previous rate and $rate"
    break
  else
    echo "✅ No rate limit at $rate req/s"
  fi

  # Wait between tests
  echo "Waiting 60s before next test..."
  sleep 60
done
EOF

chmod +x /tmp/test-zai-limits.sh
```

### Phase 3: Monitor with Prometheus

While testing, monitor these queries:

```promql
# Current rate limit setting
zai_proxy_rate_limit_requests_per_second

# 429 error rate
sum(rate(zai_proxy_requests_total{status_code="429"}[5m]))

# Success rate
sum(rate(zai_proxy_requests_total{status_code=~"2.."}[5m]))

# Rate adjustments
rate(zai_proxy_rate_limit_adjustments_total[5m])

# Retry attempts due to 429
rate(zai_proxy_retry_attempts_total{reason="429"}[5m])
```

## Recommended Configuration

Based on **2400 requests / 5 hours = 0.133 req/s**:

### Conservative (Safe Production)

Stay **well below** the limit with headroom:

```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "0.08"   # 60% of limit (288/hour, 1728/5h)
  - name: RATE_LIMIT_MIN
    value: "0.05"   # 38% of limit (180/hour)
  - name: RATE_LIMIT_MAX
    value: "0.12"   # 90% of limit (432/hour, 2160/5h)
  - name: MAX_RETRIES
    value: "5"
```

**Rationale:**
- **Initial 0.08**: Start at 60% to allow burst traffic
- **Min 0.05**: Safety floor in case of aggressive backoff
- **Max 0.12**: Cap at 90% to avoid hitting limit during bursts
- **Retries 5**: More attempts to handle transient issues

### Aggressive (Maximum Utilization)

Push closer to the limit (risky, may hit 429s):

```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "0.12"   # 90% of limit
  - name: RATE_LIMIT_MIN
    value: "0.08"   # 60% of limit
  - name: RATE_LIMIT_MAX
    value: "0.13"   # 98% of limit (2340/5h)
  - name: MAX_RETRIES
    value: "7"      # More retries needed
```

**Rationale:**
- **Initial 0.12**: Start near limit
- **Min 0.08**: Still have room to decrease
- **Max 0.13**: Push to 98% of claimed limit
- **Retries 7**: Handle bursts with more retries

## Burst Handling

The token bucket allows **bursts up to 2x the rate**:

```
Rate: 0.1 req/s
Burst: 0.2 tokens (allows 2 requests instantly)
```

This means you can handle:
- **Single spike**: 2 requests instantly
- **Sustained spike**: 0.2 req/s for burst duration
- **Then throttled**: Back to 0.1 req/s

Example burst scenario:
```
Time 0s:   Send 2 requests instantly (use burst tokens)
Time 0s:   Bucket now empty, wait for refill
Time 10s:  Bucket has 1 token (0.1 × 10s)
Time 10s:  Send 1 request
Time 20s:  Bucket has 1 token again
```

## Scaling with Multiple Replicas

If you have **N replicas**, each gets the **full rate limit**:

```yaml
# With 3 replicas:
spec:
  replicas: 3

env:
  - name: RATE_LIMIT_MAX
    value: "0.04"   # 0.04 × 3 = 0.12 total
```

**But this assumes:**
- Load balancer distributes evenly
- No single replica hits limit
- Total cluster rate = N × per-replica rate

**Reality:**
- Uneven distribution can cause one replica to hit limit
- Better to set **per-replica rate = total_limit / (N × 1.5)** for safety

Example with 3 replicas:
```
Total limit: 0.133 req/s
Per-replica: 0.133 / (3 × 1.5) = 0.03 req/s
```

## Monitoring Dashboard

Create a Grafana panel to track rate limit efficiency:

```promql
# Requests per second (actual)
sum(rate(zai_proxy_requests_total[5m]))

# Vs. configured limit
zai_proxy_rate_limit_requests_per_second

# Efficiency ratio (want ~0.9)
sum(rate(zai_proxy_requests_total{status_code=~"2.."}[5m]))
/
zai_proxy_rate_limit_requests_per_second
```

**Target efficiency: 85-95%**
- Below 85%: You're under-utilizing (can increase rate)
- Above 95%: Risk of hitting limits (decrease rate)

## Testing Scenarios

### Scenario 1: Validate 2400/5h Limit

```bash
# Send exactly 2400 requests over 5 hours
# Rate: 0.133 req/s

# Set rate limit to exactly the claimed limit
kubectl set env deployment/zai-proxy -n devpod \
  RATE_LIMIT_INITIAL=0.133 \
  RATE_LIMIT_MIN=0.133 \
  RATE_LIMIT_MAX=0.133

# Monitor for 5 hours
# If we get 429s, the limit is real
# If no 429s, we can go higher
```

### Scenario 2: Find Burst Capacity

```bash
# Send requests as fast as possible for 10 seconds
# See how many succeed before 429

for i in {1..100}; do
  curl -X POST "http://zai-proxy:8080/v1/messages" \
    -H "Content-Type: application/json" \
    -d '{"model":"glm-4.7","messages":[{"role":"user","content":"test"}]}' &
done
wait

# Check logs for 429s
kubectl logs -n devpod deployment/zai-proxy | grep -c "429"
```

### Scenario 3: Sustained Load Test

```bash
# Use wrk or ab for sustained load
wrk -t4 -c10 -d300s --rate 0.15 \
  -s /tmp/post-script.lua \
  http://zai-proxy:8080/v1/messages

# Monitor:
# - Does rate limiter adapt?
# - Are 429s absorbed by retries?
# - Does success rate stay high?
```

## Expected Results

### If limit is 2400/5h (0.133 req/s):

| Setting | Expected Behavior |
|---------|-------------------|
| Rate = 0.1 | ✅ No 429s, smooth operation |
| Rate = 0.13 | ⚠️ Occasional 429s, absorbed by retries |
| Rate = 0.15 | ❌ Frequent 429s, some reach client |
| Rate = 0.2 | ❌ Constant 429s, rate decreases to min |

### If limit is actually higher:

| True Limit | Safe Max Rate | Notes |
|------------|---------------|-------|
| 5000/5h | 0.25 req/s | Common tier |
| 10000/5h | 0.5 req/s | Pro tier |
| 50000/5h | 2.5 req/s | Enterprise |

## Automation: Self-Tuning Script

Create a CronJob to automatically tune rate limits:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: zai-rate-tuner
  namespace: devpod
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: tuner
            image: appropriate/curl
            command:
            - /bin/sh
            - -c
            - |
              # Query Prometheus for 429 rate
              PROM_URL="http://prometheus:9090"
              RATE_429=$(curl -s "$PROM_URL/api/v1/query?query=sum(rate(zai_proxy_requests_total{status_code=\"429\"}[6h]))" | jq -r '.data.result[0].value[1]')

              # If 429 rate > 0.01, decrease limit by 20%
              if (( $(echo "$RATE_429 > 0.01" | bc -l) )); then
                echo "High 429 rate detected: $RATE_429"
                echo "Decreasing rate limit..."
                # Update deployment env vars
              fi
```

## Summary & Recommendation

**For production with 2400/5h limit:**

1. **Start conservative**: 0.08 req/s (60% of limit)
2. **Monitor for 24-48h**: Check for any 429s
3. **Gradually increase**: +10% every 24h if no errors
4. **Stop when**: 429 rate exceeds 1% OR hit 0.12 req/s
5. **Set final config**: Last known-good rate as MAX

**Example final config:**
```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "0.08"
  - name: RATE_LIMIT_MIN
    value: "0.05"
  - name: RATE_LIMIT_MAX
    value: "0.11"   # Found via testing
  - name: MAX_RETRIES
    value: "5"
```

This ensures **maximum utilization** while **minimizing client-visible errors**! 🎯
