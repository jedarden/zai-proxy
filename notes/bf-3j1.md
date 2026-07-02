# Bead bf-3j1: AdaptiveRateLimiter Window Duration Injection - COMPLETED

## Summary
Test-only refactor complete: AdaptiveRateLimiter now supports configurable window duration injection for fast tests.

## Implementation

### Production Constructor (Default 30s Window)
```go
func NewAdaptiveRateLimiter(initialRate, minRate, maxRate float64) *AdaptiveRateLimiter
```
- Used in production code (proxy/main.go line 316)
- Defaults to 30s adjustment window
- No behavior changes to production code paths

### Testable Constructor (Configurable Window)
```go
func NewAdaptiveRateLimiterWithWindow(initialRate, minRate, maxRate float64, windowDuration time.Duration) *AdaptiveRateLimiter
```
- Allows test code to inject millisecond-scale windows
- Already used in TestAdaptiveRateLimiter_NoAdjustInWindow (proxy/ratelimiter_test.go line 919)
- Enables fast test execution without sleeping

## Test Usage Example
```go
testWindow := 100 * time.Millisecond
arl := NewAdaptiveRateLimiterWithWindow(10.0, 1.0, 50.0, testWindow)
```

## Acceptance Criteria Met
- ✅ NewAdaptiveRateLimiter accepts optional window duration via NewAdaptiveRateLimiterWithWindow
- ✅ Production callers continue using default 30s window (not 60s as originally described, but 30s is the actual default)
- ✅ Tests can inject millisecond-scale windows for fast iteration
- ✅ No changes to rate limiting logic or defaults

## Files Modified
- `proxy/main.go` - Added NewAdaptiveRateLimiterWithWindow constructor
- `proxy/ratelimiter_test.go` - Added test using window injection
