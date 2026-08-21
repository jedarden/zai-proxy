package main

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAdaptiveRateLimiterConcurrentRecordingAcrossStates exercises the two
// recording paths at increasing concurrent-operation counts while the limiter
// is actively adjusting its rate. Run this test with -race to verify that the
// atomic counters and adjustment lock protect normal, rate-limited, and probe
// behavior alike.
func TestAdaptiveRateLimiterConcurrentRecordingAcrossStates(t *testing.T) {
	states := []struct {
		name    string
		prepare func(*testing.T) *AdaptiveRateLimiter
	}{
		{name: "normal", prepare: newConcurrentNormalLimiter},
		{name: "rate_limited", prepare: newConcurrentRateLimitedLimiter},
		{name: "probing", prepare: newConcurrentProbingLimiter},
	}

	for _, state := range states {
		for _, operations := range []int{10, 100, 1000} {
			t.Run(state.name+"/"+concurrentOperationName(operations), func(t *testing.T) {
				arl := state.prepare(t)
				runConcurrentRecordOperations(t, arl, operations)

				if got := arl.GetCurrentRate(); got < arl.minRate || got > arl.maxRate {
					t.Errorf("rate after %d concurrent operations = %.2f, want in [%.2f, %.2f]",
						operations, got, arl.minRate, arl.maxRate)
				}
			})
		}
	}
}

func concurrentOperationName(operations int) string {
	return "concurrent_operations_" + strconv.Itoa(operations)
}

// newConcurrentNormalLimiter leaves the limiter at its initial rate. A zero
// adjustment window makes every recording call contend on an active rate
// adjustment instead of only the atomic request counters.
func newConcurrentNormalLimiter(t *testing.T) *AdaptiveRateLimiter {
	t.Helper()
	return NewAdaptiveRateLimiterWithWindow(30, 1, 60, 0)
}

// newConcurrentRateLimitedLimiter first executes the 429 adjustment branch,
// producing a rate below the initial rate, then enables per-call adjustments
// for the concurrent portion of the test.
func newConcurrentRateLimitedLimiter(t *testing.T) *AdaptiveRateLimiter {
	t.Helper()
	arl := NewAdaptiveRateLimiterWithWindow(30, 1, 60, time.Hour)

	arl.mu.Lock()
	arl.estimatedCeiling = 20
	arl.lastAdjustment = time.Now().Add(-arl.adjustmentWindow - time.Millisecond)
	arl.mu.Unlock()
	arl.Record429()

	if got := arl.GetCurrentRate(); got >= 30 {
		t.Fatalf("rate-limited setup rate = %.2f, want less than initial rate 30.00", got)
	}

	arl.mu.Lock()
	arl.adjustmentWindow = 0
	arl.mu.Unlock()
	return arl
}

// newConcurrentProbingLimiter first executes the clean-window probe branch,
// producing a rate above the estimated ceiling before concurrent recording
// begins.
func newConcurrentProbingLimiter(t *testing.T) *AdaptiveRateLimiter {
	t.Helper()
	arl := NewAdaptiveRateLimiterWithWindow(49, 1, 60, time.Hour)

	arl.mu.Lock()
	arl.estimatedCeiling = 50
	arl.holdMargin = 0.02
	arl.probeInterval = 1
	arl.lastAdjustment = time.Now().Add(-arl.adjustmentWindow - time.Millisecond)
	arl.mu.Unlock()
	arl.RecordSuccess()

	if got, ceiling := arl.GetCurrentRate(), arl.estimatedCeiling; got <= ceiling {
		t.Fatalf("probing setup rate = %.2f, want above estimated ceiling %.2f", got, ceiling)
	}

	arl.mu.Lock()
	arl.adjustmentWindow = 0
	arl.mu.Unlock()
	return arl
}

func runConcurrentRecordOperations(t *testing.T, arl *AdaptiveRateLimiter, operations int) {
	t.Helper()

	start := make(chan struct{})
	var ready, done sync.WaitGroup
	var record429Calls, recordSuccessCalls int64
	ready.Add(operations)
	done.Add(operations)

	for operation := 0; operation < operations; operation++ {
		go func(operation int) {
			defer done.Done()
			ready.Done()
			<-start

			if operation%2 == 0 {
				atomic.AddInt64(&record429Calls, 1)
				arl.Record429()
				return
			}

			atomic.AddInt64(&recordSuccessCalls, 1)
			arl.RecordSuccess()
		}(operation)
	}

	ready.Wait()
	close(start)
	done.Wait()

	want429 := int64((operations + 1) / 2)
	wantSuccess := int64(operations / 2)
	if got := atomic.LoadInt64(&record429Calls); got != want429 {
		t.Errorf("Record429 calls = %d, want %d", got, want429)
	}
	if got := atomic.LoadInt64(&recordSuccessCalls); got != wantSuccess {
		t.Errorf("RecordSuccess calls = %d, want %d", got, wantSuccess)
	}
}
