# Dashboard API Reference

This document describes the REST and SSE (Server-Sent Events) API endpoints exposed by the zai-proxy dashboard.

## Base URL

All API endpoints are relative to the dashboard base URL:

```
http://<dashboard-host>:8080
```

The default port is `8080` but can be configured via the `LISTEN_ADDR` environment variable.

---

## CORS

All API endpoints set `Access-Control-Allow-Origin: *`, enabling cross-origin requests from any origin.

---

## Endpoints

### 1. GET /healthz

Health check endpoint that returns the operational status of the dashboard service.

#### Request

```http
GET /healthz
```

No parameters or headers required.

#### Response

**Status Code:** `200 OK`

**Content-Type:** `application/json`

```json
{
  "status": "ok"
}
```

#### Example

```bash
curl http://localhost:8080/healthz
```

#### Error Responses

This endpoint does not return errors. A `200 OK` response indicates the service is operational.

---

### 2. GET /api/config

Returns dashboard configuration including scrape interval and target endpoints.

#### Request

```http
GET /api/config
```

No parameters required.

#### Response

**Status Code:** `200 OK`

**Content-Type:** `application/json`

```json
{
  "scrape_interval": 5,
  "targets": [
    "http://zai-proxy.mcp.svc.cluster.local:8080/metrics"
  ]
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `scrape_interval` | number | Scrape interval in seconds (default: 5) |
| `targets` | string[] | Array of Prometheus metrics endpoint URLs to scrape |

#### Configuration Sources

- `LISTEN_ADDR` environment variable (default: `:8080`)
- `SCRAPE_INTERVAL` environment variable (default: `5s`)
- `SCRAPE_TARGETS` environment variable (comma-separated URLs, default: `http://zai-proxy.mcp.svc.cluster.local:8080/metrics`)

#### Example

```bash
curl http://localhost:8080/api/config
```

#### Error Responses

This endpoint does not return errors under normal operation.

---

### 3. GET /api/status

Returns the latest health snapshot for each variant (production and/or canary). This endpoint provides a point-in-time status summary of all monitored proxy instances.

#### Request

```http
GET /api/status
```

No parameters required.

#### Response

**Status Code:** `200 OK`

**Content-Type:** `application/json`

```json
{
  "production": {
    "healthy": true,
    "last_scrape": "2026-06-21T10:30:00Z",
    "req_rate": 42.5,
    "error_rate_pct": 0.12,
    "latency_p50_ms": 145,
    "concurrent": 8,
    "worker_utilization": 0.32,
    "rate_limit_rps": 100,
    "token_rate_in": 1250.5,
    "token_rate_out": 3800.2
  },
  "canary": {
    "healthy": true,
    "last_scrape": "2026-06-21T10:30:00Z",
    "req_rate": 2.1,
    "error_rate_pct": 0.05,
    "latency_p50_ms": 138,
    "concurrent": 1,
    "worker_utilization": 0.08,
    "rate_limit_rps": 100,
    "token_rate_in": 75.3,
    "token_rate_out": 220.7
  }
}
```

#### Response Fields

Each variant object contains:

| Field | Type | Description |
|-------|------|-------------|
| `healthy` | boolean | Overall health status (true if data exists) |
| `last_scrape` | string | ISO 8601 timestamp of last successful scrape |
| `req_rate` | number | Current request rate (requests/second) |
| `error_rate_pct` | number | Error rate percentage (0-100) |
| `latency_p50_ms` | number | Median request latency in milliseconds |
| `concurrent` | number | Current number of concurrent requests |
| `worker_utilization` | number | Worker pool utilization ratio (0-1) |
| `rate_limit_rps` | number | Current rate limit in requests/second |
| `token_rate_in` | number | Input token rate (tokens/second) |
| `token_rate_out` | number | Output token rate (tokens/second) |

#### Example

```bash
curl http://localhost:8080/api/status
```

#### Error Responses

| Status Code | Description |
|-------------|-------------|
| `500 Internal Server Error` | Failed to retrieve status from storage |

---

### 4. GET /api/metrics

Returns historical metrics for a specified time range and variant. This is the primary endpoint for querying historical performance data.

#### Request

```http
GET /api/metrics?range={range}&variant={variant}
```

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `range` | string | No | `1h` | Time range for historical data |
| `variant` | string | No | `all` | Variant filter: `production`, `canary`, or `all` |

#### Supported Range Values

- `5m` - 5 minutes
- `15m` - 15 minutes
- `1h` - 1 hour (default)
- `6h` - 6 hours
- `24h` - 24 hours
- `7d` - 7 days

Custom duration strings are also supported via Go's `time.ParseDuration` format (e.g., `2h30m`, `45m`).

#### Data Resolution

- **≤ 1 hour**: Data returned from `metrics_5s` table (5-second granularity)
- **> 1 hour**: Data returned from `metrics_1m` table (1-minute downsampled granularity)

#### Response

**Status Code:** `200 OK`

**Content-Type:** `application/json`

Returns a JSON array of `MetricSnapshot` objects:

```json
[
  {
    "timestamp": 1766425000000,
    "variant": "production",
    "requests_2xx": 1250,
    "requests_4xx": 5,
    "requests_5xx": 2,
    "tokens_input": 15000,
    "tokens_output": 45000,
    "tokens_cache_read": 3000,
    "tokens_cache_write": 1200,
    "concurrent_requests": 8,
    "max_workers": 25,
    "rate_limit_rps": 100,
    "rate_limit_rejections": 0,
    "rate_limit_adj_increase": 0,
    "rate_limit_adj_decrease": 0,
    "upstream_errors": 1,
    "retry_attempts": 2,
    "latency_p50": 145,
    "latency_p95": 280,
    "latency_p99": 450,
    "request_size_avg": 1024,
    "response_size_avg": 8192,
    "token_rate_in": 1250.5,
    "token_rate_out": 3800.2,
    "token_rate_cache_read": 250.3,
    "token_rate_cache_write": 100.1,
    "req_rate": 42.5,
    "error_rate_pct": 0.12,
    "worker_utilization": 0.32,
    "status_code_rates": {
      "200": 40.2,
      "201": 2.0,
      "400": 0.3,
      "429": 0.1,
      "500": 0.1
    }
  },
  ...
]
```

#### MetricSnapshot Fields

| Field | Type | Description |
|-------|------|-------------|
| `timestamp` | number | Unix timestamp in milliseconds |
| `variant` | string | Variant name: `production` or `canary` |
| `requests_2xx` | number | Cumulative count of 2xx responses |
| `requests_4xx` | number | Cumulative count of 4xx responses |
| `requests_5xx` | number | Cumulative count of 5xx responses |
| `tokens_input` | number | Cumulative input tokens processed |
| `tokens_output` | number | Cumulative output tokens processed |
| `tokens_cache_read` | number | Cumulative cache-read tokens |
| `tokens_cache_write` | number | Cumulative cache-write tokens |
| `concurrent_requests` | number | Current concurrent requests |
| `max_workers` | number | Maximum worker pool size |
| `rate_limit_rps` | number | Current rate limit (requests/second) |
| `rate_limit_rejections` | number | Cumulative rate limit rejections |
| `rate_limit_adj_increase` | number | Cumulative rate limit increases |
| `rate_limit_adj_decrease` | number | Cumulative rate limit decreases |
| `upstream_errors` | number | Cumulative upstream errors |
| `retry_attempts` | number | Cumulative retry attempts |
| `latency_p50` | number | Median latency in milliseconds |
| `latency_p95` | number | 95th percentile latency in milliseconds |
| `latency_p99` | number | 99th percentile latency in milliseconds |
| `request_size_avg` | number | Average request size in bytes |
| `response_size_avg` | number | Average response size in bytes |
| `token_rate_in` | number | Input token rate (tokens/second) |
| `token_rate_out` | number | Output token rate (tokens/second) |
| `token_rate_cache_read` | number | Cache-read token rate (tokens/second) |
| `token_rate_cache_write` | number | Cache-write token rate (tokens/second) |
| `req_rate` | number | Request rate (requests/second) |
| `error_rate_pct` | number | Error rate percentage (0-100) |
| `worker_utilization` | number | Worker utilization ratio (0-1) |
| `status_code_rates` | object | Per-status-code request rates (requests/second) |

#### Examples

```bash
# Last 5 minutes, all variants
curl "http://localhost:8080/api/metrics?range=5m&variant=all"

# Last hour, production only
curl "http://localhost:8080/api/metrics?range=1h&variant=production"

# Last 24 hours, canary only
curl "http://localhost:8080/api/metrics?range=24h&variant=canary"

# Last 7 days, all variants
curl "http://localhost:8080/api/metrics?range=7d&variant=all"

# Custom duration (2 hours 30 minutes)
curl "http://localhost:8080/api/metrics?range=2h30m&variant=production"
```

#### Error Responses

| Status Code | Description |
|-------------|-------------|
| `400 Bad Request` | Invalid `range` parameter format |
| `500 Internal Server Error` | Failed to query metrics from storage |

---

### 5. GET /api/events

Server-Sent Events (SSE) endpoint for real-time streaming of metric updates. This endpoint provides a persistent connection for receiving live metrics as they are scraped.

#### Request

```http
GET /api/events
```

No query parameters required.

#### Request Headers

| Header | Value |
|--------|-------|
| `Accept` | `text/event-stream` |
| `Cache-Control` | `no-cache` |

#### Response

**Status Code:** `200 OK`

**Content-Type:** `text/event-stream`

**Response Headers:**

| Header | Value |
|--------|-------|
| `Content-Type` | `text/event-stream` |
| `Cache-Control` | `no-cache` |
| `Connection` | `keep-alive` |
| `X-Accel-Buffering` | `no` |

#### SSE Event Format

Events are sent in the standard SSE format:

```
data: <json-payload>\n\n
```

#### Event Types

##### 1. `connected` (Initial Event)

Sent immediately upon connection establishment.

```json
{
  "type": "connected",
  "scrape_interval": 5,
  "variants": ["production", "canary"]
}
```

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Event type: `connected` |
| `scrape_interval` | number | Scrape interval in seconds |
| `variants` | string[] | Available variant names |

##### 2. `metrics` (Ongoing Events)

Sent for each new metric snapshot as it arrives (typically every scrape interval).

```json
{
  "type": "metrics",
  "data": {
    "timestamp": 1766425000000,
    "variant": "production",
    "requests_2xx": 1250,
    ...
  }
}
```

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Event type: `metrics` |
| `data` | object | Full `MetricSnapshot` object (see `/api/metrics` response) |

##### 3. Heartbeat (Keepalive)

A comment sent every 30 seconds to keep the connection alive:

```
: heartbeat\n\n
```

This is an SSE comment (prefixed with `:`) and should be ignored by clients but used to detect stale connections.

#### Connection Behavior

- **Persistent Connection:** The connection remains open until the client disconnects or the server terminates.
- **Automatic Reconnect:** Clients should implement reconnection logic with exponential backoff.
- **Slow Client Detection:** Clients with full send buffers are automatically disconnected to prevent backpressure buildup.
- **Broadcast Capacity:** 100-message buffer for the broadcast channel.
- **Concurrent Clients:** No hard limit (limited by system resources).
- **HTTP/2 Compatible:** Works with HTTP/2 and cloudflared tunnels (uses standard HTTP GET, not WebSocket upgrade).

#### Server-Side Implementation

The SSE endpoint is implemented via a broadcast hub (`/dashboard/api/sse.go`):

- **Broadcast Channel:** Buffered channel (capacity: 100) for metric snapshots
- **Client Registration:** Concurrent-safe client registration/unregistration
- **Slow Client Protection:** Clients with full send buffers are automatically disconnected
- **Heartbeat:** Sent every 30 seconds to keep connections alive
- **Client Context:** Disconnections detected via `r.Context().Done()`

#### Example Clients

**JavaScript (Browser):**

```javascript
const eventSource = new EventSource('http://localhost:8080/api/events');

eventSource.onmessage = (event) => {
  const message = JSON.parse(event.data);
  
  if (message.type === 'connected') {
    console.log('Connected to SSE stream');
    console.log('Scrape interval:', message.scrape_interval);
  } else if (message.type === 'metrics') {
    console.log('New metrics:', message.data);
    // Update your UI with the new metrics
  }
};

eventSource.onerror = (error) => {
  console.error('SSE connection error:', error);
  // Implement reconnection logic
};
```

**cURL (for testing):**

```bash
curl -N \
  -H "Accept: text/event-stream" \
  http://localhost:8080/api/events
```

**Go:**

```go
resp, err := http.Get("http://localhost:8080/api/events")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix(line, "data: ") {
        data := strings.TrimPrefix(line, "data: ")
        var msg model.SSEMessage
        json.Unmarshal([]byte(data), &msg)
        // Handle message
    }
}
```

#### Error Responses

| Status Code | Description |
|-------------|-------------|
| `500 Internal Server Error` | Streaming not supported (server doesn't implement `http.Flusher`) |

---

## Data Retention

The dashboard stores metrics at two granularities with different retention policies:

| Table | Granularity | Retention | Purpose |
|-------|-------------|-----------|---------|
| `metrics_5s` | 5 seconds | 24 hours | High-resolution recent data |
| `metrics_1m` | 1 minute | 7 days | Downsampled historical data |

### Automatic Operations

- **Downsampling:** Runs every 10 minutes, aggregates 5-second data into 1-minute averages
- **Cleanup:** Runs every hour, deletes data beyond retention windows
- **WAL Mode:** Enabled for performance and concurrency

### Storage Backend

**Database:** SQLite with WAL mode

**Path:** Configured via `DB_PATH` environment variable (default: `/data/dashboard.db`)

**Write Strategy:** Asynchronous writes via buffered channel (capacity: 1000)

**Query Strategy:** Time-based table selection
- Queries > 1 hour use `metrics_1m` (downsampled)
- Queries ≤ 1 hour use `metrics_5s` (high-resolution)

Data is automatically downsampled from 5-second to 1-minute granularity every 10 minutes. Old data beyond retention periods is automatically cleaned up.

---

## Common Error Response Format

All error responses return plain text with a descriptive message:

```http
HTTP/1.1 400 Bad Request
Content-Type: text/plain

invalid range parameter
```

---

## Rate Limiting

The dashboard API does not implement rate limiting. However, clients should:

1. **Poll `/api/metrics` sparingly** - Use SSE (`/api/events`) for real-time updates instead of polling
2. **Cache `/api/config` responses** - Configuration changes infrequently
3. **Use appropriate `range` values** - Don't request more data than needed

---

## Browser Usage

All endpoints support CORS, enabling direct browser access:

```javascript
// Fetch status
const response = await fetch('http://localhost:8080/api/status');
const status = await response.json();

// Fetch metrics
const response = await fetch('http://localhost:8080/api/metrics?range=1h&variant=production');
const metrics = await response.json();

// SSE stream
const eventSource = new EventSource('http://localhost:8080/api/events');
```

---

## Related Documentation

- [Token Counting](./TOKEN_COUNTING.md) - Detailed token metrics explanation
- [Monitoring Setup](./MONITORING_SETUP.md) - Dashboard deployment and configuration
- [Canary Promotion Procedure](./CANARY_PROMOTION_PROCEDURE.md) - Canary deployment workflow
- [Deployment Guide](./DEPLOYMENT.md) - Production deployment instructions
