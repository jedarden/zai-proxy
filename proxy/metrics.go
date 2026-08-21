package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type tokenPricingRates struct {
	input      float64
	output     float64
	cacheRead  float64
	cacheWrite float64
}

const (
	// Z.AI's GLM-4.7 public API rates, converted from USD per 1M to USD per
	// 1K tokens. Source: https://docs.z.ai/guides/overview/pricing (accessed
	// 2026-08-20). Cached-input storage (cache writes) was listed as
	// limited-time free on that date.
	offPeakInputUSDPer1K      = 0.00060
	offPeakOutputUSDPer1K     = 0.00220
	offPeakCacheReadUSDPer1K  = 0.00011
	offPeakCacheWriteUSDPer1K = 0.0
)

// tokenPricingRatesUSDPer1K holds the estimated Z.AI GLM-4.7 API rates by
// pricing tier. Peak rates are 2x off-peak rates, matching GetPricingTier.
var tokenPricingRatesUSDPer1K = map[string]tokenPricingRates{
	"off_peak": {
		input:      offPeakInputUSDPer1K,
		output:     offPeakOutputUSDPer1K,
		cacheRead:  offPeakCacheReadUSDPer1K,
		cacheWrite: offPeakCacheWriteUSDPer1K,
	},
	"peak": {
		input:      offPeakInputUSDPer1K * 2,
		output:     offPeakOutputUSDPer1K * 2,
		cacheRead:  offPeakCacheReadUSDPer1K * 2,
		cacheWrite: offPeakCacheWriteUSDPer1K * 2,
	},
}

var (
	// Request metrics
	requestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_requests_total",
			Help: "Total number of requests by method, path, status code, and variant",
		},
		[]string{"method", "path", "status_code", "variant"},
	)

	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zai_proxy_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		},
		[]string{"method", "path", "status_code", "variant"},
	)

	requestSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zai_proxy_request_size_bytes",
			Help:    "Request size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8),
		},
		[]string{"method", "path", "variant"},
	)

	responseSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zai_proxy_response_size_bytes",
			Help:    "Response size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8),
		},
		[]string{"method", "path", "status_code", "variant"},
	)

	concurrentRequests = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_concurrent_requests",
			Help: "Number of requests currently being processed",
		},
		[]string{"variant"},
	)

	upstreamErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_upstream_errors_total",
			Help: "Total number of upstream errors by type",
		},
		[]string{"error_type", "variant"},
	)

	maxWorkers = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_max_workers",
			Help: "Maximum number of concurrent workers allowed",
		},
		[]string{"variant"},
	)

	workerUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_worker_utilization_ratio",
			Help: "Current worker utilization ratio (concurrent_requests/max_workers)",
		},
		[]string{"variant"},
	)

	// Rate limiting metrics
	rateLimitCurrentRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_rate_limit_requests_per_second",
			Help: "Current rate limit in requests per second",
		},
		[]string{"variant"},
	)

	rateLimitWaitTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zai_proxy_rate_limit_wait_seconds",
			Help:    "Time spent waiting for rate limiter",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10},
		},
		[]string{"variant"},
	)

	rateLimitAdjustments = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_rate_limit_adjustments_total",
			Help: "Total number of rate limit adjustments",
		},
		[]string{"direction", "variant"}, // "increase" or "decrease"
	)

	rateLimitRejections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_rate_limit_rejections_total",
			Help: "Total number of requests rejected due to rate limiting",
		},
		[]string{"variant"},
	)

	retryAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_retry_attempts_total",
			Help: "Total number of retry attempts",
		},
		[]string{"reason", "variant"}, // "429" or "network_error"
	)

	// Token counting metrics
	tokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_tokens_total",
			Help: "Total number of tokens processed by direction (input/output), model, deployment variant, and pricing tier",
		},
		[]string{"direction", "model", "variant", "pricing_tier"},
	)

	estimatedCostUSDTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_estimated_cost_usd_total",
			Help: "Estimated Z.AI API cost in USD by token direction, model, deployment variant, and pricing tier",
		},
		[]string{"direction", "model", "variant", "pricing_tier"},
	)

	tokenCountDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zai_proxy_token_count_duration_seconds",
			Help:    "Duration of token counting operations",
			Buckets: []float64{.0001, .0005, .001, .005, .01, .025, .05, .1},
		},
		[]string{"variant"},
	)

	// Token rate metrics - tracks tokens per second throughput
	// This measures how fast tokens are being processed (tokenization speed)
	tokenRateSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zai_proxy_token_rate_seconds",
			Help:    "Token processing rate histogram - tracks time taken to process tokens (lower is faster). Labels: direction (input/output), model (glm-4, etc.), variant (stable/canary)",
			Buckets: []float64{.00001, .00005, .0001, .0005, .001, .005, .01, .05, .1},
		},
		[]string{"direction", "model", "variant"},
	)

	// Alternative token rate metric - tokens per second throughput
	tokenRate = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zai_proxy_token_rate",
			Help:    "Token processing rate in tokens per second (throughput). Labels: direction (input/output), model (glm-4, etc.), variant (stable/canary)",
			Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000},
		},
		[]string{"direction", "model", "variant"},
	)

	// Build info metric for version tracking
	buildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_build_info",
			Help: "Build information including version, variant, commit, and build time",
		},
		[]string{"version", "variant", "commit", "build_time"},
	)
)

// GetPricingTier returns "peak" during 02:00-06:00 ET, "off_peak" otherwise.
// Z.AI Coding Plan: 1x off-peak, 2x peak.
func GetPricingTier() string {
	et := time.FixedZone("ET", -5*3600)
	hour := time.Now().In(et).Hour()
	if hour >= 2 && hour < 6 {
		return "peak"
	}
	return "off_peak"
}

// estimatedTokenCostUSD converts a token count to an estimated USD cost for a
// direction and pricing tier. Unknown directions or tiers deliberately return
// zero so a new label cannot cause the proxy to overstate spend.
func estimatedTokenCostUSD(direction, tier string, count int) float64 {
	if count <= 0 {
		return 0
	}

	rates, ok := tokenPricingRatesUSDPer1K[tier]
	if !ok {
		return 0
	}

	var rate float64
	switch direction {
	case "input":
		rate = rates.input
	case "output":
		rate = rates.output
	case "cache_read":
		rate = rates.cacheRead
	case "cache_write":
		rate = rates.cacheWrite
	default:
		return 0
	}

	return float64(count) / 1000 * rate
}

// recordTokenUsage records matching token and estimated-cost counters for one
// usage direction. A zero-cost direction still creates its cost series when
// tokens are observed, making the free rate explicit in Prometheus.
func recordTokenUsage(direction, model, variant, tier string, count int) {
	if count <= 0 {
		return
	}

	tokensTotal.WithLabelValues(direction, model, variant, tier).Add(float64(count))
	estimatedCostUSDTotal.WithLabelValues(direction, model, variant, tier).Add(estimatedTokenCostUSD(direction, tier, count))
}

// RecordInputTokens records input token count metrics
func RecordInputTokens(model string, version string, count int) {
	recordTokenUsage("input", model, version, GetPricingTier(), count)
}

// RecordOutputTokens records output token count metrics
func RecordOutputTokens(model string, version string, count int) {
	recordTokenUsage("output", model, version, GetPricingTier(), count)
}

// RecordTokenRate records token processing rate metrics
// This function records BOTH time-based and throughput-based token rate metrics
// Parameters:
//   - direction: "input" or "output"
//   - model: tokenizer model name (e.g., "glm-4", "claude-3")
//   - version: deployment variant ("stable" or "canary")
//   - duration: time taken to process the tokens
//   - tokenCount: number of tokens processed
func RecordTokenRate(direction string, model string, version string, duration time.Duration, tokenCount int) {
	if tokenCount <= 0 || duration <= 0 {
		return
	}

	// Record time-based metric (seconds taken to process tokens)
	tokenRateSeconds.WithLabelValues(direction, model, version).Observe(duration.Seconds())

	// Record throughput-based metric (tokens per second)
	tokensPerSecond := float64(tokenCount) / duration.Seconds()
	tokenRate.WithLabelValues(direction, model, version).Observe(tokensPerSecond)
}

// RecordInputTokenRate records input token processing rate
// This is a convenience wrapper for RecordTokenRate with direction="input"
func RecordInputTokenRate(model string, version string, duration time.Duration, tokenCount int) {
	RecordTokenRate("input", model, version, duration, tokenCount)
}

// RecordOutputTokenRate records output token processing rate
// This is a convenience wrapper for RecordTokenRate with direction="output"
func RecordOutputTokenRate(model string, version string, duration time.Duration, tokenCount int) {
	RecordTokenRate("output", model, version, duration, tokenCount)
}

// RecordUsage records all four token counts from a UsageData in a single call.
// Directions: "input", "output", "cache_read", "cache_write".
func RecordUsage(model, variant string, usage UsageData) {
	tier := GetPricingTier()
	recordTokenUsage("input", model, variant, tier, usage.InputTokens)
	recordTokenUsage("output", model, variant, tier, usage.OutputTokens)
	recordTokenUsage("cache_read", model, variant, tier, usage.CacheReadTokens)
	recordTokenUsage("cache_write", model, variant, tier, usage.CacheWriteTokens)
}
