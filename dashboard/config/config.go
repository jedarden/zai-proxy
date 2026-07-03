// Package config provides shared configuration constants and helpers for the dashboard.
package config

import (
	"os"
	"time"
)

// DefaultScrapeTarget is the default Prometheus metrics endpoint to scrape.
const DefaultScrapeTarget = "http://zai-proxy.devpod.svc.cluster.local:8080/metrics"

// DefaultScrapeInterval is the default interval between scrapes.
const DefaultScrapeInterval = 5 * time.Second

// DefaultScrapeTimeout is the default HTTP timeout for each scrape.
const DefaultScrapeTimeout = 3 * time.Second

// DefaultListenAddr is the default HTTP listen address for the dashboard API.
const DefaultListenAddr = ":8080"

// Storage defaults
const (
	// DefaultDBPath is the default path to the SQLite database file.
	DefaultDBPath = "/data/dashboard.db"
	// DefaultRetention5s is the default retention for 5-second interval data.
	DefaultRetention5s = 24 * time.Hour
	// DefaultRetention1m is the default retention for 1-minute interval data.
	DefaultRetention1m = 168 * time.Hour // 7 days
)

// GetEnvOrDefault retrieves an environment variable or returns a default value.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ParseDurationOrDefault parses a duration from an env var or returns a default.
func ParseDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// SplitTargets splits a comma-separated list of scrape targets.
// Empty strings between commas are skipped.
func SplitTargets(s string) []string {
	var result []string
	var current string
	for _, c := range s {
		if c == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// GetScrapeTargets returns the list of scrape targets from SCRAPE_TARGETS env var,
// or the default target if not set.
func GetScrapeTargets() []string {
	if value := os.Getenv("SCRAPE_TARGETS"); value != "" {
		return SplitTargets(value)
	}
	return []string{DefaultScrapeTarget}
}

// GetScrapeInterval returns the scrape interval from SCRAPE_INTERVAL env var,
// or the default interval if not set/invalid.
func GetScrapeInterval() time.Duration {
	return ParseDurationOrDefault("SCRAPE_INTERVAL", DefaultScrapeInterval)
}

// GetScrapeTimeout returns the scrape timeout from SCRAPE_TIMEOUT env var,
// or the default timeout if not set/invalid.
func GetScrapeTimeout() time.Duration {
	return ParseDurationOrDefault("SCRAPE_TIMEOUT", DefaultScrapeTimeout)
}

// GetListenAddr returns the HTTP listen address from LISTEN_ADDR env var,
// or the default address if not set.
func GetListenAddr() string {
	return GetEnvOrDefault("LISTEN_ADDR", DefaultListenAddr)
}

// GetDBPath returns the database path from DB_PATH env var,
// or the default path if not set.
func GetDBPath() string {
	return GetEnvOrDefault("DB_PATH", DefaultDBPath)
}

// GetRetention5s returns the retention for 5s data from RETENTION_5S env var,
// or the default retention if not set/invalid.
func GetRetention5s() time.Duration {
	return ParseDurationOrDefault("RETENTION_5S", DefaultRetention5s)
}

// GetRetention1m returns the retention for 1m data from RETENTION_1M env var,
// or the default retention if not set/invalid.
func GetRetention1m() time.Duration {
	return ParseDurationOrDefault("RETENTION_1M", DefaultRetention1m)
}
