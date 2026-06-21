# ZAI Proxy Ecosystem — Plan

**Last updated:** 2026-05-16
**Version:** proxy/1.10.0, dashboard/1.0.0

## Objective

Provide a stable, observable endpoint for LLM agents to access the Z.AI API without
exposing the Z.AI API key to calling processes. The proxy is the sole keeper of the
credential; agents reach it via cluster-internal DNS — isolation is enforced at the
network layer, not via per-agent authentication.

## Security Model

| Threat | Mitigation |
|--------|------------|
| Agent exfiltrates Z.AI key | Key never leaves proxy pod; agents reach the proxy only via cluster-internal DNS (not public); key is not in agent env, logs, or metrics |
| Network path to proxy compromised | Proxy is not reachable outside the cluster except via Tailscale ingress; no public IP |
| Log scraping leaks key | Z.AI key is never logged; incoming Authorization header is overwritten before forwarding, never echoed |
| Metric label leakage | No credential values in metric labels |
| Runaway agent burns quota | Global adaptive rate limiter + 429 backoff + `MAX_WORKERS` concurrency cap |
| Z.AI quota exhaustion | 429 counter triggers alerts before quota fully consumed |
| Malformed upstream response | Proxy validates response body before committing; retries on empty/truncated JSON |

**What the proxy does NOT do:**

- Validate per-agent credentials (no proxy-key authentication). Any pod that can reach the
  proxy via cluster DNS is treated as authorized. Access control is the cluster's responsibility.
- Cache or store responses.
- Load-balance across multiple Z.AI accounts.

## Architecture

```
LLM Agent (Claude Code, NEEDLE worker, etc.)
    │
    │  POST /v1/messages  (or any path)
    │  Authorization: Bearer <any-value>     ← overwritten; not validated
    ▼
┌─────────────────────────────────────────────────────┐
│                    zai-proxy                        │
│                                                     │
│  • Overwrites Authorization → Bearer <zai-api-key>  │
│  • Enforces concurrency cap (MAX_WORKERS)           │
│  • Global adaptive AIMD rate limiter                │
│  • Counts tokens (tiktoken / API-reported)          │
│  • Validates response body; retries on truncation   │
│  • Records metrics (Prometheus)                     │
│  • TranslateRequest: no-op (Z.AI is Claude-native)  │
│                                                     │
└──────────────────┬──────────────────────────────────┘
                   │  HTTPS
                   ▼
           api.z.ai  (Z.AI upstream)
```

The Z.AI API key lives **only** as a Kubernetes Secret (sealed-secrets encrypted at rest,
injected as an env var into the proxy pod only). No agent process, worker, or tool ever
sees the upstream key.

## Components

### proxy/ — Reverse Proxy (Go)

The core component. Handles:

- **Credential injection:** overwrites the incoming `Authorization` header with
  `Bearer <ZAI_API_KEY>`. No incoming credential is validated — access is controlled
  entirely by network policy (cluster-internal DNS + Tailscale boundary).

- **Concurrency cap:** `MAX_WORKERS` (default 10) bounds the number of in-flight
  requests. Requests beyond the cap receive 503 immediately.

- **Global adaptive rate limiter (AIMD/EWMA):**
  A single token-bucket limiter serves all traffic. Every 30-second window it inspects
  the 429 rate from the upstream and adjusts:
  - If 429-rate > 5 %: updates the estimated ceiling via EWMA
    (`alpha = 0.3`; default), then drops to `ceiling × (1 − hold_margin)`.
  - If 429-rate < 1 %: converges toward the hold position in 50 % steps per window;
    after `probe_interval` clean windows, probes above the ceiling to detect upward shifts.
  - Rate is bounded by `[RATE_LIMIT_MIN, RATE_LIMIT_MAX]` (defaults: 1–50 req/s).
  - Parameters tunable via env: `RATE_LIMIT_CEILING_ALPHA`, `RATE_LIMIT_HOLD_MARGIN`,
    `RATE_LIMIT_PROBE_INTERVAL`.
  - Reset endpoint: `POST /admin/reset-rate-limit` resets to initial rate (unauthenticated).

- **Retry logic:** on network error, 429, or truncated/empty response body, the proxy
  retries up to `MAX_RETRIES` times (default 3) with exponential backoff (1 s, 2 s, 4 s).
  If a 429 carries `Retry-After`, that delay is honoured before the next attempt.

- **Response validation:**
  - Non-streaming: reads the full body before committing; retries if empty or invalid JSON.
  - Streaming: peeks the first 4 KiB; retries if the stream opens with zero bytes.
  - 422 responses are not retried — they indicate a structural request problem.
    Full request/response bodies are logged for diagnosis.

- **Token counting:** prefers API-reported usage from the response body
  (`usage.input_tokens`, `usage.output_tokens`, `usage.cache_read_input_tokens`,
  `usage.cache_creation_input_tokens`). Falls back to tiktoken cl100k_base local counting
  if the response carries no usage block; further falls back to `SimpleTokenCounter` if
  tiktoken fails to initialise. Enabled via `TOKEN_COUNTING_ENABLED` (default `true`).

- **Request translation:** `TranslateRequest` is a documented **no-op**. Z.AI natively
  accepts the Anthropic Claude wire format (including `thinking`, `cache_control`,
  `system` arrays). Prior field-stripping translations caused 422 errors and were removed.

- **Prometheus metrics:** exposes `/metrics` with request counts, latency histograms,
  token usage by direction and pricing tier, rate-limiter state, retry counts,
  and build info.

- **Deployment variants:** `DEPLOYMENT_VARIANT` env distinguishes metric streams from
  production and canary pods. All Prometheus metrics carry a `variant` label.

- **Canary support:** two Deployments share the `devpod` namespace. The canary
  (`zai-proxy-v2`) currently carries all production traffic (original `zai-proxy`
  Deployment is scaled to 0). A `zai-proxy-canary` Service enables weighted traffic
  splits for testing new versions.

### dashboard/ — Metrics Dashboard (Go + React)

The observability layer. Three subsystems work together:

```
zai-proxy /metrics
      │
      │  HTTP scrape every 5 s (per SCRAPE_TARGETS)
      ▼
┌──────────────────────────────────────────────┐
│  Collector (goroutine per target)            │
│  • Parses Prometheus text format             │
│  • Computes per-interval rates (req/s etc.)  │
│  • Infers variant from target URL            │
│    ("test"/"canary" → canary, else prod)     │
│  • Handles counter resets                    │
└──────────┬───────────────────────────────────┘
           │ MetricSnapshot channel
    ┌──────┴──────┐
    ▼             ▼
┌────────┐   ┌─────────────────────────────────┐
│Storage │   │  SSE Hub (broadcast to clients) │
│        │   │  • "connected" event on join     │
│5s/24h  │   │    (scrape_interval, variants)   │
│1m/7d   │   │  • 30 s keepalive heartbeat      │
│SQLite  │   │  • Drops slow consumers          │
│WAL     │   └─────────────────────────────────┘
└────────┘
      │
      ▼
REST API
  GET /api/events              SSE stream (live)
  GET /api/metrics?range=&variant=  Historical snapshots
  GET /api/status              Latest snapshot per variant
  GET /api/config              Scrape interval + targets
  GET /healthz                 Health check
```

**Storage schema (SQLite, WAL mode):**

| Table | Resolution | Retention |
|-------|-----------|-----------|
| `metrics_5s` | 5 s | 24 h |
| `metrics_1m` | 1 min averages | 7 d |

`QueryRange` automatically selects the table: `metrics_5s` for ranges ≤ 1 h,
`metrics_1m` for longer ranges. Downsampling runs every 10 minutes. Retention
purge runs every 10 minutes.

> **Note:** The deployment uses `emptyDir` for `/data` — dashboard history is
> lost on pod restart. A PVC is commented out in the manifest for future use.

**REST API parameters:**

- `GET /api/metrics?range={5m,15m,1h,6h,24h,7d}&variant={production,canary,all}`
- Returns a JSON array of `MetricSnapshot` objects

**Snapshot fields computed by collector:**

| Field | Description |
|-------|-------------|
| `req_rate` | Requests per second (counter rate over interval) |
| `token_rate_in/out` | Input/output tokens per second |
| `error_rate_pct` | `5xx / total * 100` |
| `latency_p50/p95/p99` | Histogram quantiles (ms) |
| `request_size_avg` / `response_size_avg` | Histogram mean (bytes) |
| `status_code_rates` | Per-status-code req/s map |
| `rate_limit_rps` | Current limiter rate |
| `rate_limit_adj_increase/decrease` | AIMD adjustment counters |
| `worker_utilization` | `concurrent / max_workers` |

**Frontend (React/Vite/Tailwind, embedded in binary via `//go:embed`):**

Six panels in a 2×3 responsive grid, each wrapped in an error boundary:

| Panel | What it shows |
|-------|---------------|
| Request Rate | req/s time series |
| Latency | p50 / p95 / p99 (ms) time series |
| Tokens | Input + output token rate (tokens/s) |
| Concurrency | In-flight requests vs MAX_WORKERS |
| Rate Limiter | Current rate, AIMD adjustments, rejections |
| Errors | Error rate %, upstream errors by type |

Global controls:
- **Variant toggle:** Production / Canary / Both — filters all panels
- **Time range selector:** 5 m / 15 m / 1 h / 6 h / 24 h
- **Theme toggle:** Dark / Light
- **Status bar:** connection state, req/s, p50, token rate, error %, workers; stale-data indicators per variant
- **Loading skeleton:** shown until first SSE data arrives
- **Auto-reconnect:** exponential backoff with countdown timer + manual reconnect button
- **History backfill:** on connect, fetches REST history for the current time range before live SSE data arrives

**Dashboard environment variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `SCRAPE_TARGETS` | `http://zai-proxy.mcp.svc.cluster.local:8080/metrics` | Comma-separated scrape URLs |
| `SCRAPE_INTERVAL` | `5s` | How often to scrape |
| `SCRAPE_TIMEOUT` | `3s` | Per-scrape HTTP timeout |
| `LISTEN_ADDR` | `:8080` | Dashboard listen address |
| `DB_PATH` | `/data/dashboard.db` | SQLite file path |
| `RETENTION_5S` | `24h` | High-resolution data retention |
| `RETENTION_1M` | `168h` (7d) | Downsampled data retention |

> The default `SCRAPE_TARGETS` hardcodes `mcp` namespace. In deployments where
> the proxy runs in a different namespace (e.g., `devpod`), override via env.

### Grafana — Prometheus Dashboard (separate from the React dashboard)

A Grafana dashboard ConfigMap lives at
`k8s/ardenone-cluster/monitoring/grafana-dashboard-zai-proxy.yml` and queries
Prometheus directly. Panels:

| Panel | Query |
|-------|-------|
| Total Requests (1h) | `increase(zai_proxy_requests_total[1h])` |
| Error Rate | `rate(4xx+5xx) / rate(total)` |
| 429 Errors (1h) | `increase(requests_total{status_code="429"}[1h])` |
| Response Time p90 | `histogram_quantile(0.90, ...)` |
| Worker Utilization | `sum(zai_proxy_worker_utilization_ratio)` |
| Rate Limit (current) | `zai_proxy_rate_limit_requests_per_second` |
| Concurrent Requests | `sum(zai_proxy_concurrent_requests)` |
| Success Rate | `rate(2xx) / rate(total)` |
| Request Rate by Status | by `status_code` label |
| Concurrent vs Max Workers | concurrent + max_workers overlay |
| Duration Percentiles | p50 / p90 / p99 |
| Request/Response Size p90 | histogram_quantile on size histograms |
| Upstream Errors | by `error_type` label |
| Rate Limit Behavior | retries by reason + adjustments by direction |
| Token panels | total / input / output `increase(...[1h])` |

## Telemetry & Metrics

### Token counting

The proxy records token usage after every request. API-reported counts are preferred;
tiktoken is the fallback.

| Metric | Labels |
|--------|--------|
| `zai_proxy_tokens_total` | `direction={input,output,cache_read,cache_write}`, `model`, `variant`, `pricing_tier={peak,off_peak}` |
| `zai_proxy_request_duration_seconds` | `method`, `path`, `status_code`, `variant` |
| `zai_proxy_requests_total` | `method`, `path`, `status_code`, `variant` |
| `zai_proxy_request_size_bytes` | `method`, `path`, `variant` |
| `zai_proxy_response_size_bytes` | `method`, `path`, `status_code`, `variant` |
| `zai_proxy_concurrent_requests` | `variant` |
| `zai_proxy_max_workers` | `variant` |
| `zai_proxy_worker_utilization_ratio` | `variant` |
| `zai_proxy_token_count_duration_seconds` | `variant` |
| `zai_proxy_token_rate_seconds` | `direction`, `model`, `variant` |
| `zai_proxy_token_rate` | `direction`, `model`, `variant` |
| `zai_proxy_build_info` | `version`, `variant`, `commit`, `build_time` |

**Pricing tier:** `GetPricingTier()` returns `peak` between 02:00–06:00 ET (Z.AI 2×
pricing window), `off_peak` otherwise. Applied to all `tokensTotal` observations.

**Token header:** input token count is also set in `X-Token-Input` response header so
agents can track their own consumption without querying the dashboard.

### Rate-limiter metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `zai_proxy_rate_limit_requests_per_second` | `variant` | Current limiter rate |
| `zai_proxy_rate_limit_wait_seconds` | `variant` | Time waiting in the limiter |
| `zai_proxy_rate_limit_adjustments_total` | `direction={increase,decrease,probe}`, `variant` | Algorithm decisions |
| `zai_proxy_rate_limit_rejections_total` | `variant` | Requests rejected (capacity) |
| `zai_proxy_retry_attempts_total` | `reason={retry,network_error,429,truncated_response,empty_streaming}`, `variant` | Retry causes |
| `zai_proxy_upstream_errors_total` | `error_type={422,429,truncated_response,empty_streaming,upstream_connection,write_error,read_error,request_creation}`, `variant` | Error taxonomy |

### Error classification

| Upstream condition | Proxy action |
|-------------------|--------------|
| 429 + Retry-After | Wait header delay, then retry (up to MAX_RETRIES) |
| 429 no header | Exponential backoff retry |
| 422 | Log bodies, no retry, return 422 to client |
| Empty/invalid JSON body (2xx) | Retry; 502 after MAX_RETRIES |
| Empty streaming response | Retry; 502 after MAX_RETRIES |
| Network error | Retry; 502 after MAX_RETRIES |
| Other 4xx/5xx | Pass through; no retry |

### Dashboard alerting targets (future)

- 429 rate from Z.AI > 5 % over 5 m → alert (quota pressure)
- p95 latency > 10 s → alert (upstream degradation)
- Error rate > 2 % → alert

## Environment Variables

See [`docs/notes/ENVIRONMENT_VARIABLES.md`](../notes/ENVIRONMENT_VARIABLES.md) for the full
reference. Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `ZAI_API_KEY` | required | Upstream Z.AI API key |
| `DEPLOYMENT_VARIANT` | `production` | Metric stream tag |
| `MAX_WORKERS` | `10` | Concurrency cap |
| `TOKEN_COUNTING_ENABLED` | `true` | Enable/disable token counting |
| `TOKENIZER_MODEL` | `glm-4` | Model label for token metrics |
| `RATE_LIMIT_INITIAL` | `10.0` | Starting rate (req/s) |
| `RATE_LIMIT_MIN` | `1.0` | Floor rate |
| `RATE_LIMIT_MAX` | `50.0` | Ceiling cap |
| `RATE_LIMIT_CEILING_ALPHA` | `0.3` | EWMA smoothing factor |
| `RATE_LIMIT_HOLD_MARGIN` | `0.02` | Hold this % below estimated ceiling |
| `RATE_LIMIT_PROBE_INTERVAL` | `10` | Probe above ceiling every N clean windows |
| `MAX_RETRIES` | `3` | Max retry attempts |
| `ZAI_TARGET_URL` | `https://api.z.ai/api/anthropic` | Upstream URL |

## Repository Layout

```
zai-proxy/                          (git.ardenone.com/jedarden/zai-proxy)
├── proxy/                          Go module: git.ardenone.com/jedarden/zai-proxy
│   ├── main.go                     HTTP server, routing, rate limiter, retry logic
│   ├── translator.go               No-op (Z.AI natively speaks the Claude wire format)
│   ├── bodyparser.go               Body parsing, streaming capture, usage injection
│   ├── tokenizer.go                Token counting (tiktoken cl100k_base + GLM fallback)
│   ├── metrics.go                  Prometheus instrumentation + pricing tier logic
│   ├── evaluation/                 Offline eval harness (token count accuracy vs Anthropic API)
│   ├── cmd/evaluate/               CLI for batch evaluation
│   ├── cmd/demo-eval/              Demo evaluation runner
│   ├── scripts/                    Load test, canary integration, benchmarks
│   ├── tests/                      Integration and regression test suites
│   └── Dockerfile                  Production image
├── dashboard/                      Go module: git.ardenone.com/jedarden/zai-proxy/dashboard
│   ├── main.go                     HTTP server + SSE broadcaster
│   ├── collector/                  Prometheus scraper + parser
│   ├── api/                        REST + SSE handlers
│   ├── storage/                    SQLite persistence layer
│   ├── model/                      Shared metric data types
│   ├── logger/                     Structured logger
│   └── frontend/                   React/Vite/Tailwind dashboard UI
└── docs/
    ├── plan/plan.md                This document
    ├── notes/                      Deployment, operations, canary procedures
    └── research/                   Tokenizer research, metrics references
```

## CI/CD

Build templates live in `jedarden/declarative-config → k8s/iad-ci/argo-workflows/`:

| Template | Builds | Pushes to |
|----------|--------|-----------|
| `zai-proxy-build` | `proxy/` | `ronaldraygun/zai-proxy:{VERSION}` |
| `zai-proxy-dashboard-build` | `dashboard/` | `ronaldraygun/zai-proxy-dashboard:{VERSION}` |

Both templates clone from `git.ardenone.com/jedarden/zai-proxy` (no auth required).
Versions are read from `proxy/VERSION` and `dashboard/VERSION` respectively.

Triggering a build:
```bash
kubectl --kubeconfig=/home/coding/.kube/iad-ci.kubeconfig create -f - <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: zai-proxy-build-manual-
  namespace: argo-workflows
spec:
  workflowTemplateRef:
    name: zai-proxy-build
EOF
```

## Deployment

Both components deploy to the `devpod` namespace on `ardenone-cluster` via ArgoCD from
`jedarden/declarative-config`.

Key manifests:
- `k8s/ardenone-cluster/devpod/zai-proxy.yml` — original Deployment (currently replicas=0)
- `k8s/ardenone-cluster/devpod/zai-proxy-v2.yml` — active production Deployment
- `k8s/ardenone-cluster/devpod/zai-proxy-canary-deployment.yml` — canary config
- `k8s/ardenone-cluster/devpod/zai-proxy-canary-service.yml` — weighted traffic split
- `k8s/ardenone-cluster/devpod/zai-proxy-tailscale.yml` — Tailscale ingress
- `k8s/ardenone-cluster/devpod/zai-proxy-servicemonitor.yml` — Prometheus scrape target
- `k8s/ardenone-cluster/monitoring/grafana-dashboard-zai-proxy.yml` — Grafana dashboard

The Z.AI API key flows: OpenBao → ESO ExternalSecret → K8s Secret → proxy pod env
(read once at startup; never written to any metric, log, or response).

Workers reach the proxy via cluster-internal DNS:
- Production: `http://zai-proxy.devpod.svc.cluster.local:8080/api/anthropic`
- Canary: `http://zai-proxy-test.devpod.svc.cluster.local:8080/api/anthropic`

## Operations

| Document | What it covers |
|----------|----------------|
| `docs/notes/ENVIRONMENT_VARIABLES.md` | Full env var reference |
| `docs/notes/DEPLOYMENT.md` | Production/canary dual-deploy workflow |
| `docs/notes/CANARY_PROMOTION_PROCEDURE.md` | Step-by-step canary promotion |
| `docs/notes/CANARY_PROMOTION_CHECKLIST.md` | Go/no-go checklist |
| `docs/notes/CANARY_ROLLBACK_PROCEDURE.md` | Rollback triggers and steps |
| `docs/notes/CANARY_TROUBLESHOOTING_GUIDE.md` | Common canary issues |
| `docs/notes/REGRESSION_TESTING.md` | Regression test suite overview |
| `docs/notes/REGRESSION_TEST_GUIDE.md` | Running regression tests |
| `docs/notes/TOKEN_COUNTING.md` | Token counting design and validation |
| `docs/notes/TOKENIZER_CONFIGURATION.md` | Tokenizer tuning |
| `docs/notes/MONITORING_SETUP.md` | Grafana + Prometheus setup |
| `docs/notes/zai-proxy-rate-limiting.md` | Adaptive rate limiter deep-dive |
| `docs/notes/TROUBLESHOOTING.md` | General troubleshooting |

## Migration Status

- [x] Source extracted from `ardenone-cluster/containers/zai-proxy` → `proxy/`
- [x] Source extracted from `ardenone-cluster/containers/zai-proxy-dashboard` → `dashboard/`
- [x] Go module paths updated to `git.ardenone.com/jedarden/zai-proxy[/dashboard]`
- [x] Argo Workflow templates created (`zai-proxy-build`, `zai-proxy-dashboard-build`)
- [x] Push new workflow templates to declarative-config (triggers ArgoCD sync)
- [x] Update documentation to point to new repo
- [x] Retire `ardenone-cluster/containers/zai-proxy` and `containers/zai-proxy-dashboard` once builds verified from new repo

**Migration complete as of 2026-06-21.** The zai-proxy project now lives at `git.ardenone.com/jedarden/zai-proxy` with CI/CD workflow templates deployed via ArgoCD.
