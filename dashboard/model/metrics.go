// Package model defines the data structures for metrics snapshots.
package model

import (
	"encoding/json"
	"time"
)

// MetricSnapshot represents a single point-in-time collection of metrics
// from a zai-proxy instance.
type MetricSnapshot struct {
	Timestamp             int64   `json:"timestamp"`              // Unix timestamp in milliseconds
	Variant               string  `json:"variant"`                // "production" or "canary"
	Requests2xx           float64 `json:"requests_2xx"`           // Total 2xx requests
	Requests4xx           float64 `json:"requests_4xx"`           // Total 4xx requests
	Requests5xx           float64 `json:"requests_5xx"`           // Total 5xx requests
	TokensInput           float64 `json:"tokens_input"`           // Total input tokens
	TokensOutput          float64 `json:"tokens_output"`          // Total output tokens
	TokensCacheRead       float64 `json:"tokens_cache_read"`      // Total cache-read tokens
	TokensCacheWrite      float64 `json:"tokens_cache_write"`     // Total cache-write tokens
	ConcurrentRequests    float64 `json:"concurrent_requests"`    // Current concurrent requests
	MaxWorkers            float64 `json:"max_workers"`            // Maximum workers
	RateLimitRps          float64 `json:"rate_limit_rps"`         // Current rate limit (req/s)
	RateLimitRejections   float64 `json:"rate_limit_rejections"`  // Total rate limit rejections
	RateLimitAdjIncrease  float64 `json:"rate_limit_adj_increase"` // Total rate limit increases
	RateLimitAdjDecrease  float64 `json:"rate_limit_adj_decrease"` // Total rate limit decreases
	UpstreamErrors        float64 `json:"upstream_errors"`        // Total upstream errors
	RetryAttempts         float64 `json:"retry_attempts"`         // Total retry attempts
	LatencyP50            float64 `json:"latency_p50"`            // Request latency p50 (ms)
	LatencyP95            float64 `json:"latency_p95"`            // Request latency p95 (ms)
	LatencyP99            float64 `json:"latency_p99"`            // Request latency p99 (ms)
	RequestSizeAvg        float64 `json:"request_size_avg"`       // Average request size (bytes)
	ResponseSizeAvg       float64 `json:"response_size_avg"`      // Average response size (bytes)
	TokenRateIn           float64 `json:"token_rate_in"`           // Input token rate (tokens/s)
	TokenRateOut          float64 `json:"token_rate_out"`          // Output token rate (tokens/s)
	TokenRateCacheRead    float64 `json:"token_rate_cache_read"`   // Cache-read token rate (tokens/s)
	TokenRateCacheWrite   float64 `json:"token_rate_cache_write"`  // Cache-write token rate (tokens/s)
	ReqRate               float64 `json:"req_rate"`               // Request rate (req/s)
	ErrorRatePct          float64 `json:"error_rate_pct"`         // Error rate percentage
	WorkerUtilization     float64 `json:"worker_utilization"`     // Worker utilization ratio (0-1)
	StatusCodeRates       map[string]float64 `json:"status_code_rates,omitempty"` // Per-status-code request rates (req/s)
}

// ToJSON serializes the snapshot to JSON bytes.
func (s *MetricSnapshot) ToJSON() ([]byte, error) {
	return json.Marshal(s)
}

// FromJSON deserializes a snapshot from JSON bytes.
func FromJSON(data []byte) (*MetricSnapshot, error) {
	var s MetricSnapshot
	err := json.Unmarshal(data, &s)
	return &s, err
}

// VariantStatus represents the health status of a single variant.
type VariantStatus struct {
	Healthy          bool      `json:"healthy"`
	LastScrape       time.Time `json:"last_scrape"`
	ReqRate          float64   `json:"req_rate"`
	ErrorRatePct     float64   `json:"error_rate_pct"`
	LatencyP50Ms     float64   `json:"latency_p50_ms"`
	Concurrent       float64   `json:"concurrent"`
	WorkerUtilization float64  `json:"worker_utilization"`
	RateLimitRps     float64   `json:"rate_limit_rps"`
	TokenRateIn      float64   `json:"token_rate_in"`
	TokenRateOut     float64   `json:"token_rate_out"`
}

// StatusResponse is the response for /api/status.
type StatusResponse struct {
	Production *VariantStatus `json:"production,omitempty"`
	Canary     *VariantStatus `json:"canary,omitempty"`
}

// MarshalJSON serializes the StatusResponse to JSON.
func (r *StatusResponse) MarshalJSON() ([]byte, error) {
	type Alias StatusResponse
	return json.Marshal((*Alias)(r))
}

// SSEMessage represents a message sent over SSE.
type SSEMessage struct {
	Type string         `json:"type"`
	Data *MetricSnapshot `json:"data,omitempty"`
	// For "connected" messages
	ScrapeInterval int      `json:"scrape_interval,omitempty"`
	Variants       []string `json:"variants,omitempty"`
}

// HistogramBucket represents a single bucket in a Prometheus histogram.
type HistogramBucket struct {
	UpperBound float64
	Count      float64
}

// Histogram represents a parsed Prometheus histogram.
type Histogram struct {
	Buckets []HistogramBucket
	Sum     float64
	Count   float64
}
