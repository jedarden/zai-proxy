# ZAI Proxy Ecosystem — Plan

## Objective

Provide a stable, observable endpoint for LLM agents to access the Z.AI API without exposing the Z.AI API key as an environment variable or in any other plaintext form accessible to the calling process. The proxy is the sole keeper of the credential; agents authenticate via a shared secret (proxy API key) that carries no Z.AI billing rights on its own.

## Architecture

```
LLM Agent (Claude Code, NEEDLE worker, etc.)
    │
    │  POST /v1/chat/completions
    │  Authorization: Bearer <proxy-key>   ← agent's credential (not the Z.AI key)
    ▼
┌─────────────────────────────────────────────────────┐
│                    zai-proxy                        │
│                                                     │
│  • Validates proxy-key                              │
│  • Rewrites Authorization → Bearer <zai-api-key>   │
│  • Rate-limits (token bucket per key)               │
│  • Counts tokens (request + response)               │
│  • Records metrics (Prometheus)                     │
│  • Translates request/response format if needed     │
│                                                     │
└──────────────────┬──────────────────────────────────┘
                   │  HTTPS
                   ▼
           api.z.ai  (Z.AI upstream)
```

The Z.AI API key lives **only** as a Kubernetes Secret (sealed-secrets encrypted at rest, injected as an env var into the proxy pod only). No agent process, worker, or tool ever sees the upstream key.

## Components

### proxy/ — Reverse Proxy (Go)

The core component. Handles:

- **Credential isolation:** accepts `Authorization: Bearer <proxy-key>`, injects the real Z.AI key upstream. Proxy keys are hashed and stored in config; compromise of a proxy key cannot be used to bill or enumerate usage independently.
- **Token counting:** both request and response token counts via tiktoken (for OpenAI-compat models) and GLM tokenizer (for GLM series). Token counts feed the metrics pipeline.
- **Rate limiting:** configurable token-bucket per proxy key. Prevents a runaway agent from exhausting the Z.AI quota. Returns 429 when the bucket is empty.
- **Prometheus metrics:** exposes `/metrics` with request counts, latency histograms, token usage, error rates, and rate-limit hit counts.
- **Request/response translation:** normalises differences between the OpenAI wire format and Z.AI's dialect so agents using standard OpenAI client libraries work without modification.
- **Canary support:** runs two deployment variants (production + canary) simultaneously; traffic split is controlled by the Kubernetes service config, not the proxy itself.

### dashboard/ — Metrics Dashboard (Go + React)

The observability layer. Scrapes the proxy's Prometheus endpoint, persists aggregated data in SQLite, and serves a live React frontend via SSE.

Panels:
- Request rate (req/s)
- Token throughput (tokens/s, split by direction)
- Latency (p50/p95/p99)
- Error rate (4xx, 5xx, 429 broken out separately)
- Rate-limit hit rate
- Concurrency (in-flight requests)

## Telemetry & Error Tracking

### Token counting

Every request and response passes through the token counter before forwarding/returning. The proxy records:

| Metric | Labels |
|--------|--------|
| `zai_proxy_tokens_total` | `direction=request\|response`, `model`, `key_id` |
| `zai_proxy_request_duration_seconds` | `model`, `status_code`, `key_id` |
| `zai_proxy_requests_total` | `model`, `status_code`, `key_id` |

Token counts are also written to the response `X-Tokens-Used` header so the calling agent can track its own consumption without querying the dashboard.

### Error rate tracking

Upstream errors (4xx/5xx from Z.AI) are classified and exposed as:

| Metric | Description |
|--------|-------------|
| `zai_proxy_upstream_errors_total{code="429"}` | Rate-limit responses from Z.AI — indicates quota pressure |
| `zai_proxy_upstream_errors_total{code="5xx"}` | Z.AI server errors |
| `zai_proxy_upstream_errors_total{code="4xx"}` | Malformed requests, auth failures |
| `zai_proxy_rate_limited_total` | Requests dropped by the proxy's own rate limiter (before hitting Z.AI) |

429s from Z.AI are given special treatment: the proxy applies automatic back-off and surfaces a `Retry-After` header to the agent, giving agents a signal to pause rather than spin.

### Dashboard alerting targets (future)

- 429 rate from Z.AI > 5% of requests over 5m → alert (quota approaching)
- Proxy-side 429s > 10% → alert (agent is over rate limit)
- p95 latency > 10s → alert (upstream degradation)
- Error rate > 2% → alert

## Security Model

| Threat | Mitigation |
|--------|------------|
| Agent exfiltrates Z.AI key | Key never leaves proxy pod; not in agent env, not in logs, not in metrics |
| Proxy key compromise | Proxy key has no Z.AI billing rights; can be rotated without touching Z.AI |
| Log scraping | Z.AI key is never logged; proxy key is masked in access logs |
| Metric label leakage | `key_id` label is a hash, not the raw proxy key |
| Runaway agent burns quota | Per-key rate limiter + 429 back-off |
| Z.AI quota exhaustion | 429 counter triggers alerts before quota is fully consumed |

## Repository Layout

```
zai-proxy/                          (git.ardenone.com/jedarden/zai-proxy)
├── proxy/                          Go module: git.ardenone.com/jedarden/zai-proxy
│   ├── main.go                     HTTP server, routing, auth middleware
│   ├── translator.go               Request/response format translation
│   ├── bodyparser.go               Body parsing, streaming support
│   ├── tokenizer.go                Token counting (tiktoken + GLM)
│   ├── metrics.go                  Prometheus instrumentation
│   ├── evaluation/                 Offline eval harness
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

Both templates clone from the public `git.ardenone.com/jedarden/zai-proxy` repo (no auth required). Versions are read from `proxy/VERSION` and `dashboard/VERSION` respectively.

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

Both components deploy to the `devpod` namespace on `ardenone-cluster` via ArgoCD from `jedarden/declarative-config`.

Key manifests:
- `k8s/ardenone-cluster/devpod/zai-proxy.yml` — production Deployment + Service
- `k8s/ardenone-cluster/devpod/zai-proxy-v2.yml` — canary Deployment
- `k8s/ardenone-cluster/devpod/zai-proxy-canary-deployment.yml` — canary config
- `k8s/ardenone-cluster/devpod/zai-proxy-tailscale.yml` — Tailscale ingress
- `k8s/ardenone-cluster/devpod/zai-api-key.sealedsecret.yml` — encrypted Z.AI API key

The Z.AI API key flows: OpenBao → ESO ExternalSecret → K8s Secret → proxy pod env (read once at startup, never written to any metric, log, or response).

## Migration Status

- [x] Source extracted from `ardenone-cluster/containers/zai-proxy` → `proxy/`
- [x] Source extracted from `ardenone-cluster/containers/zai-proxy-dashboard` → `dashboard/`
- [x] Go module paths updated to `git.ardenone.com/jedarden/zai-proxy[/dashboard]`
- [x] Argo Workflow templates created (`zai-proxy-build`, `zai-proxy-dashboard-build`)
- [ ] Push new workflow templates to declarative-config (triggers ArgoCD sync)
- [ ] Update CLAUDE.md / ardenone-cluster README to point to new repo
- [ ] Retire `ardenone-cluster/containers/zai-proxy` and `containers/zai-proxy-dashboard` once builds verified from new repo
