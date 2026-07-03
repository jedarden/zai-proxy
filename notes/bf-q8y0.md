# Bead bf-q8y0: Consolidate Duplicate Default Parsing Logic

## Summary

Successfully consolidated duplicate "get env or default" parsing logic in the zai-proxy codebase.

## Changes Made

### Before (3 duplicate implementations)
1. **proxy/config/config.go** - `GetString(key, defaultValue string) string`
2. **dashboard/config/config.go** - `GetEnvOrDefault(key, defaultValue string) string`
3. **dashboard/logger/logger.go** - `getEnvOrDefault(key, def string) string`

### After (2 canonical implementations)
1. **proxy/config/config.go** - `GetString()` - canonical for main module
2. **dashboard/config/config.go** - `GetEnvOrDefault()` - canonical for dashboard module
3. **dashboard/logger/logger.go** - now uses `config.GetEnvOrDefault()` instead of its own duplicate

## Files Modified
- `dashboard/logger/logger.go` - removed duplicate `getEnvOrDefault()` function, now imports and uses `dashboard/config.GetEnvOrDefault()`

## Verification
- Code compiles successfully: `go build ./...` in dashboard module
- Commit created: `66d09c3` - "refactor(bf-q8y0): consolidate duplicate getEnvOrDefault parsing logic"
- Pushed to remote: `git.ardenone.com/jedarden/zai-proxy.git`

## Acceptance Criteria Met
- ✅ Default parsing defined exactly once per module (proxy/config for main, dashboard/config for dashboard)
- ✅ No duplicate parsing logic exists (removed from logger)
- ✅ Code compiles successfully after consolidation

## Note on Bead Close Command
The `br close bf-q8y0` command failed with "Invalid claimed_at format: premature end of input" - this appears to be a bug in the br CLI tool, not related to this work.
