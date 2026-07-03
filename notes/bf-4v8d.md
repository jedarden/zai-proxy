# Parsing Functions Catalog

## Task: Catalog duplicate parsing functions

This document catalogs all implementations of `GetString`, `GetInt`, `GetBool`, and `ParseDurationOrDefault` functions in the zai-proxy codebase.

---

## Summary

**NO DUPLICATES FOUND** - All parsing functions are consolidated in a single shared package `internal/configenv`.

---

## Function Definitions

All parsing functions are defined in a single location:

### `internal/configenv/env.go`

| Function | Signature | Description |
|----------|-----------|-------------|
| `GetString` | `GetString(key, defaultValue string) string` | Retrieves env var or returns default |
| `GetInt` | `GetInt(key string, defaultValue int) int` | Parses env var as int or returns default |
| `GetInt64` | `GetInt64(key string, defaultValue int64) int64` | Parses env var as int64 or returns default |
| `GetIntNonNegative` | `GetIntNonNegative(key string, defaultValue int) int` | Parses env var as non-negative int |
| `GetPositiveInt` | `GetPositiveInt(key string, defaultValue int) int` | Parses env var as positive int (>0) |
| `GetPositiveInt64` | `GetPositiveInt64(key string, defaultValue int64) int64` | Parses env var as positive int64 (>0) |
| `GetFloat64` | `GetFloat64(key string, defaultValue float64) float64` | Parses env var as float64 or returns default |
| `GetPositiveFloat64` | `GetPositiveFloat64(key string, defaultValue float64) float64` | Parses env var as positive float64 (>0) |
| `GetFloat64Range` | `GetFloat64Range(key string, defaultValue, min, max float64) float64` | Parses env var as float64 within range [min, max] |
| `GetBool` | `GetBool(key string, defaultValue bool) bool` | Parses env var as bool or returns default |
| `ParseDurationOrDefault` | `ParseDurationOrDefault(key string, defaultValue time.Duration) time.Duration` | Parses env var as duration or returns default |

---

## Usage by Package

### `proxy/config/config.go`

Uses the following shared helpers:
- `GetString` - for `DEPLOYMENT_VARIANT`, `TOKENIZER_MODEL`, `ZAI_TARGET_URL`
- `GetIntNonNegative` - for `MAX_RETRIES`
- `GetBool` - for `TOKEN_COUNTING_ENABLED`
- `GetPositiveInt64` - for `MAX_WORKERS`
- `GetPositiveFloat64` - for `RATE_LIMIT_INITIAL`, `RATE_LIMIT_MIN`, `RATE_LIMIT_MAX`
- `GetFloat64Range` - for `RATE_LIMIT_CEILING_ALPHA`, `RATE_LIMIT_HOLD_MARGIN`
- `GetPositiveInt` - for `RATE_LIMIT_PROBE_INTERVAL`

### `dashboard/config/config.go`

Uses the following shared helpers:
- `GetString` - for `SCRAPE_TARGETS`, `LISTEN_ADDR`, `DB_PATH`
- `ParseDurationOrDefault` - for `SCRAPE_INTERVAL`, `SCRAPE_TIMEOUT`, `RETENTION_5S`, `RETENTION_1M`

---

## Direct `os.Getenv` Usage

The following files use `os.Getenv` directly but are NOT duplicates:

### `proxy/main.go` (lines 206, 220, 227, 234)

Direct env var reads for:
- `ZAI_API_KEY` - Runtime API key (not config constant)
- `ZAI_PROXY_VERSION` - Build-time version override
- `ZAI_PROXY_COMMIT` - Build-time commit override
- `ZAI_PROXY_BUILD_TIME` - Build-time timestamp override

These are runtime overrides applied at application startup, not configuration constants, so direct `os.Getenv` usage is appropriate.

---

## Other Inline Parsing (Not Duplicates)

### `strconv.Atoi` usage

- `proxy/main.go:469` - Parses HTTP `Retry-After` header value
- `proxy/handler.go:215` - Parses HTTP `Retry-After` header value

These are parsing HTTP response headers, not environment variables.

### `strconv.Itoa` usage

- `proxy/main.go:562` - Converts HTTP status code to string for header
- `proxy/handler.go:308` - Converts HTTP status code to string for header

These are format conversions for HTTP headers, not parsing.

### `time.ParseDuration` usage

- `dashboard/api/router.go:194` - Parses duration from query parameter

This is parsing a URL query parameter, not an environment variable.

---

## Conclusion

**No consolidation needed** - The codebase already follows the correct pattern:

1. All environment variable parsing functions are centralized in `internal/configenv/env.go`
2. Both `proxy/config` and `dashboard/config` use the shared helpers correctly
3. Direct `os.Getenv` usage is limited to appropriate cases (runtime overrides, not config)
4. No duplicate implementations found

The `internal/configenv` package is a well-designed shared utility that provides:
- Consistent parsing behavior across the codebase
- Type-safe helpers with validation (non-negative, positive, range checks)
- Single source of truth for env var parsing logic
