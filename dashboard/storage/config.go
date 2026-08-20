// Package storage implements bounded in-memory metric storage.
package storage

import (
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/config"
)

const (
	rawResolution         = 5 * time.Second
	downsampledResolution = time.Minute
	defaultMaxVariants    = 2
)

// Config holds in-memory storage configuration.
type Config struct {
	Retention5s time.Duration // Retention for 5-second data (default 24h).
	Retention1m time.Duration // Retention for 1-minute data (default 7d).
	MaxVariants int           // Metric streams to retain (production and canary).
}

// DefaultConfig returns the default storage configuration.
func DefaultConfig() Config {
	return Config{
		Retention5s: config.GetRetention5s(),
		Retention1m: config.GetRetention1m(),
		MaxVariants: defaultMaxVariants,
	}
}

func (c Config) normalized() Config {
	if c.Retention5s <= 0 {
		c.Retention5s = config.DefaultRetention5s
	}
	if c.Retention1m <= 0 {
		c.Retention1m = config.DefaultRetention1m
	}
	if c.MaxVariants <= 0 {
		c.MaxVariants = defaultMaxVariants
	}
	return c
}

func capacityFor(retention, resolution time.Duration) int {
	capacity := int((retention + resolution - 1) / resolution)
	if capacity < 1 {
		return 1
	}
	return capacity
}
