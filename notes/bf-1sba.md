# Bead bf-1sba: Extract shared DefaultConfig logic

## Finding

The task to extract shared DefaultConfig logic was **already complete** when this bead was claimed.

### Current State

The `dashboard/config` package already serves as the single source of truth for environment variable parsing:

1. **`config.GetScrapeTargets()`** - parses SCRAPE_TARGETS env var (lines 61-68)
2. **`config.GetScrapeInterval()`** - parses SCRAPE_INTERVAL env var (lines 70-74)
3. **`config.GetScrapeTimeout()`** - parses SCRAPE_TIMEOUT env var (lines 76-80)
4. **`config.GetListenAddr()`** - parses LISTEN_ADDR env var (lines 82-86)

### Both DefaultConfig implementations use the shared source

**collector.DefaultConfig()** (dashboard/collector/collector.go:40-46):
```go
func DefaultConfig() Config {
	return Config{
		Targets:  config.GetScrapeTargets(),
		Interval: config.GetScrapeInterval(),
		Timeout:  config.GetScrapeTimeout(),
	}
}
```

**api.DefaultConfig()** (dashboard/api/router.go:29-35):
```go
func DefaultConfig() *Config {
	return &Config{
		ListenAddr:     config.GetListenAddr(),
		ScrapeInterval: config.GetScrapeInterval(),
		ScrapeTargets:  config.GetScrapeTargets(),
	}
}
```

### Conclusion

No code changes were needed. The SCRAPE_TARGETS and SCRAPE_INTERVAL environment variable parsing logic was already centralized in the `config` package, and both DefaultConfig implementations already reference the shared helper functions. The two cannot drift because they both call the same single source of truth.

This refactoring was likely done in an earlier commit (possibly `633df37 refactor(bf-j6s): update default SCRAPE_TARGETS from mcp to devpod namespace` or related work).
