// Package configenv provides shared environment variable parsing helpers
// across all packages in the zai-proxy project.
package configenv

import (
	"os"
	"strconv"
	"time"
)

// GetString retrieves an environment variable or returns the default value.
// If the environment variable is set and non-empty, that value is returned.
// Otherwise, the defaultValue is returned.
func GetString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetInt retrieves an environment variable as an int, or returns the default value.
// If the environment variable is set and can be parsed as an integer, that value is returned.
// Otherwise, the defaultValue is returned.
func GetInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// GetInt64 retrieves an environment variable as an int64, or returns the default value.
// If the environment variable is set and can be parsed as a 64-bit integer, that value is returned.
// Otherwise, the defaultValue is returned.
func GetInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
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

// GetFloat64 retrieves an environment variable as a float64, or returns the default value.
// If the environment variable is set and can be parsed as a float64, that value is returned.
// Otherwise, the defaultValue is returned.
func GetFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
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

// GetBool retrieves an environment variable as a bool, or returns the default value.
// Accepts "false", "0", "no", "n" as false; everything else is true (including empty string).
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

// ParseDurationOrDefault parses a duration from an env var or returns a default.
// Returns the default value if the env var is not set or parsing fails.
func ParseDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
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
