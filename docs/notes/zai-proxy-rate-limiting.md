# Z.AI Proxy Adaptive Rate Limiting

## Overview

The zai-proxy v1.2.0+ includes **adaptive rate limiting** that automatically adjusts request rates to minimize 429 (rate limit) errors while maximizing throughput.

## How It Works

### Adaptive Algorithm

The proxy uses a **token bucket rate limiter** with automatic adjustment based on upstream 429 responses:

```
┌─────────────────────────────────────────────────────────────┐
│                  Adaptive Rate Limiter                       │
│                                                               │
│  ┌──────────────┐      ┌──────────────┐                     │
│  │ Token Bucket │ ───> │  Rate: X/s   │                     │
│  │   Limiter    │      │ Burst: 2X    │                     │
│  └──────────────┘      └──────────────┘                     │
│         │                      │                             │
│         │                      ▼                             │
│         │              ┌──────────────┐                     │
│         │              │ Every 30s:   │                     │
│         │              │ Analyze 429s │                     │
│         │              └──────┬───────┘                     │
│         │                     │                             │
│         │      ┌──────────────┴──────────────┐              │
│         │      │                             │              │
│         │      ▼                             ▼              │
│         │  >5% 429s?                    <1% 429s?          │
│         │  Decrease 50%                 Increase 10%        │
│         │      │                             │              │
│         └──────┴─────────────────────────────┘              │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### Rate Adjustment Logic

**Every 30 seconds**, the proxy evaluates recent requests:

1. **Calculate 429 rate**: `count(429) / count(total)`

2. **If 429 rate > 5%**:
   - Update the estimated ceiling with an exponentially weighted moving average.
   - Hold at `estimated_ceiling × (1 - RATE_LIMIT_HOLD_MARGIN)`.
   - Reset the clean-window counter.

3. **If 429 rate < 1%**:
   - Increment the clean-window counter and move half of the remaining gap toward
     the hold position (with a 0.25 req/s minimum step).
   - After `RATE_LIMIT_PROBE_INTERVAL` clean windows, probe above the estimated
     ceiling, then reset the counter.

4. **If 429 rate is from 1% through 5%, inclusive**:
   - Preserve the clean-window counter and keep the current rate.

5. **Rate bounds**:
   - Never go below `RATE_LIMIT_MIN` (default: 1 req/s)
   - Never exceed `RATE_LIMIT_MAX` (default: 50 req/s)

### Clean-Window Test Coverage

Clean-window behavior is covered by deterministic unit tests in
`proxy/ratelimiter_statetransition_test.go` and `proxy/ratelimiter_test.go`.
They account for a complete request window and then evaluate it directly, so no
extra synthetic success can move a boundary case below its intended rate.

The tests cover:

- clean rates below 1%, including 0.99%;
- the exact 1% boundary and a just-above 1% rate, both of which preserve the
  counter;
- transitions between clean and middle windows, repeated crossings, and
  persistence across consecutive windows;
- the 5% boundary, high-rate reset behavior, probe activation, and rate
  convergence.

They are part of the main Go suite. Run all packages with `go test ./...`, or
run the focused group with:

```bash
go test ./proxy -run 'CleanWindow|NonCleanWindow|StateTransition|Probe'
```

### Retry Logic

When a 429 error occurs:

1. **Respect Retry-After header** if present
2. **Exponential backoff**: 1s, 2s, 4s, 8s, ...
3. **Retry up to MAX_RETRIES times** (default: 3)
4. **Only then** return 429 to client

This means most transient rate limits are absorbed by the proxy.

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `RATE_LIMIT_INITIAL` | `10` | Starting rate in requests/second |
| `RATE_LIMIT_MIN` | `1` | Minimum rate (never go below) |
| `RATE_LIMIT_MAX` | `50` | Maximum rate (never exceed) |
| `RATE_LIMIT_CEILING_ALPHA` | `0.3` | EWMA weight for new ceiling observations |
| `RATE_LIMIT_HOLD_MARGIN` | `0.02` | Fraction held below the estimated ceiling |
| `RATE_LIMIT_PROBE_INTERVAL` | `10` | Clean windows before a ceiling probe |
| `MAX_RETRIES` | `3` | Number of retry attempts on 429/errors |
| `MAX_WORKERS` | `20` | Max concurrent requests (separate from rate) |

### Example: Conservative Rate Limiting

```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "5"    # Start slow
  - name: RATE_LIMIT_MIN
    value: "1"
  - name: RATE_LIMIT_MAX
    value: "20"   # Cap at 20 req/s
  - name: MAX_RETRIES
    value: "5"    # More retries
```

### Example: Aggressive Rate Limiting

```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "30"   # Start fast
  - name: RATE_LIMIT_MIN
    value: "5"
  - name: RATE_LIMIT_MAX
    value: "100"  # Cap at 100 req/s
  - name: MAX_RETRIES
    value: "2"    # Fewer retries
```

## New Metrics

### Rate Limiting Metrics

1. **`zai_proxy_rate_limit_requests_per_second`** (Gauge)
   - Current rate limit setting
   - Tracks how the rate adjusts over time

2. **`zai_proxy_rate_limit_wait_seconds`** (Histogram)
   - Time requests spend waiting for rate limiter
   - Buckets: 1ms to 10s

3. **`zai_proxy_rate_limit_adjustments_total`** (Counter)
   - Count of rate adjustments by direction
   - Labels: `direction=increase` or `direction=decrease`

4. **`zai_proxy_rate_limit_rejections_total`** (Counter)
   - Requests rejected due to rate limiting
   - Should be rare if MAX_WORKERS is set correctly

5. **`zai_proxy_retry_attempts_total`** (Counter)
   - Retry attempts by reason
   - Labels: `reason=429` or `reason=network_error`

### Useful Queries

**Current rate limit:**
```promql
zai_proxy_rate_limit_requests_per_second
```

**429 error rate:**
```promql
sum(rate(zai_proxy_requests_total{status_code="429"}[5m]))
/
sum(rate(zai_proxy_requests_total[5m]))
```

**Success rate (2xx):**
```promql
sum(rate(zai_proxy_requests_total{status_code=~"2.."}[5m]))
/
sum(rate(zai_proxy_requests_total[5m]))
```

**Rate adjustments over time:**
```promql
rate(zai_proxy_rate_limit_adjustments_total[5m])
```

**Average wait time for rate limiter:**
```promql
histogram_quantile(0.90,
  sum(rate(zai_proxy_rate_limit_wait_seconds_bucket[5m])) by (le)
)
```

**Retry rate:**
```promql
sum(rate(zai_proxy_retry_attempts_total[5m])) by (reason)
```

## Monitoring & Tuning

### Grafana Dashboard

The updated Grafana dashboard includes:
- **Rate limit gauge** - Current req/s limit
- **429 error rate panel** - Track rate limit errors
- **Rate adjustment history** - See when/how rate changes
- **Retry attempts** - Monitor retry frequency

### Tuning Strategy

#### 1. Monitor for 24-48 hours

Watch these metrics:
- `zai_proxy_rate_limit_requests_per_second` - Does it stabilize?
- `zai_proxy_requests_total{status_code="429"}` - Are 429s still occurring?
- `zai_proxy_retry_attempts_total{reason="429"}` - How many retries?

#### 2. Adjust based on patterns

**If you see:**
- **Frequent rate decreases** → Lower `RATE_LIMIT_INITIAL` or `RATE_LIMIT_MAX`
- **Rate stuck at minimum** → Your initial rate is too aggressive
- **429s still reaching clients** → Increase `MAX_RETRIES`
- **Long wait times** → Rate limit is too low for your traffic

#### 3. Optimize for your subscription

**Example: Z.AI subscription allows 60 req/min = 1 req/s**

```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "0.8"   # Start below limit
  - name: RATE_LIMIT_MIN
    value: "0.5"   # 50% of limit
  - name: RATE_LIMIT_MAX
    value: "1.0"   # Exactly at limit
  - name: MAX_RETRIES
    value: "5"     # More retries
```

**Example: Z.AI subscription allows 1000 req/min = 16.7 req/s**

```yaml
env:
  - name: RATE_LIMIT_INITIAL
    value: "15"    # Start below limit
  - name: RATE_LIMIT_MIN
    value: "8"     # 50% of limit
  - name: RATE_LIMIT_MAX
    value: "16"    # Slightly below limit for safety
  - name: MAX_RETRIES
    value: "3"
```

## Architecture

### Request Flow with Rate Limiting

```
Client Request
    │
    ▼
┌─────────────────┐
│ Worker Capacity │ ← MAX_WORKERS check
│ Check           │   (20 concurrent)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Rate Limiter    │ ← Token bucket wait
│ (Adaptive)      │   (X req/s)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Upstream        │
│ Request         │
└────────┬────────┘
         │
         ├─ 200 OK ──────────────────> Success
         │                              Record success
         │                              (maybe increase rate)
         │
         ├─ 429 Rate Limit ──┐
         │                    │
         │                    ▼
         │              ┌──────────────┐
         │              │ Wait + Retry │ ← MAX_RETRIES
         │              └──────┬───────┘   (3 attempts)
         │                     │
         │                     ├─ Success ──> Return to client
         │                     │               Record 429
         │                     │               (decrease rate)
         │                     │
         │                     └─ Still 429 ─> Return 429 to client
         │
         └─ Network Error ──┐
                            │
                            ▼
                      ┌──────────────┐
                      │ Retry Logic  │ ← Exponential backoff
                      └──────────────┘   1s, 2s, 4s, ...
```

## Benefits

### 1. **Automatic Optimization**
- No manual tuning required
- Adapts to changing API limits
- Finds optimal rate automatically

### 2. **Resilience**
- Handles transient 429s via retries
- Backs off aggressively when needed
- Gradually increases when safe

### 3. **Cost Efficiency**
- Maximizes subscription utilization
- Reduces wasted requests
- Minimizes client-visible errors

### 4. **Observability**
- Rich metrics for monitoring
- Clear visibility into rate adjustments
- Easy to debug rate limit issues

## Troubleshooting

### Problem: Rate keeps decreasing

**Symptoms:**
- `zai_proxy_rate_limit_requests_per_second` trending down
- Frequent `decrease` adjustments

**Solutions:**
1. Lower `RATE_LIMIT_INITIAL` - Starting too high
2. Check upstream API health - May be globally rate-limited
3. Verify subscription limits - May have exceeded quota

### Problem: Still seeing 429 errors

**Symptoms:**
- `zai_proxy_requests_total{status_code="429"}` > 0 after retries

**Solutions:**
1. Increase `MAX_RETRIES` - More retry attempts
2. Lower `RATE_LIMIT_MAX` - Too aggressive ceiling
3. Check burst traffic - May need multiple proxy replicas

### Problem: High latency

**Symptoms:**
- `zai_proxy_rate_limit_wait_seconds` p90 > 1s

**Solutions:**
1. Increase `RATE_LIMIT_MAX` - Currently bottlenecked
2. Add more replicas - Distribute load
3. Check if rate stuck at minimum - Adjust RATE_LIMIT_MIN

### Problem: Rate not increasing

**Symptoms:**
- `zai_proxy_rate_limit_requests_per_second` stuck below max
- No `increase` adjustments

**Solutions:**
1. Wait longer - Increases are gradual (10% every 30s)
2. Check 429 rate - Must be <1% to increase
3. Verify traffic volume - Need sustained load to trigger increases

## Advanced: Per-Client Rate Limiting

For future enhancement, you could add per-client rate limiting:

```go
// Example: Rate limit per API key
type ClientRateLimiter struct {
    limiters map[string]*AdaptiveRateLimiter
    mu       sync.RWMutex
}

func (crl *ClientRateLimiter) GetLimiter(clientID string) *AdaptiveRateLimiter {
    crl.mu.RLock()
    limiter, exists := crl.limiters[clientID]
    crl.mu.RUnlock()

    if exists {
        return limiter
    }

    crl.mu.Lock()
    defer crl.mu.Unlock()

    limiter = NewAdaptiveRateLimiter(10, 1, 50)
    crl.limiters[clientID] = limiter
    return limiter
}
```

This would allow different rate limits per client/tenant.

## References

- [Token Bucket Algorithm](https://en.wikipedia.org/wiki/Token_bucket)
- [HTTP 429 Status Code](https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/429)
- [Retry-After Header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Retry-After)
- [Exponential Backoff](https://en.wikipedia.org/wiki/Exponential_backoff)
