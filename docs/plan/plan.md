# ZAI Proxy Ecosystem — Plan

**Last updated:** 2026-09-04
**Version:** proxy/1.11.1, dashboard/1.1.5

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
| Runaway agent burns quota | Account-quota pacing supervises the global adaptive rate limiter; `MAX_WORKERS` remains the independent concurrency cap |
| Z.AI quota exhaustion | Poll the provider quota windows, reserve capacity, and fail fast until reset instead of discovering exhaustion through retry storms |
| Promotional or harness-specific allowance changes | Treat cost/allowance as observed provider state, not a permanent property of a model or client; keep a fallback path until the entitlement is verified over time |
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
    │  Authorization: <any-value>            ← removed; not validated
    ▼
┌─────────────────────────────────────────────────────┐
│                    zai-proxy                        │
│                                                     │
│  • Replaces caller auth with x-api-key              │
│  • Enforces concurrency cap (MAX_WORKERS)           │
│  • Quota-paced adaptive request admission           │
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

- **Credential injection:** removes incoming `Authorization` and replaces any caller
  credential with `x-api-key: <ZAI_API_KEY>` for the Anthropic-compatible upstream.
  No incoming credential is validated — access is controlled entirely by network policy
  (cluster-internal DNS + Tailscale boundary).

- **Concurrency cap:** `MAX_WORKERS` (default 10) bounds the number of in-flight
  requests. Requests beyond the cap receive 503 immediately.

- **Global adaptive rate limiter (AIMD/EWMA, current short-horizon control):**
  A single adaptive token-bucket limiter remains the global ceiling for all traffic.
  When that ceiling is contended, requests are queued into 64 deterministic source
  buckets derived from the direct peer IP and dispatched round-robin, so one source
  cannot monopolize the shared budget. Bucket collisions are possible by design to
  bound queue state and Prometheus cardinality; forwarded headers and caller-supplied
  identities are deliberately ignored because they are not credentials. Every
  30-second window it inspects the 429 rate from the upstream and adjusts:
  - If 429-rate > 5 %: updates the estimated ceiling via EWMA
    (`alpha = 0.3`; default), then drops to `ceiling × (1 − hold_margin)`.
  - If 429-rate < 1 %: converges toward the hold position in 50 % steps per window;
    after `probe_interval` clean windows, probes above the ceiling to detect upward shifts.
  - Rate is bounded by `[RATE_LIMIT_MIN, RATE_LIMIT_MAX]` (defaults: 1–50 req/s).
  - Parameters tunable via env: `RATE_LIMIT_CEILING_ALPHA`, `RATE_LIMIT_HOLD_MARGIN`,
    `RATE_LIMIT_PROBE_INTERVAL`.
  - Reset endpoint: `POST /admin/reset-rate-limit` resets to initial rate (unauthenticated).

- **Quota-aware supervisor (planned long-horizon control):** periodically reads Z.AI's
  account quota windows and derives a quota-paced admission cap. The effective request
  rate is the minimum of the configured maximum, the learned short-horizon congestion
  ceiling, and the quota cap. Quota data guides pacing; it does not replace the
  concurrency cap or the 429-based congestion controller.

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

- **Canary support:** the `zai-proxy` Deployment is the two-replica production
  workload selected by the `zai-proxy` Service. The separate `zai-proxy-canary`
  Deployment and Service isolate canary traffic for testing new versions.

#### Quota-aware throttling plan

Z.AI exposes two distinct usage signals, and the proxy must not conflate them:

1. Anthropic-compatible model responses contain per-call token usage. The proxy already
   prefers these values over local token estimates. They are useful for attribution and
   burn-rate diagnostics, but plan credits are not assumed to equal raw tokens because
   model, cache, and promotional multipliers can change.
2. Z.AI's official Coding Plan usage tooling queries account-level endpoints at
   `/api/monitor/usage/model-usage`, `/api/monitor/usage/tool-usage`, and
   `/api/monitor/usage/quota/limit`. The quota endpoint is the primary observed budget
   signal for five-hour and weekly model-usage windows. It is polled out of band, never
   once per model request. The integration must accept both legacy limit types and the
   current credit-based schema, ignore unknown types safely, and retain only normalized,
   non-secret state in logs and metrics. The endpoint set and authentication pattern are
   taken from Z.AI's
   [official usage-query plugin](https://github.com/zai-org/zai-coding-plugins/blob/main/plugins/glm-plan-usage/skills/usage-query-skill/scripts/query-usage.mjs).

The quota controller is deliberately supervisory because quota observations may lag,
may include traffic outside this proxy, and do not describe instantaneous upstream
concurrency. For every valid quota window it computes:

- usable remaining allowance after `QUOTA_RESERVE_FRACTION`;
- time remaining until the provider's reset;
- an EWMA of observed allowance burn between successful polls;
- an estimated sustainable admission rate from allowed burn per second divided by
  observed burn per completed request.

The smallest sustainable rate across active model-quota windows becomes the quota cap.
Reductions take effect immediately; increases are smoothed and step-limited so a reset
or promotional allowance does not create a burst. Until enough deltas exist to estimate
burn per request, quota state is advisory and the existing congestion limiter remains
authoritative. If quota data becomes stale, the proxy keeps the last cap only until
`QUOTA_STALE_AFTER`, then degrades to congestion-only control and alerts rather than
inventing quota state.

The resulting admission decision is:

```text
effective_rate = min(configured_max, congestion_hold_rate, quota_rate_cap)
```

`RATE_LIMIT_MIN` applies only to the congestion controller. The quota supervisor may
pace below that floor. Confirmed exhaustion opens a circuit and fails new requests fast
with HTTP 429 plus the known reset time; it must not leave requests queued until a
five-hour or weekly reset. Every actual upstream attempt, including a proxy retry, must
obtain admission so retries cannot bypass quota pacing.

HTTP status alone is insufficient feedback. The proxy will read a bounded Z.AI error
body and classify its business code before adapting:

| Z.AI condition | Supervisory action |
|----------------|--------------------|
| Concurrency limit (`1302`) | Reduce/hold concurrent admission; do not poison the RPS ceiling |
| Frequency/rate limit (`1303`, `1305`) | Feed the short-horizon congestion controller and honor `Retry-After` |
| Five-hour quota exhausted (`1308`) | Open the quota circuit until the advertised reset |
| Weekly/monthly quota exhausted (`1310`) | Open the applicable longer-window circuit until reset |
| Temporary model congestion (`1312`) | Jittered backoff and optional model fallback; do not treat as account-quota exhaustion |
| Unknown 429 | Conservative bounded retry, preserve the upstream response, and expose an `unknown` classification metric |

On the final failed attempt, the proxy preserves the safe upstream error body,
`Retry-After`, and reset metadata for the caller. Quota polling uses a separate client
with a short timeout, exponential backoff, and cached last-known-good state; a monitor
endpoint failure must never block the model data path.

Quota is account-wide. A multi-replica deployment must not give every pod the full
account cap independently. Before enabling enforcement with more than one replica, use
one leader to publish normalized quota state and divide the account admission budget
among healthy replicas, or use an equivalent shared account-level limiter. The
single-replica apex deployment may enforce the cap locally during the first rollout.

ZCode is a separate execution path. First-party ZCode idle-time tasks are documented as
free and quota-free for eligible subscribers, but ordinary ZCode tasks use the connected
Coding Plan and its quota. Time-limited Unlimited Flash promotions are not a permanent
cost guarantee. NEEDLE may prefer ZCode for bead work when live usage observations show
zero quota burn, but it must keep the direct proxy harness as a fallback until ZCode has
met reliability, headless-control, and sustained zero-burn acceptance gates. ZCode
traffic that bypasses this proxy remains visible only through account-level quota deltas;
the proxy must include that external burn when calculating its own safe cap. The
[ZCode documentation](https://zcode.z.ai/en/docs/welcome) is the authority for which task
modes are quota-free, and its [usage view](https://zcode.z.ai/en/docs/usage-stats) is an
independent operator check on the proxy's normalized quota telemetry.

Rollout and acceptance:

1. Ship quota polling and metrics in observe-only mode; compare provider quota deltas
   with proxy request/token metrics and ZCode activity for at least one full five-hour
   window and one weekly reset.
2. Parse and expose Z.AI business error classes without changing admission behavior.
3. Enable a quota cap in canary, with a configurable reserve and an immediate kill
   switch; prove that stale/malformed quota responses degrade safely.
4. Enable production enforcement after canary shows no premature circuit opens, no
   retry amplification, and quota consumption remains within the planned pace.
5. Treat ZCode as the default bead worker only after repeated end-to-end bead completion,
   verification, and usage telemetry demonstrate that its target task mode consumes no
   paid quota. Deprecate, but do not initially remove, the Claude-Code-plus-GLM adapter.

##### ZCode proving tranche (2026-09-05)

Use the observe-only implementation as a production-quality build evaluation for the
custom `zcode-headless` NEEDLE adapter during Z.AI's
[GLM-5.3-Flash Usage Campaign](https://docs.z.ai/devpack/notice/event-glm-5.3-flash).
From 2026-09-03 through 2026-09-20, the campaign makes ZCode + GLM-5.3-Flash
zero-quota and unlimited daily from 23:00 to 09:00 Singapore time (15:00 to 01:00
UTC; 11:00 to 21:00 EDT). Each NEEDLE invocation remains bound to exactly one
harness and inference model: `zcode-headless` + `glm-5.3-flash`. It does not switch
to Claude Code or another model. Separate adapters and worker invocations own any
fallback path.

The first dispatch uses only the three independent, ready work packages below. Their
resource keys and file ownership keep concurrent workers from editing the same surface:

| Bead | Work package | Initial dependency/state |
|------|--------------|--------------------------|
| `zaiproxy-3c47e2c0` | Credential-safe quota client and schema normalizer | Ready; owns new `proxy/quota/` files |
| `zaiproxy-04dbae1f` | Bounded Prometheus quota metrics | Ready; owns `proxy/metrics*` |
| `zaiproxy-d032b140` | Bounded Z.AI business-error parser | Ready; owns new `proxy/zai_error*` files |
| `zaiproxy-00dedd67` | Observe-only poller, health, and configuration wiring | Blocked by client + metrics |
| `zaiproxy-e212b64b` | Retry/rate-feedback integration | Blocked by error parser + metrics |
| `zaiproxy-34621366` | Dashboard quota and freshness presentation | Blocked by poller + metrics |
| `zaiproxy-6e54beab` | End-to-end observe-only and campaign zero-burn validation | Blocked by all integration work |
| `zaiproxy-0a1eaf6d` | Quota-cap enforcement in single-replica canary | Deferred until validation evidence exists |
| `zaiproxy-88f2b3d6` | Multi-replica account-budget coordination | Deferred until canary enforcement succeeds |

The umbrella bead is `zaiproxy-a544ad8a` and remains deferred so a worker cannot claim
planning prose as implementation. ZCode workers must run with Explore and generative
strands disabled for this tranche, must hold no bead while waiting outside the campaign
window, and should exit after the scoped ready frontier is exhausted. A worker already
executing at 21:00 EDT may finish and verify its bead; it must not begin a new claim.

This tranche passes the build evaluation only when workers independently produce
reviewable commits, respect file/resource boundaries, close beads only after repository
tests pass, avoid credentials and raw account payloads, and require no human repair of
their implementation. Throughput alone is not success.

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
│In-memory│  │  • Drops slow consumers          │
│rings   │   └─────────────────────────────────┘
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

**Storage layout (in-memory ring buffers):**

| Buffer | Resolution | Retention | Capacity per variant |
|--------|-----------|-----------|----------------------|
| Raw | 5 s | 24 h | 17,280 snapshots |
| Downsampled | 1 min averages | 7 d | 10,080 snapshots |

The storage holds fixed-size rings for the production and canary streams, with
no database, PVC, or filesystem dependency. `QueryRange` automatically selects
the raw ring for ranges ≤ 1 h and the downsampled ring for longer ranges.
Downsampling and retention cleanup run every 10 minutes. A pod restart begins
with an empty window, which refills through normal scraping.

**REST API parameters:**

- `GET /api/metrics?range={5m,15m,1h,6h,24h,7d}&variant={production,canary,all}`
- Returns a JSON array of `MetricSnapshot` objects

**Snapshot fields computed by collector:**

| Field | Description |
|-------|-------------|
| `req_rate` | Requests per second (counter rate over interval) |
| `token_rate_in/out` | Input/output tokens per second |
| `token_rate_cache_read/write` | Cache-read/cache-write tokens per second |
| `error_rate_pct` | `5xx / total * 100` |
| `latency_p50/p95/p99` | Histogram quantiles (ms) |
| `request_size_avg` / `response_size_avg` | Histogram mean (bytes) |
| `status_code_rates` | Per-status-code req/s map |
| `rate_limit_rps` | Current limiter rate |
| `rate_limit_adj_increase/decrease` | AIMD adjustment counters |
| `quota_used_ratio` / `quota_remaining_ratio` | Provider-reported account quota state by window |
| `quota_rate_cap` / `quota_gate_open` | Quota supervisor's current pacing decision |
| `quota_sample_age_seconds` | Freshness of the last valid account-quota sample |
| `worker_utilization` | `concurrent / max_workers` |

**Frontend (React/Vite/Tailwind, embedded in binary via `//go:embed`):**

Six panels in a 2×3 responsive grid, each wrapped in an error boundary:

| Panel | What it shows |
|-------|---------------|
| Request Rate | req/s time series |
| Latency | p50 / p95 / p99 (ms) time series |
| Tokens | Input + output + cache-read + cache-write token rate (tokens/s) and window running totals |
| Concurrency | In-flight requests vs MAX_WORKERS |
| Rate Limiter | Effective rate, congestion ceiling, quota cap/gate, sample freshness, adjustments, and rejections |
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
| `SCRAPE_TARGETS` | `http://zai-proxy.devpod.svc.cluster.local:8080/metrics` | Comma-separated scrape URLs |
| `SCRAPE_INTERVAL` | `5s` | How often to scrape |
| `SCRAPE_TIMEOUT` | `3s` | Per-scrape HTTP timeout |
| `LISTEN_ADDR` | `:8080` | Dashboard listen address |
| `RETENTION_5S` | `24h` | High-resolution data retention |
| `RETENTION_1M` | `168h` (7d) | Downsampled data retention |

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
| `zai_proxy_rate_limit_wait_seconds` | `variant`, `client={source-00…source-63}` | Time waiting in the limiter, by bounded source bucket |
| `zai_proxy_rate_limit_adjustments_total` | `direction={increase,decrease,probe}`, `variant` | Algorithm decisions |
| `zai_proxy_rate_limit_rejections_total` | `variant`, `client={source-00…source-63}` | Requests rejected at the concurrency cap, by bounded source bucket |
| `zai_proxy_retry_attempts_total` | `reason={retry,network_error,429,truncated_response,empty_streaming}`, `variant` | Retry causes |
| `zai_proxy_upstream_errors_total` | `error_type={HTTP status (for example 400,422,429,500),truncated_response,empty_streaming,upstream_connection,write_error,read_error,request_creation}`, `variant` | Error taxonomy |
| `zai_proxy_quota_usage_ratio` | `window={five_hour,weekly}`, `limit_type`, `variant` | Normalized provider-reported usage |
| `zai_proxy_quota_remaining_ratio` | `window`, `limit_type`, `variant` | Remaining fraction after no local policy adjustment |
| `zai_proxy_quota_reset_time_seconds` | `window`, `limit_type`, `variant` | Provider reset time as a Unix timestamp |
| `zai_proxy_quota_rate_cap` | `variant` | Account-level cap derived from quota pacing |
| `zai_proxy_quota_gate_open` | `window`, `variant` | Whether confirmed exhaustion is rejecting new work |
| `zai_proxy_quota_sample_age_seconds` | `variant` | Age of the last valid quota sample |
| `zai_proxy_quota_poll_total` | `result={success,error,malformed,stale}`, `variant` | Quota monitor health |
| `zai_proxy_zai_errors_total` | `class={concurrency,frequency,quota,model_congestion,unknown}`, `code`, `variant` | Bounded business-error classification |

### Error classification

| Upstream condition | Proxy action |
|-------------------|--------------|
| 429 frequency limit (`1303`, `1305`) | Acquire admission, wait the larger of jittered backoff or `Retry-After`, then retry up to `MAX_RETRIES` |
| 429 quota exhaustion (`1308`, `1310`) | Do not retry; open quota circuit until reset and preserve reset metadata |
| 429 concurrency limit (`1302`) | Reduce/hold concurrent admission and retry only within the bounded retry policy |
| Temporary model congestion (`1312`) | Jittered retry and optional fallback without reducing the account quota cap |
| Unknown 429 | Conservative bounded retry; preserve upstream error response and classify as unknown |
| 422 | Log bodies, no retry, return 422 to client |
| Empty/invalid JSON body (2xx) | Retry; 502 after MAX_RETRIES |
| Empty streaming response | Retry; 502 after MAX_RETRIES |
| Network error | Retry; 502 after MAX_RETRIES |
| Other 4xx/5xx | Pass through; no retry |

### Dashboard alerting targets (future)

- Frequency-limit 429 rate from Z.AI > 5 % over 5 m → alert (short-horizon pressure)
- Five-hour or weekly quota is ahead of planned burn pace → warning
- Remaining usable quota reaches the configured reserve → critical
- Quota sample older than `QUOTA_STALE_AFTER` → warning; enforcement degrades safely
- Quota gate remains open after the advertised reset → critical
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
| `QUOTA_TRACKING_ENABLED` | `true` | Poll and expose account-quota state; enforcement remains separately gated during rollout |
| `QUOTA_ENFORCEMENT_ENABLED` | `false` | Apply the derived quota cap and exhaustion circuit |
| `QUOTA_POLL_INTERVAL` | `60s` | Normal account-quota polling interval |
| `QUOTA_STALE_AFTER` | `5m` | Maximum age for quota state to affect admission |
| `QUOTA_RESERVE_FRACTION` | `0.05` | Preserve this fraction of each quota window from routine fleet consumption |
| `QUOTA_RATE_ALPHA` | `0.2` | EWMA smoothing for observed quota burn and upward cap changes |
| `QUOTA_RATE_MAX_INCREASE` | `0.20` | Maximum fractional cap increase per valid quota sample |
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
│   ├── storage/                    In-memory dual-resolution ring buffers
│   ├── model/                      Shared metric data types
│   ├── logger/                     Structured logger
│   └── frontend/                   React/Vite/Tailwind dashboard UI
└── docs/
    ├── plan/plan.md                This document
    ├── notes/                      Deployment, operations, canary procedures
    └── research/                   Tokenizer research, metrics references
```

GitOps deployment manifests live in the separate `jedarden/declarative-config`
repository:

| Path | Purpose |
|------|---------|
| `k8s/ardenone-cluster/devpod/zai-proxy.yml` | Canonical two-replica production Deployment and its `zai-proxy` Service |
| `k8s/ardenone-cluster/devpod/zai-proxy-canary-deployment.yml` | Isolated canary Deployment |
| `k8s/ardenone-cluster/devpod/zai-proxy-canary-service.yml` | Canary Service for test traffic |

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
- `k8s/ardenone-cluster/devpod/zai-proxy.yml` — canonical two-replica production Deployment and Service
- `k8s/ardenone-cluster/devpod/zai-proxy-canary-deployment.yml` — canary config
- `k8s/ardenone-cluster/devpod/zai-proxy-canary-service.yml` — canary traffic Service
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
| `dashboard/README.md` | Dashboard architecture, components, and development guide |
| `docs/notes/DASHBOARD_API_REFERENCE.md` | Dashboard REST API and SSE event documentation |
| `DEVELOPMENT.md` | Development workflow, testing, and CI/CD |
| `CONTRIBUTING.md` | Contribution guidelines and code review process |
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

## ADR-1: 2026-07-20 — Dashboard metrics storage: drop the PVC-backed SQLite dependency, go stateless

### Context

A 2026-07-20 live-artifact check of the `devpod` namespace on `ardenone-cluster` found
`zai-proxy-dashboard` non-functional in production, and has apparently been so for weeks:

- `zai-proxy-dashboard-5ff6b485f-5fpn6`: `CrashLoopBackOff`, 14,569 restarts over 51 days.
  `--previous` logs show: `failed to initialize storage ... unable to open database file:
  out of memory (14)`.
- `zai-proxy-dashboard-b9fd57878-thc4t`: stuck `ContainerCreating` for 36 days — a second
  ReplicaSet fighting the first for the single `ReadWriteOnce` PVC (`zai-proxy-dashboard-data`,
  Longhorn), which can only attach to one node/pod at a time.
- The public URL (`https://zai-dash.ardenone.com/`) returns `503 no available server` —
  confirmed by direct curl during this audit.

Root cause chain: `dashboard/main.go` treats storage initialization as fatal
(`os.Exit(1)` if `storage.NewStorage()` fails), with no in-memory/degraded fallback. Any
hiccup opening the SQLite file on the Longhorn-backed PVC takes the entire dashboard
offline, and because the volume is RWO, a wedged old pod can block the replacement pod's
attach — turning a transient storage error into a weeks-long outage that nothing alerted
on (the dashboard itself is the thing that would have shown the alert).

Meanwhile, the same metrics this dashboard exists to serve are already durably captured
elsewhere: `k8s/ardenone-cluster/devpod/zai-proxy-servicemonitor.yml` has Prometheus
(`kube-prometheus-stack`, confirmed live, `retention: 10d`) scraping `/metrics` from both
`zai-proxy` (production pods) and `zai-proxy-canary` every 15s, and
`k8s/ardenone-cluster/monitoring/grafana-dashboard-zai-proxy.yml` already renders
essentially the same panel set (throughput, latency percentiles, error rate, token rate,
rate-limiter state) straight from Prometheus. The custom dashboard's SQLite tables
(`metrics_5s`/24h, `metrics_1m`/7d) are therefore redundant history — Prometheus already
retains equal-or-longer history at comparable resolution. The genuinely differentiated
value of the Go+React dashboard is the live SSE push experience (sub-5s updates, variant
toggle, connection status, loading skeleton) — none of which requires durable storage.

### Decision

Make the dashboard backend stateless: replace the PVC-backed SQLite store with an
in-process, bounded in-memory ring buffer (sized for the documented 24h@5s / 7d@1m
windows — a few thousand small structs, trivially within the existing 256Mi limit) for
serving `/api/metrics`, `/api/status`, and the SSE stream. Drop the
`zai-proxy-dashboard-data` PVC and the Longhorn dependency entirely. On pod restart the
in-memory window starts empty and refills over subsequent scrape cycles — acceptable
because Grafana remains the system of record for anything older than the live view, and
the frontend's existing "history backfill on connect" behavior already tolerates a thin
initial window.

### Alternatives Considered

1. **Keep SQLite, switch to `emptyDir`** (what this plan originally documented as the
   design, before the PVC was actually added). Fixes the RWO multi-attach/stuck-rollout
   half of the incident but not the fragility half: node drain/pod eviction still loses
   history, and a corrupt or ENOMEM-prone SQLite open still hits the same fatal
   `os.Exit(1)` path. Rejected as only a partial fix.
2. **Fix the immediate incident (stuck ReplicaSet + PVC) and leave the architecture as
   is.** Cheapest, restores service fastest, but leaves the exact failure class in place
   to recur — which is how it went unnoticed for 51+ days in the first place. Tracked
   separately as a near-term tactical bead; not sufficient alone as the long-term
   answer.
3. **Make storage-init failures non-fatal (log + run in degraded in-memory mode) but
   keep SQLite+PVC for the common case.** Smaller diff, preserves durable history across
   ordinary restarts. Rejected as the primary decision because it still carries the
   RWO/Longhorn operational risk that caused half of this outage — but it's a reasonable
   defense-in-depth addition regardless of which storage backend wins (see Consequences).
4. **Point the dashboard's storage layer at Prometheus via PromQL range queries** instead
   of an in-memory buffer, avoiding re-implementing retention/downsampling logic.
   Rejected for this first cut because it adds a new runtime dependency (dashboard →
   Prometheus reachability) and a PromQL query layer; worth revisiting once the
   stateless rewrite ships, as a way to extend the dashboard's default ranges past the
   in-memory window using Prometheus's 10d+ retention.

### Consequences

- Eliminates the PVC/Longhorn failure mode entirely for this component — removes the
  exact bug class that caused the current 51+ day outage.
- Dashboard becomes trivially horizontally scalable (no RWO volume contention), closing
  the way for running it as more than a single replica.
- Loses metrics history across pod restarts/redeploys. Anyone needing history beyond the
  live window should be pointed at the existing Grafana dashboard, which already has
  equivalent panels with comparable-or-longer retention.
- Simplifies `k8s/ardenone-cluster/devpod/zai-proxy-dashboard.yml`: drop the PVC,
  `storageClassName: longhorn`, and the volume mount.
- Should be paired with making storage-layer errors non-fatal in general (Alternative 3)
  as defense in depth, so a future storage bug degrades the dashboard instead of taking
  it down entirely.

Tracked via beads: restoring the current outage is a near-term tactical fix; the
stateless storage rewrite this ADR commits to is tracked as follow-up implementation
work against `dashboard/storage/`, `dashboard/main.go`, and
`k8s/ardenone-cluster/devpod/zai-proxy-dashboard.yml` (in `jedarden/declarative-config`).

## Ceiling persistence across restarts (2026-09-02)

The rate ceiling is deliberately unknown and inferred from the observed 429 rate; the
operator's goal is to saturate a prepaid plan, so the estimate is the proxy's most valuable
state. It is also the only state that is lost on every restart: the container logs for
2026-09-02 03:00-04:15 UTC show 13 starts in 62 minutes, each logging
`Ceiling updated 40.00 -> 30.40` and then a 40-60% 429 burst while re-learning from
RATE_LIMIT_MAX. Persist `{ceiling, hold, ts}` on every update to a state file on an
emptyDir volume (survives container restarts, which is the case that matters) and resume
from it on start when it is younger than a configurable age; keep the probe-for-shift
logic so the estimate still drifts up when upstream loosens. Bead: zaiproxy-cb072626.
