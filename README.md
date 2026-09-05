# zai-proxy

Production-grade LLM reverse proxy with token counting, adaptive rate limiting, and a real-time metrics dashboard.

## What it is

zai-proxy sits in front of the Z.AI Claude API and adds the observability and reliability layer that bare API access lacks:

- **Token tracking** — counts input and output tokens on every request, using API-reported usage when available and tiktoken estimation as a fallback
- **Adaptive rate limiting** — tracks upstream 429 responses with an EWMA ceiling estimator and automatically holds just below the limit, probing periodically to detect ceiling increases
- **Prometheus metrics** — request counts, latency histograms, token rates, error rates, and rate-limit wait times, all labelled by model and deployment variant
- **Blue/green and canary support** — tag requests by `DEPLOYMENT_VARIANT` to compare proxy versions or model configurations side-by-side in the same metrics namespace
- **SSE streaming** — passes through chunked streaming responses without buffering

## Components

### `proxy/`

Go HTTP reverse proxy. Listens on `:8080`, forwards to the upstream LLM API, and exports Prometheus metrics at `/metrics`.

See [proxy/README.md](proxy/README.md) for full configuration and deployment instructions.

### `dashboard/`

Go backend + React frontend for live monitoring. Scrapes the proxy's Prometheus endpoint every 5 seconds, keeps dual-resolution snapshots in bounded in-memory rings, and streams updates to the browser via SSE.

```
proxy :8080/metrics  →  Collector  →  In-memory rings  →  SSE Hub  →  React UI
                                            5s (24h)
                                            1m (7d)
```

Features: request throughput, latency percentiles, token usage totals, error rates, and per-variant comparison panels.

## Quick start

```bash
# 1. Configure the upstream API key and (optionally) override the target URL
export ZAI_API_KEY="your-api-key"
export ZAI_TARGET_URL="https://your-llm-provider.example.com/v1"  # optional

# 2. Start the proxy
cd proxy
go run .

# Proxy → :8080   Metrics → :8080/metrics

# 3. (Optional) start the dashboard in another terminal
cd dashboard
go run .
# Dashboard → :8080
```

## Environment variables

### Proxy

| Variable | Default | Description |
|---|---|---|
| `ZAI_API_KEY` | (required) | API key forwarded to the upstream provider |
| `ZAI_TARGET_URL` | `https://api.z.ai/api/anthropic` | Override the upstream base URL |
| `TOKEN_COUNTING_ENABLED` | `true` | Enable/disable token counting |
| `TOKENIZER_MODEL` | `glm-4` | Tokenizer model name for estimation |
| `MAX_WORKERS` | `10` | Max concurrent upstream requests |
| `DEPLOYMENT_VARIANT` | `production` | Label for blue/green or canary tracking |
| `RATE_LIMIT_INITIAL` | `10.0` | Initial rate (req/s) |
| `RATE_LIMIT_MIN` | `1.0` | Minimum rate floor |
| `RATE_LIMIT_MAX` | `50.0` | Maximum rate ceiling |
| `RATE_LIMIT_CEILING_ALPHA` | `0.3` | EWMA smoothing factor for ceiling estimation |
| `RATE_LIMIT_HOLD_MARGIN` | `0.02` | Hold margin below ceiling (as fraction, e.g., 0.02 = 2%) |
| `RATE_LIMIT_PROBE_INTERVAL` | `10` | Probe ceiling every N clean windows |
| `RATE_LIMIT_STATE_FILE` | `/var/lib/zai-proxy/ceiling.json` | Where the inferred ceiling is persisted across restarts |
| `RATE_LIMIT_STATE_MAX_AGE` | `6h` | Ignore a persisted ceiling older than this on startup |
| `QUOTA_POLL_ENABLED` | `false` | Poll the account quota endpoint out of band (observe-only) |
| `QUOTA_POLL_INTERVAL` | `1m` | Cadence of the out-of-band quota poll |
| `QUOTA_POLL_TIMEOUT` | `5s` | Budget for one quota poll |
| `QUOTA_STALE_AFTER` | `15m` | Age at which the cached quota sample is reported stale |
| `MAX_RETRIES` | `3` | Retry count on transient errors |

### Dashboard

See [`docs/notes/`](docs/notes/) for dashboard configuration and deployment.

## Metrics

The proxy exposes standard Prometheus metrics at `/metrics`:

- `zai_proxy_requests_total` — request counts by method, path, status, and variant
- `zai_proxy_request_duration_seconds` — latency histogram
- `zai_proxy_request_size_bytes` — request size histogram
- `zai_proxy_response_size_bytes` — response size histogram
- `zai_proxy_concurrent_requests` — concurrent request gauge
- `zai_proxy_upstream_errors_total` — upstream error counts by type and variant
- `zai_proxy_tokens_total` — token counters with labels: direction (input/output/cache_read/cache_write), model, variant, pricing_tier
- `zai_proxy_rate_limit_requests_per_second` — current rate limit gauge
- `zai_proxy_rate_limit_wait_seconds` — time spent waiting on the rate limiter, with a bounded `client` source-bucket label
- `zai_proxy_rate_limit_adjustments_total` — rate limit adjustments by direction and variant
- `zai_proxy_rate_limit_rejections_total` — capacity rejections, with a bounded `client` source-bucket label
- `zai_proxy_retry_attempts_total` — retry attempts by reason and variant
- `zai_proxy_worker_utilization_ratio` — worker utilization gauge
- `zai_proxy_max_workers` — maximum concurrent workers gauge
- `zai_proxy_token_rate_seconds` — token processing time histogram
- `zai_proxy_token_rate` — token throughput histogram (tokens per second)
- `zai_proxy_token_count_duration_seconds` — token counting operation duration
- `zai_proxy_build_info` — build information (version, variant, commit, build time)

## Docs

- [`docs/plan/`](docs/plan/) — architecture decisions and roadmaps
- [`docs/notes/`](docs/notes/) — deployment, operations, monitoring, canary procedures
- [`docs/research/`](docs/research/) — tokenizer research, metrics references
- [`DEVELOPMENT.md`](DEVELOPMENT.md) — contributor guide
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — contribution guidelines

## License

See [LICENSE](LICENSE) if present, or contact the maintainer.

---

Part of [jedarden.com](https://jedarden.com)

*This GitHub repo is a read-only mirror of git.ardenone.com/jedarden/zai-proxy — issues and PRs are welcome here either way.*
