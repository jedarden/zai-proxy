// Package config provides shared configuration constants and helpers for the dashboard.
package config

import (
	"git.ardenone.com/jedarden/zai-proxy/internal/configenv"
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

// Storage retention defaults.
const (
	// DefaultRetention5s is the default retention for 5-second interval data.
	DefaultRetention5s = 24 * time.Hour
	// DefaultRetention1m is the default retention for 1-minute interval data.
	DefaultRetention1m = 168 * time.Hour // 7 days
)

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
	if value := configenv.GetString("SCRAPE_TARGETS", ""); value != "" {
		return SplitTargets(value)
	}
	return []string{DefaultScrapeTarget}
}

// GetScrapeInterval returns the scrape interval from SCRAPE_INTERVAL env var,
// or the default interval if not set/invalid.
func GetScrapeInterval() time.Duration {
	return configenv.ParseDurationOrDefault("SCRAPE_INTERVAL", DefaultScrapeInterval)
}

// GetScrapeTimeout returns the scrape timeout from SCRAPE_TIMEOUT env var,
// or the default timeout if not set/invalid.
func GetScrapeTimeout() time.Duration {
	return configenv.ParseDurationOrDefault("SCRAPE_TIMEOUT", DefaultScrapeTimeout)
}

// GetListenAddr returns the HTTP listen address from LISTEN_ADDR env var,
// or the default address if not set.
func GetListenAddr() string {
	return configenv.GetString("LISTEN_ADDR", DefaultListenAddr)
}

// GetEnvOrDefault returns an environment value or a supplied fallback.
// It remains here for dashboard packages that share this configuration API.
func GetEnvOrDefault(key, fallback string) string {
	return configenv.GetString(key, fallback)
}

// GetRetention5s returns the retention for 5s data from RETENTION_5S env var,
// or the default retention if not set/invalid.
func GetRetention5s() time.Duration {
	return configenv.ParseDurationOrDefault("RETENTION_5S", DefaultRetention5s)
}

// GetRetention1m returns the retention for 1m data from RETENTION_1M env var,
// or the default retention if not set/invalid.
func GetRetention1m() time.Duration {
	return configenv.ParseDurationOrDefault("RETENTION_1M", DefaultRetention1m)
}
