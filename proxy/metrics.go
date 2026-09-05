package main

import (
	"math"
	"strings"
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
			Help:    "Time spent waiting for the rate limiter, by bounded source bucket",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10},
		},
		[]string{"variant", "client"},
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
			Help: "Total number of requests rejected due to proxy capacity, by bounded source bucket",
		},
		[]string{"variant", "client"},
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

	// Quota supervisor metrics (docs/plan/plan.md, "Rate-limiter metrics").
	// All labels are bounded: window and poll-result values come from fixed
	// enums, and limit_type/variant values are sanitized and length-capped so
	// provider payloads, credentials, account identifiers, or raw model
	// strings can never reach a label. These are registered through
	// registerQuotaCollector so duplicate initialization stays safe.
	quotaUsageRatio = registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_usage_ratio",
			Help: "Normalized provider-reported quota usage ratio by window and limit type",
		},
		[]string{"window", "limit_type", "variant"},
	))

	quotaRemainingRatio = registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_remaining_ratio",
			Help: "Remaining fraction of the provider-reported quota after no local policy adjustment",
		},
		[]string{"window", "limit_type", "variant"},
	))

	quotaResetTimeSeconds = registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_reset_time_seconds",
			Help: "Provider quota reset time as a Unix timestamp",
		},
		[]string{"window", "limit_type", "variant"},
	))

	quotaRateCap = registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_rate_cap",
			Help: "Account-level admission rate cap derived from quota pacing",
		},
		[]string{"variant"},
	))

	quotaGateOpen = registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_gate_open",
			Help: "Whether confirmed quota exhaustion is rejecting new work",
		},
		[]string{"window", "variant"},
	))

	quotaSampleAgeSeconds = registerQuotaCollector(prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zai_proxy_quota_sample_age_seconds",
			Help: "Age of the last valid account quota sample",
		},
		[]string{"variant"},
	))

	quotaPollsTotal = registerQuotaCollector(prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zai_proxy_quota_poll_total",
			Help: "Quota poll outcomes by result (success, error, malformed, stale)",
		},
		[]string{"result", "variant"},
	))
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

const quotaLabelValueMaxLength = 32

var (
	// quotaWindows is the bounded set of provider quota windows this proxy
	// tracks, per docs/plan/plan.md.
	quotaWindows = map[string]struct{}{
		"five_hour": {},
		"weekly":    {},
	}

	// quotaPollResults is the bounded set of documented poll outcomes.
	quotaPollResults = map[string]struct{}{
		"success":   {},
		"error":     {},
		"malformed": {},
		"stale":     {},
	}
)

// registerQuotaCollector registers c with the default Prometheus registry and
// returns c. When a collector with an identical definition is already
// registered — which duplicate initialization in tests can produce — it
// returns the existing collector instead of failing. A definition conflict
// (same name, different help or labels) is a programming error and stays
// fatal.
func registerQuotaCollector[T prometheus.Collector](c T) T {
	err := prometheus.DefaultRegisterer.Register(c)
	if err == nil {
		return c
	}

	already, ok := err.(prometheus.AlreadyRegisteredError)
	if !ok {
		panic(err)
	}

	existing, ok := already.ExistingCollector.(T)
	if !ok {
		panic(err)
	}

	return existing
}

// sanitizeQuotaLabel bounds a label value to a fixed charset and length so
// unbounded provider or configuration strings cannot inflate Prometheus
// cardinality or smuggle payloads into labels. Lowercase letters, digits,
// "-", and "_" survive; every other rune collapses to "_"; values longer
// than 32 characters truncate; an empty value becomes "unknown".
func sanitizeQuotaLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))

	var kept strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			kept.WriteRune(r)
		default:
			kept.WriteByte('_')
		}
	}

	label := kept.String()
	if len(label) > quotaLabelValueMaxLength {
		label = label[:quotaLabelValueMaxLength]
	}
	if label == "" {
		return "unknown"
	}
	return label
}

// quotaWindowLabel maps a provider window onto the documented bounded enum,
// collapsing unrecognized windows into "unknown".
func quotaWindowLabel(window string) string {
	label := sanitizeQuotaLabel(window)
	if _, ok := quotaWindows[label]; !ok {
		return "unknown"
	}
	return label
}

// quotaPollResultLabel maps a poll outcome onto the documented bounded enum.
// Unrecognized outcomes count as "error" so unanticipated provider behavior
// cannot silently extend the label set.
func quotaPollResultLabel(result string) string {
	label := sanitizeQuotaLabel(result)
	if _, ok := quotaPollResults[label]; !ok {
		return "error"
	}
	return label
}

// exportableQuotaValue reports whether a provider-provided number is safe to
// export. NaN and infinities are dropped rather than exported as gauges.
func exportableQuotaValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// quotaLabels normalizes the observation labels shared by the usage-ratio,
// remaining-ratio, and reset-time metrics.
func quotaLabels(window, limitType, variant string) (string, string, string) {
	return quotaWindowLabel(window), sanitizeQuotaLabel(limitType), sanitizeQuotaLabel(variant)
}

// RecordQuotaUsageRatio records the normalized provider-reported usage ratio
// for one quota window. Ratios are recorded unclamped so an overdrawn window
// stays visible; non-finite values are dropped.
func RecordQuotaUsageRatio(window, limitType, variant string, ratio float64) {
	if !exportableQuotaValue(ratio) {
		return
	}
	w, lt, v := quotaLabels(window, limitType, variant)
	quotaUsageRatio.WithLabelValues(w, lt, v).Set(ratio)
}

// RecordQuotaRemainingRatio records the remaining fraction of one quota
// window after no local policy adjustment. Non-finite values are dropped.
func RecordQuotaRemainingRatio(window, limitType, variant string, ratio float64) {
	if !exportableQuotaValue(ratio) {
		return
	}
	w, lt, v := quotaLabels(window, limitType, variant)
	quotaRemainingRatio.WithLabelValues(w, lt, v).Set(ratio)
}

// RecordQuotaResetTime records the provider reset time for one quota window
// as a Unix timestamp. A zero time means no reset was advertised and nothing
// is recorded.
func RecordQuotaResetTime(window, limitType, variant string, reset time.Time) {
	if reset.IsZero() {
		return
	}
	w, lt, v := quotaLabels(window, limitType, variant)
	quotaResetTimeSeconds.WithLabelValues(w, lt, v).Set(float64(reset.Unix()))
}

// RecordQuotaRateCap records the account-level admission rate cap derived
// from quota pacing. Negative caps clamp to zero; non-finite values are
// dropped.
func RecordQuotaRateCap(variant string, requestsPerSecond float64) {
	if !exportableQuotaValue(requestsPerSecond) {
		return
	}
	if requestsPerSecond < 0 {
		requestsPerSecond = 0
	}
	quotaRateCap.WithLabelValues(sanitizeQuotaLabel(variant)).Set(requestsPerSecond)
}

// RecordQuotaGateOpen records whether confirmed quota exhaustion is currently
// rejecting new work for one window.
func RecordQuotaGateOpen(window, variant string, open bool) {
	value := 0.0
	if open {
		value = 1
	}
	quotaGateOpen.WithLabelValues(quotaWindowLabel(window), sanitizeQuotaLabel(variant)).Set(value)
}

// RecordQuotaSampleAge records the age of the last valid account-quota
// sample. Negative ages, which only clock skew can produce, clamp to zero.
func RecordQuotaSampleAge(variant string, age time.Duration) {
	if age < 0 {
		age = 0
	}
	quotaSampleAgeSeconds.WithLabelValues(sanitizeQuotaLabel(variant)).Set(age.Seconds())
}

// RecordQuotaPollOutcome counts one quota poll outcome.
func RecordQuotaPollOutcome(result, variant string) {
	quotaPollsTotal.WithLabelValues(quotaPollResultLabel(result), sanitizeQuotaLabel(variant)).Inc()
}
