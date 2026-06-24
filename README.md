# zai-proxy

Production-grade LLM reverse proxy with token counting, adaptive rate limiting, and a real-time metrics dashboard.

## What it is

zai-proxy sits in front of an OpenAI-compatible LLM API and adds the observability and reliability layer that bare API access lacks:

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

Go backend + React frontend for live monitoring. Scrapes the proxy's Prometheus endpoint every 5 seconds, stores dual-resolution snapshots in SQLite, and streams updates to the browser via SSE.

```
proxy :8080/metrics  →  Collector  →  SQLite  →  SSE Hub  →  React UI
                                       metrics_5s (24h)
                                       metrics_1m (7d)
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
# Dashboard → :3000
```

## Environment variables

### Proxy

| Variable | Default | Description |
|---|---|---|
| `ZAI_API_KEY` | (required) | API key forwarded to the upstream provider |
| `ZAI_TARGET_URL` | provider default | Override the upstream base URL |
| `TOKEN_COUNTING_ENABLED` | `true` | Enable/disable token counting |
| `TOKENIZER_MODEL` | `glm-4` | Tokenizer model name for estimation |
| `MAX_WORKERS` | `10` | Max concurrent upstream requests |
| `DEPLOYMENT_VARIANT` | `production` | Label for blue/green or canary tracking |
| `RATE_LIMIT_INITIAL` | | Initial rate (req/s) |
| `RATE_LIMIT_MIN` | | Minimum rate floor |
| `RATE_LIMIT_MAX` | | Maximum rate ceiling |
| `MAX_RETRIES` | | Retry count on transient errors |

### Dashboard

See [`docs/notes/`](docs/notes/) for dashboard configuration and deployment.

## Metrics

The proxy exposes standard Prometheus metrics at `/metrics`:

- `proxy_requests_total` — request counts by method, path, status, and variant
- `proxy_request_duration_seconds` — latency histogram
- `proxy_response_size_bytes` — response size histogram
- `proxy_upstream_errors_total` — upstream error counts by type and variant
- `proxy_token_input_total` / `proxy_token_output_total` — token counters by model and variant
- `proxy_rate_limit_wait_seconds` — time spent waiting on the rate limiter

## Docs

- [`docs/plan/`](docs/plan/) — architecture decisions and roadmaps
- [`docs/notes/`](docs/notes/) — deployment, operations, monitoring, canary procedures
- [`docs/research/`](docs/research/) — tokenizer research, metrics references
- [`DEVELOPMENT.md`](DEVELOPMENT.md) — contributor guide
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — contribution guidelines

## License

See [LICENSE](LICENSE) if present, or contact the maintainer.
