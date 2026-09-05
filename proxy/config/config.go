// Package config provides centralized configuration defaults and environment
// variable parsing helpers for the zai-proxy.
package config

import (
	"time"

	"git.ardenone.com/jedarden/zai-proxy/internal/configenv"
	"git.ardenone.com/jedarden/zai-proxy/proxy/quota"
)

// =============================================================================
// Default Configuration Constants
// =============================================================================

// API defaults
const (
	// DefaultTargetURL is the default Z.AI API endpoint.
	DefaultTargetURL = "https://api.z.ai/api/anthropic"
)

// Rate limiter defaults
const (
	// DefaultRateLimitInitial is the default initial rate limit (req/s).
	DefaultRateLimitInitial = 10.0
	// DefaultRateLimitMin is the default minimum rate limit (req/s).
	DefaultRateLimitMin = 1.0
	// DefaultRateLimitMax is the default maximum rate limit (req/s).
	DefaultRateLimitMax = 50.0
	// DefaultRateLimitCeilingAlpha is the default EWMA smoothing factor for ceiling estimation.
	DefaultRateLimitCeilingAlpha = 0.3
	// DefaultRateLimitHoldMargin is the default margin below ceiling to hold (fraction).
	DefaultRateLimitHoldMargin = 0.02
	// DefaultRateLimitProbeInterval is the default interval between ceiling probes (in clean windows).
	DefaultRateLimitProbeInterval = 10
)

// Quota polling defaults (observe-only; docs/plan/plan.md, "Quota-aware
// throttling plan"). Polling stays off until a deployment explicitly turns it
// on, because observe-only telemetry still produces an outbound monitor call
// carrying the account credential.
const (
	// DefaultQuotaPollEnabled keeps quota observation off by default.
	DefaultQuotaPollEnabled = false
	// DefaultQuotaPollInterval is the default out-of-band poll cadence.
	DefaultQuotaPollInterval = time.Minute
	// DefaultQuotaPollTimeout bounds one quota poll. It mirrors
	// quota.DefaultTimeout, which is the value the client would apply alone.
	DefaultQuotaPollTimeout = quota.DefaultTimeout
	// DefaultQuotaStaleAfter is how long a cached quota sample stays trusted
	// before /health and /metrics report it as stale.
	DefaultQuotaStaleAfter = 15 * time.Minute
)

// Rate limiter state persistence defaults
const (
	// DefaultRateLimitStateFile is the default path of the persisted ceiling
	// snapshot. In the cluster deployment this is an emptyDir volume, so the
	// snapshot survives container restarts (the case that matters) but not pod
	// rescheduling.
	DefaultRateLimitStateFile = "/var/lib/zai-proxy/ceiling.json"
	// DefaultRateLimitStateMaxAge is the default maximum age of a persisted
	// ceiling snapshot before a restart falls back to re-learning from
	// RATE_LIMIT_MAX.
	DefaultRateLimitStateMaxAge = 6 * time.Hour
)

// Worker defaults
const (
	// DefaultMaxWorkers is the default maximum number of concurrent workers.
	DefaultMaxWorkers = 10
	// DefaultMaxRetries is the default maximum number of retry attempts.
	DefaultMaxRetries = 3
)

// Feature defaults
const (
	// DefaultDeploymentVariant is the default deployment variant for metrics labeling.
	DefaultDeploymentVariant = "production"
	// DefaultTokenizerModel is the default model name for token metrics.
	DefaultTokenizerModel = "glm-4"
	// DefaultTokenCountingEnabled is the default state for token counting.
	DefaultTokenCountingEnabled = true
)

// =============================================================================
// Configuration Loaders
// =============================================================================

// GetDeploymentVariant returns the deployment variant from DEPLOYMENT_VARIANT env var,
// or the default value if not set.
func GetDeploymentVariant() string {
	return configenv.GetString("DEPLOYMENT_VARIANT", DefaultDeploymentVariant)
}

// GetTokenizerModel returns the tokenizer model name from TOKENIZER_MODEL env var,
// or the default value if not set.
func GetTokenizerModel() string {
	return configenv.GetString("TOKENIZER_MODEL", DefaultTokenizerModel)
}

// GetMaxWorkers returns the max workers from MAX_WORKERS env var,
// or the default value if not set/invalid.
func GetMaxWorkers() int64 {
	return configenv.GetPositiveInt64("MAX_WORKERS", DefaultMaxWorkers)
}

// GetMaxRetries returns the max retries from MAX_RETRIES env var,
// or the default value if not set/invalid.
func GetMaxRetries() int {
	return configenv.GetIntNonNegative("MAX_RETRIES", DefaultMaxRetries)
}

// GetTargetURL returns the Z.AI API target URL from ZAI_TARGET_URL env var,
// or the default value if not set.
func GetTargetURL() string {
	return configenv.GetString("ZAI_TARGET_URL", DefaultTargetURL)
}

// GetTokenCountingEnabled returns whether token counting is enabled from
// TOKEN_COUNTING_ENABLED env var, or the default value if not set.
func GetTokenCountingEnabled() bool {
	return configenv.GetBool("TOKEN_COUNTING_ENABLED", DefaultTokenCountingEnabled)
}

// GetRateLimitInitial returns the initial rate limit from RATE_LIMIT_INITIAL env var,
// or the default value if not set/invalid.
func GetRateLimitInitial() float64 {
	return configenv.GetPositiveFloat64("RATE_LIMIT_INITIAL", DefaultRateLimitInitial)
}

// GetRateLimitMin returns the minimum rate limit from RATE_LIMIT_MIN env var,
// or the default value if not set/invalid.
func GetRateLimitMin() float64 {
	return configenv.GetPositiveFloat64("RATE_LIMIT_MIN", DefaultRateLimitMin)
}

// GetRateLimitMax returns the maximum rate limit from RATE_LIMIT_MAX env var,
// or the default value if not set/invalid.
func GetRateLimitMax() float64 {
	return configenv.GetPositiveFloat64("RATE_LIMIT_MAX", DefaultRateLimitMax)
}

// GetRateLimitCeilingAlpha returns the ceiling smoothing factor from RATE_LIMIT_CEILING_ALPHA env var,
// or the default value if not set/invalid. Must be in (0, 1].
func GetRateLimitCeilingAlpha() float64 {
	return configenv.GetFloat64RangeExclusiveMin("RATE_LIMIT_CEILING_ALPHA", DefaultRateLimitCeilingAlpha, 0, 1)
}

// GetRateLimitHoldMargin returns the hold margin from RATE_LIMIT_HOLD_MARGIN env var,
// or the default value if not set/invalid. Must be in (0, 1).
func GetRateLimitHoldMargin() float64 {
	return configenv.GetFloat64RangeExclusiveBoth("RATE_LIMIT_HOLD_MARGIN", DefaultRateLimitHoldMargin, 0, 1)
}

// GetRateLimitProbeInterval returns the probe interval from RATE_LIMIT_PROBE_INTERVAL env var,
// or the default value if not set/invalid.
func GetRateLimitProbeInterval() int {
	return configenv.GetPositiveInt("RATE_LIMIT_PROBE_INTERVAL", DefaultRateLimitProbeInterval)
}

// GetRateLimitStateFile returns the path of the persisted ceiling snapshot from
// RATE_LIMIT_STATE_FILE env var, or the default value if not set.
func GetRateLimitStateFile() string {
	return configenv.GetString("RATE_LIMIT_STATE_FILE", DefaultRateLimitStateFile)
}

// GetRateLimitStateMaxAge returns the maximum age of a persisted ceiling
// snapshot from RATE_LIMIT_STATE_MAX_AGE env var, or the default value if not
// set/invalid. Snapshots older than this are ignored on startup.
func GetRateLimitStateMaxAge() time.Duration {
	return configenv.ParseDurationOrDefault("RATE_LIMIT_STATE_MAX_AGE", DefaultRateLimitStateMaxAge)
}

// GetQuotaPollEnabled reports whether out-of-band quota polling is enabled,
// from QUOTA_POLL_ENABLED env var. It defaults to false: quota observation is
// observe-only and still makes an authenticated monitor call.
func GetQuotaPollEnabled() bool {
	return configenv.GetBool("QUOTA_POLL_ENABLED", DefaultQuotaPollEnabled)
}

// GetQuotaBaseURL returns the quota endpoint origin from ZAI_QUOTA_BASE_URL
// env var, or the default value if not set. It shares the client's variable
// so the proxy cannot drift from the endpoint the quota package documents.
func GetQuotaBaseURL() string {
	return configenv.GetString(quota.EnvBaseURL, quota.DefaultBaseURL)
}

// GetQuotaPollInterval returns the out-of-band poll cadence from
// QUOTA_POLL_INTERVAL env var, or the default value if not set. A cadence
// that is not positive would spin the poller, so it falls back to the
// default instead of reaching it.
func GetQuotaPollInterval() time.Duration {
	return positiveDurationOrDefault("QUOTA_POLL_INTERVAL", DefaultQuotaPollInterval)
}

// GetQuotaPollTimeout returns the per-poll budget from QUOTA_POLL_TIMEOUT
// env var, or the default value if not set. This is the same variable the
// quota client applies on its own; reading it here keeps the poller's
// timeout in the one configuration surface the deployment tunes.
func GetQuotaPollTimeout() time.Duration {
	return positiveDurationOrDefault(quota.EnvTimeout, DefaultQuotaPollTimeout)
}

// GetQuotaStaleAfter returns how long a cached quota sample stays trusted
// from QUOTA_STALE_AFTER env var, or the default value if not set. Beyond it
// the poller reports the sample stale and keeps the last-known-good state.
func GetQuotaStaleAfter() time.Duration {
	return positiveDurationOrDefault("QUOTA_STALE_AFTER", DefaultQuotaStaleAfter)
}

// positiveDurationOrDefault parses key as a Go duration, returning def when
// the variable is unset, unparsable, or not positive. Durations guarded this
// way bound a loop, so a non-positive value can never reach it.
func positiveDurationOrDefault(key string, def time.Duration) time.Duration {
	if parsed := configenv.ParseDurationOrDefault(key, 0); parsed > 0 {
		return parsed
	}
	return def
}
