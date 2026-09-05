# Environment Variables

This document describes all environment variables supported by the zai-proxy service.

## Tokenizer Configuration

### `TOKEN_COUNTING_ENABLED`
**Type:** Boolean
**Default:** `true`
**Description:** Enable or disable token counting for input and output tokens.

When enabled, the proxy will:
- Count input tokens from request messages using tiktoken cl100k_base encoding
- Count output tokens from response content (both streaming and non-streaming)
- Emit Prometheus metrics: `zai_proxy_tokens_total` and `zai_proxy_token_count_duration_seconds`
- Log token usage for each request

When disabled, the proxy will skip all token counting operations, reducing CPU overhead.

**Valid values:**
- `true`, `1`, or empty (default) - Token counting enabled
- `false`, `0` - Token counting disabled

**Example:**
```bash
# Enable token counting (default)
TOKEN_COUNTING_ENABLED=true

# Disable token counting
TOKEN_COUNTING_ENABLED=false
```

### `TOKENIZER_MODEL`
**Type:** String
**Default:** `glm-4`
**Description:** Model name used for Prometheus metrics labels.

This value is used as the `model` label in the `zai_proxy_tokens_total` metric. It does not affect the tokenization algorithm (which always uses tiktoken cl100k_base encoding), but allows distinguishing token counts by model in metrics.

**Example:**
```bash
# Default
TOKENIZER_MODEL=glm-4

# For different model tracking
TOKENIZER_MODEL=claude-3-opus
TOKENIZER_MODEL=gpt-4
```

**Prometheus metric example:**
```
zai_proxy_tokens_total{direction="input",model="glm-4"} 1234
zai_proxy_tokens_total{direction="output",model="glm-4"} 5678
```

## Worker Configuration

### `MAX_WORKERS`
**Type:** Integer
**Default:** `10`
**Description:** Maximum number of concurrent requests allowed.

When the number of concurrent requests exceeds this limit, new requests will receive a `503 Service Unavailable` response.

**Example:**
```bash
MAX_WORKERS=50
```

## Rate Limiting Configuration

### `RATE_LIMIT_INITIAL`
**Type:** Float
**Default:** `10.0`
**Description:** Initial rate limit in requests per second.

The proxy uses adaptive rate limiting that automatically adjusts based on 429 responses from the upstream API.

**Example:**
```bash
RATE_LIMIT_INITIAL=20.0
```

### `RATE_LIMIT_MIN`
**Type:** Float
**Default:** `1.0`
**Description:** Minimum rate limit in requests per second.

The adaptive rate limiter will never decrease below this value, even when receiving 429 responses.

**Example:**
```bash
RATE_LIMIT_MIN=0.5
```

### `RATE_LIMIT_MAX`
**Type:** Float
**Default:** `50.0`
**Description:** Maximum rate limit in requests per second.

The adaptive rate limiter will never increase above this value, even during successful operation.

**Example:**
```bash
RATE_LIMIT_MAX=100.0
```

### `RATE_LIMIT_ADDITIVE_INCREASE`
**Type:** Float
**Default:** `0.5`
**Description:** Additive increase step in requests per second (AIMD algorithm).

The rate limiter uses AIMD (Additive Increase, Multiplicative Decrease):
- On success (< 1% 429s): rate increases by this fixed amount
- On 429s (> 5%): rate decreases multiplicatively (5-40%)

This produces stable convergence instead of oscillation. For example, with a ceiling of 20 req/s and additive step of 0.5, the rate will converge near 19.5 instead of bouncing between 19 and 20.

**Example:**
```bash
RATE_LIMIT_ADDITIVE_INCREASE=0.5
```

### `RATE_LIMIT_STATE_FILE`
**Type:** String (path)
**Default:** `/var/lib/zai-proxy/ceiling.json`
**Description:** Where the adaptive rate limiter persists the inferred ceiling so a container
restart resumes at the learned value instead of re-learning from `RATE_LIMIT_MAX`. Written
atomically on every ceiling update as `{"ceiling": .., "hold": .., "ts": ..}`.

**Example:**
```bash
RATE_LIMIT_STATE_FILE=/var/lib/zai-proxy/ceiling.json
```

### `RATE_LIMIT_STATE_MAX_AGE`
**Type:** Duration (Go syntax)
**Default:** `6h`
**Description:** Maximum age of a persisted ceiling snapshot for a restart to resume from it.
Older snapshots (or a corrupt/missing file) make the proxy fall back to starting from
`RATE_LIMIT_MAX` and re-learning.

**Example:**
```bash
RATE_LIMIT_STATE_MAX_AGE=30m
```

## Quota Polling Configuration (observe-only)

These variables configure the out-of-band poll of Z.AI's account quota endpoint
(`/api/monitor/usage/quota/limit`). Polling is **observe-only**: it publishes the
normalized five-hour and weekly windows to `/health` and `/metrics` and never
changes request admission. A poll that fails, times out, or returns a malformed
payload keeps the last-known-good sample and counts the outcome; admission stays
with the congestion limiter. Enforcement is a separately gated change.

### `QUOTA_POLL_ENABLED`
**Type:** Boolean
**Default:** `false`
**Description:** Enables the out-of-band quota poll. It stays off by default
because a poll is an authenticated monitor call carrying the account credential.
When disabled, `/health` still reports a `"quota"` section with `enabled: false`.

**Example:**
```bash
QUOTA_POLL_ENABLED=true
```

### `QUOTA_POLL_INTERVAL`
**Type:** Duration (Go syntax)
**Default:** `1m`
**Description:** Cadence of the out-of-band poll. The first poll happens
immediately at startup, so `/health` and `/metrics` populate without waiting an
interval. A value that is not positive falls back to the default rather than
spinning the poll loop.

**Example:**
```bash
QUOTA_POLL_INTERVAL=45s
```

### `QUOTA_POLL_TIMEOUT`
**Type:** Duration (Go syntax)
**Default:** `5s`
**Description:** Budget for one quota poll. A poll that exceeds it counts as an
error outcome and never blocks the request path.

**Example:**
```bash
QUOTA_POLL_TIMEOUT=2s
```

### `QUOTA_STALE_AFTER`
**Type:** Duration (Go syntax)
**Default:** `15m`
**Description:** How long the cached sample stays trusted. Past this, `/health`
reports `fresh: false`, the `zai_proxy_quota_poll_total{result="stale"}` counter
advances once, and the last-known-good values stay exported until a poll
succeeds.

**Example:**
```bash
QUOTA_STALE_AFTER=10m
```

### `ZAI_QUOTA_BASE_URL`
**Type:** String (URL)
**Default:** `https://api.z.ai`
**Description:** Origin hosting the quota endpoint. Override it only for
testing; the path is fixed by the quota client.

**Example:**
```bash
ZAI_QUOTA_BASE_URL=https://api.z.ai
```

## Retry Configuration

### `MAX_RETRIES`
**Type:** Integer
**Default:** `3`
**Description:** Maximum number of retry attempts for failed requests.

The proxy will retry requests on:
- Network errors
- HTTP 429 (Too Many Requests) responses

Exponential backoff is used: 1s, 2s, 4s, etc.

**Example:**
```bash
MAX_RETRIES=5
```

## Required Configuration

### `ZAI_API_KEY`
**Type:** String
**Required:** Yes
**Description:** API key for authenticating with the Z.AI upstream API.

The proxy will fail to start if this variable is not set.

**Example:**
```bash
ZAI_API_KEY=your-api-key-here
```

## Complete Example Configuration

```bash
# Required
ZAI_API_KEY=sk-xxx...

# Tokenizer settings
TOKEN_COUNTING_ENABLED=true
TOKENIZER_MODEL=glm-4

# Worker settings
MAX_WORKERS=20

# Rate limiting
RATE_LIMIT_INITIAL=15.0
RATE_LIMIT_MIN=1.0
RATE_LIMIT_MAX=50.0

# Quota polling (observe-only; off until explicitly enabled)
QUOTA_POLL_ENABLED=true
QUOTA_POLL_INTERVAL=1m
QUOTA_POLL_TIMEOUT=5s
QUOTA_STALE_AFTER=15m

# Retry settings
MAX_RETRIES=3
```

## Startup Logs

When the proxy starts, it logs the current configuration:

```
Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)
Max workers set to: 20
Adaptive rate limiting: initial=15.0, min=1.0, max=50.0 req/s
Quota polling enabled (observe-only): interval=1m0s, timeout=5s, stale_after=15m0s, base_url=https://api.z.ai
Z.AI proxy listening on :8080
Metrics available at :8080/metrics
```

If token counting is disabled:

```
Token counting disabled (TOKEN_COUNTING_ENABLED=false)
Max workers set to: 20
Adaptive rate limiting: initial=15.0, min=1.0, max=50.0 req/s
Quota polling disabled (QUOTA_POLL_ENABLED is not true)
Z.AI proxy listening on :8080
Metrics available at :8080/metrics
```

## Kubernetes ConfigMap Example

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: zai-proxy-config
  namespace: devpod
data:
  TOKEN_COUNTING_ENABLED: "true"
  TOKENIZER_MODEL: "glm-4"
  MAX_WORKERS: "20"
  RATE_LIMIT_INITIAL: "15.0"
  RATE_LIMIT_MIN: "1.0"
  RATE_LIMIT_MAX: "50.0"
  MAX_RETRIES: "3"
```

## Kubernetes Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zai-proxy
  namespace: devpod
spec:
  template:
    spec:
      containers:
      - name: zai-proxy
        image: ghcr.io/ardenone/zai-proxy:latest
        env:
        - name: ZAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: zai-api-key
              key: api-key
        envFrom:
        - configMapRef:
            name: zai-proxy-config
```
