/**
 * TypeScript types matching the backend Go model (model/metrics.go)
 */

/** Metric snapshot from a single scrape */
export interface MetricSnapshot {
  timestamp: number; // Unix timestamp in milliseconds
  variant: 'production' | 'canary';
  requests_2xx: number;
  requests_4xx: number;
  requests_5xx: number;
  tokens_input: number;
  tokens_output: number;
  concurrent_requests: number;
  max_workers: number;
  rate_limit_rps: number;
  rate_limit_rejections: number;
  rate_limit_adj_increase: number;
  rate_limit_adj_decrease: number;
  upstream_errors: number;
  retry_attempts: number;
  latency_p50: number; // milliseconds
  latency_p95: number;
  latency_p99: number;
  request_size_avg: number; // bytes
  response_size_avg: number;
  token_rate_in: number; // tokens/s
  token_rate_out: number;
  req_rate: number; // requests/s
  error_rate_pct: number; // percentage
  worker_utilization: number; // ratio 0-1
  status_code_rates?: Record<string, number>; // Per-status-code request rates (req/s)
}

/** Health status of a single variant */
export interface VariantStatus {
  healthy: boolean;
  last_scrape: string; // ISO timestamp
  req_rate: number;
  error_rate_pct: number;
  latency_p50_ms: number;
  concurrent: number;
  worker_utilization: number;
  rate_limit_rps: number;
  token_rate_in: number;
  token_rate_out: number;
}

/** Response from /api/status */
export interface StatusResponse {
  production?: VariantStatus;
  canary?: VariantStatus;
}

/** SSE message from server */
export interface SSEMessage {
  type: 'metrics' | 'connected' | 'error';
  data?: MetricSnapshot;
  scrape_interval?: number;
  variants?: string[];
}

/** Time range options */
export type TimeRange = '5m' | '15m' | '1h' | '6h' | '24h';

/** Variant filter options */
export type VariantFilter = 'production' | 'canary' | 'both';

/** Time range to milliseconds mapping */
export const TIME_RANGE_MS: Record<TimeRange, number> = {
  '5m': 5 * 60 * 1000,
  '15m': 15 * 60 * 1000,
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
};

/** API query parameters for /api/metrics */
export interface MetricsQueryParams {
  range?: TimeRange;
  variant?: VariantFilter;
  fields?: string[];
}

/** Chart data point with timestamp */
export interface ChartDataPoint {
  timestamp: number;
  time: string; // Formatted time for display
  [key: string]: number | string;
}
