// Package config provides centralized configuration defaults and environment
// variable parsing helpers for the zai-proxy.
package config

import (
	"os"
	"strconv"
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
// Environment Variable Parsing Helpers
// =============================================================================

// GetString retrieves an environment variable or returns the default value.
func GetString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetInt retrieves an environment variable as an int, or returns the default value.
func GetInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// GetInt64 retrieves an environment variable as an int64, or returns the default value.
func GetInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// GetFloat64 retrieves an environment variable as a float64, or returns the default value.
func GetFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// GetBool retrieves an environment variable as a bool, or returns the default value.
// Accepts "false", "0", "no", "n" as false; everything else is true.
func GetBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		switch value {
		case "false", "0", "no", "n":
			return false
		default:
			return true
		}
	}
	return defaultValue
}

// GetFloat64Range retrieves an environment variable as a float64, ensuring it's within [min, max].
// Returns the default value if parsing fails or the value is out of range.
func GetFloat64Range(key string, defaultValue, min, max float64) float64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= min && parsed <= max {
			return parsed
		}
	}
	return defaultValue
}

// GetIntNonNegative retrieves an environment variable as a non-negative int.
// Returns the default value if parsing fails or the value is negative.
func GetIntNonNegative(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			return parsed
		}
	}
	return defaultValue
}

// GetPositiveInt retrieves an environment variable as a positive int.
// Returns the default value if parsing fails or the value is not positive.
func GetPositiveInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}

// GetPositiveInt64 retrieves an environment variable as a positive int64.
// Returns the default value if parsing fails or the value is not positive.
func GetPositiveInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}

// GetPositiveFloat64 retrieves an environment variable as a positive float64.
// Returns the default value if parsing fails or the value is not positive.
func GetPositiveFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}

// =============================================================================
// Configuration Loaders
// =============================================================================

// GetDeploymentVariant returns the deployment variant from DEPLOYMENT_VARIANT env var,
// or the default value if not set.
func GetDeploymentVariant() string {
	return GetString("DEPLOYMENT_VARIANT", DefaultDeploymentVariant)
}

// GetTokenizerModel returns the tokenizer model name from TOKENIZER_MODEL env var,
// or the default value if not set.
func GetTokenizerModel() string {
	return GetString("TOKENIZER_MODEL", DefaultTokenizerModel)
}

// GetMaxWorkers returns the max workers from MAX_WORKERS env var,
// or the default value if not set/invalid.
func GetMaxWorkers() int64 {
	return GetPositiveInt64("MAX_WORKERS", DefaultMaxWorkers)
}

// GetMaxRetries returns the max retries from MAX_RETRIES env var,
// or the default value if not set/invalid.
func GetMaxRetries() int {
	return GetIntNonNegative("MAX_RETRIES", DefaultMaxRetries)
}

// GetTargetURL returns the Z.AI API target URL from ZAI_TARGET_URL env var,
// or the default value if not set.
func GetTargetURL() string {
	return GetString("ZAI_TARGET_URL", DefaultTargetURL)
}

// GetTokenCountingEnabled returns whether token counting is enabled from
// TOKEN_COUNTING_ENABLED env var, or the default value if not set.
func GetTokenCountingEnabled() bool {
	return GetBool("TOKEN_COUNTING_ENABLED", DefaultTokenCountingEnabled)
}

// GetRateLimitInitial returns the initial rate limit from RATE_LIMIT_INITIAL env var,
// or the default value if not set/invalid.
func GetRateLimitInitial() float64 {
	return GetPositiveFloat64("RATE_LIMIT_INITIAL", DefaultRateLimitInitial)
}

// GetRateLimitMin returns the minimum rate limit from RATE_LIMIT_MIN env var,
// or the default value if not set/invalid.
func GetRateLimitMin() float64 {
	return GetPositiveFloat64("RATE_LIMIT_MIN", DefaultRateLimitMin)
}

// GetRateLimitMax returns the maximum rate limit from RATE_LIMIT_MAX env var,
// or the default value if not set/invalid.
func GetRateLimitMax() float64 {
	return GetPositiveFloat64("RATE_LIMIT_MAX", DefaultRateLimitMax)
}

// GetRateLimitCeilingAlpha returns the ceiling smoothing factor from RATE_LIMIT_CEILING_ALPHA env var,
// or the default value if not set/invalid. Must be in (0, 1].
func GetRateLimitCeilingAlpha() float64 {
	return GetFloat64Range("RATE_LIMIT_CEILING_ALPHA", DefaultRateLimitCeilingAlpha, 0, 1)
}

// GetRateLimitHoldMargin returns the hold margin from RATE_LIMIT_HOLD_MARGIN env var,
// or the default value if not set/invalid. Must be in (0, 1).
func GetRateLimitHoldMargin() float64 {
	return GetFloat64Range("RATE_LIMIT_HOLD_MARGIN", DefaultRateLimitHoldMargin, 0, 1)
}

// GetRateLimitProbeInterval returns the probe interval from RATE_LIMIT_PROBE_INTERVAL env var,
// or the default value if not set/invalid.
func GetRateLimitProbeInterval() int {
	return GetPositiveInt("RATE_LIMIT_PROBE_INTERVAL", DefaultRateLimitProbeInterval)
}
