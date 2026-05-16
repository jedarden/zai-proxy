# Tokenizer Configuration

This document describes the tokenizer configuration options for the Z.AI proxy.

## Environment Variables

### `TOKEN_COUNTING_ENABLED`

**Default:** `true`

Controls whether token counting is enabled or disabled.

**Values:**
- `true` or `1` or unset: Token counting is enabled (default)
- `false` or `0`: Token counting is disabled

**Example:**
```bash
# Disable token counting
export TOKEN_COUNTING_ENABLED=false

# Enable token counting (default)
export TOKEN_COUNTING_ENABLED=true
```

**Behavior:**
- When enabled, the proxy will initialize the tiktoken tokenizer and count tokens for all requests and responses
- When disabled, no tokenizer is initialized and no token metrics are collected
- Disabling can reduce CPU usage and memory footprint if token metrics are not needed

### `TOKENIZER_MODEL`

**Default:** `glm-4`

Specifies the model name to use as a label in Prometheus token metrics.

**Values:** Any string (e.g., `glm-4`, `claude-3`, `gpt-4`, etc.)

**Example:**
```bash
# Set model name for metrics
export TOKENIZER_MODEL=glm-4.7

# Use different model name
export TOKENIZER_MODEL=claude-3-sonnet
```

**Behavior:**
- This is purely for Prometheus metrics labeling and does not affect the tokenization algorithm
- The proxy always uses tiktoken's `cl100k_base` encoding regardless of this setting
- Metrics will be tagged with the specified model name: `zai_proxy_tokens_total{direction="input",model="glm-4"}`
- Useful for tracking token usage per model when the proxy handles multiple models

## Startup Log Messages

The proxy logs its tokenizer configuration at startup:

**Token counting enabled (tiktoken):**
```
Token counting enabled (tiktoken cl100k_base encoding, model: glm-4)
```

**Token counting enabled (fallback mode):**
```
Warning: Failed to initialize TikToken counter: <error>
Falling back to SimpleTokenCounter
Token counting enabled (fallback mode, model: glm-4)
```

**Token counting disabled:**
```
Token counting disabled (TOKEN_COUNTING_ENABLED=false)
```

## Prometheus Metrics

When token counting is enabled, the following metrics are exposed:

### `zai_proxy_tokens_total`

**Type:** Counter

**Labels:**
- `direction`: `input` or `output`
- `model`: Value from `TOKENIZER_MODEL` environment variable

**Description:** Total number of tokens processed by direction and model.

**Example:**
```
# HELP zai_proxy_tokens_total Total number of tokens processed
# TYPE zai_proxy_tokens_total counter
zai_proxy_tokens_total{direction="input",model="glm-4"} 15234
zai_proxy_tokens_total{direction="output",model="glm-4"} 8921
```

### `zai_proxy_token_count_duration_seconds`

**Type:** Histogram

**Description:** Duration of token counting operations in seconds.

**Example:**
```
# HELP zai_proxy_token_count_duration_seconds Duration of token counting operations
# TYPE zai_proxy_token_count_duration_seconds histogram
zai_proxy_token_count_duration_seconds_bucket{le="0.0001"} 142
zai_proxy_token_count_duration_seconds_bucket{le="0.0005"} 289
zai_proxy_token_count_duration_seconds_bucket{le="0.001"} 456
...
```

## Kubernetes Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zai-proxy
spec:
  template:
    spec:
      containers:
      - name: zai-proxy
        image: zai-proxy:latest
        env:
        - name: ZAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: zai-api-key
              key: api-key
        - name: TOKEN_COUNTING_ENABLED
          value: "true"
        - name: TOKENIZER_MODEL
          value: "glm-4"
        - name: MAX_WORKERS
          value: "50"
        - name: RATE_LIMIT_INITIAL
          value: "10"
        - name: RATE_LIMIT_MIN
          value: "1"
        - name: RATE_LIMIT_MAX
          value: "50"
```

## Implementation Details

- **Tokenizer:** Uses tiktoken-go with `cl100k_base` encoding (Claude 3 compatible)
- **Fallback:** If tiktoken initialization fails, falls back to simple word-based approximation
- **Thread-safe:** Token counting is mutex-protected for concurrent access
- **Performance:** Token counting adds minimal latency (~0.1-1ms per request)
- **Streaming:** Supports both streaming (SSE) and non-streaming responses

## See Also

- [RESPONSE_TOKEN_COUNTING.md](../RESPONSE_TOKEN_COUNTING.md) - Token counting workflow
- [TOKEN_COUNTING_WORKFLOW.md](../TOKEN_COUNTING_WORKFLOW.md) - Detailed token counting architecture
