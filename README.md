# zai-proxy

LLM reverse proxy and metrics dashboard for the [Z.AI](https://z.ai) API.

## Components

### proxy/

OpenAI-compatible reverse proxy that fronts the Z.AI API. Features:

- Request/response body parsing and token counting (tiktoken + GLM tokenizers)
- Rate limiting with configurable burst and steady-state limits
- Prometheus metrics export
- Blue/green and canary deployment support
- Translation layer for provider-specific request/response formats

See [proxy/README.md](proxy/README.md) for setup and configuration.

### dashboard/

Go backend + React frontend for visualizing proxy metrics, token usage, and request history.

- Collects metrics from the proxy's Prometheus endpoint
- Stores aggregated data in SQLite
- Serves a Tailwind/Vite frontend via SSE for live updates

See [dashboard docs](docs/notes/) for deployment and monitoring.

## Docs

- [`docs/plan/`](docs/plan/) — architecture decisions and roadmaps
- [`docs/notes/`](docs/notes/) — deployment, operations, monitoring, canary procedures
- [`docs/research/`](docs/research/) — tokenizer research, metrics references

## Git remotes

- Forgejo (primary): `https://git.ardenone.com/jedarden/zai-proxy.git`
