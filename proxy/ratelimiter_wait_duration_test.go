package main

import (
	"math"
	"testing"
	"time"
)

// TestAdaptiveRateLimiterWaitDurationSanity verifies the refill delay after
// the initial token burst has been consumed. A fresh limiter has an available
// burst, so measuring it before draining that burst would only measure call
// overhead rather than the configured request rate.
func TestAdaptiveRateLimiterWaitDurationSanity(t *testing.T) {
	const (
		windowSize = time.Second
		minRate    = 2.0
		maxRate    = 16.0
		tolerance  = 25 * time.Millisecond
	)

	tests := []struct {
		name string
		rate float64
	}{
		{name: "minimum rate", rate: minRate},
		{name: "intermediate rate", rate: 4},
		{name: "maximum rate", rate: maxRate},
	}

	waits := make(map[float64]time.Duration, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiter(tt.rate, minRate, maxRate)

			// No history has been recorded: this is the initial limiter state.
			if got := arl.recent429Count + arl.recentSuccessCount; got != 0 {
				t.Fatalf("new limiter history = %d, want 0", got)
			}

			for i := 0; i < arl.limiter.Burst(); i++ {
				if wait := arl.Wait("test"); wait < 0 {
					t.Fatalf("Wait() returned negative duration while draining burst: %v", wait)
				}
			}

			got := arl.Wait("test")
			if got < 0 {
				t.Fatalf("Wait() returned negative duration: %v", got)
			}

			want := time.Duration(float64(windowSize) / math.Max(1, tt.rate))
			if got < want-tolerance || got > want+tolerance {
				t.Errorf("Wait() after burst exhaustion = %v, want %v (windowSize / max(1, rate)) within %v", got, want, tolerance)
			}

			waits[tt.rate] = got
		})
	}

	if waits[minRate] <= waits[4] || waits[4] <= waits[maxRate] {
		t.Errorf("Wait() durations do not scale inversely with rate: rate %.0f = %v, rate 4 = %v, rate %.0f = %v", minRate, waits[minRate], waits[4], maxRate, waits[maxRate])
	}
}

func TestAdaptiveRateLimiterWaitZeroRateBlocksAfterBurst(t *testing.T) {
	arl := NewAdaptiveRateLimiter(0, 0, 16)

	done := make(chan time.Duration, 1)
	go func() {
		done <- arl.Wait("test")
	}()

	select {
	case wait := <-done:
		t.Fatalf("Wait() with rate=0 returned %v; want infinite wait", wait)
	case <-time.After(50 * time.Millisecond):
		// A zero-rate limiter cannot issue or replenish a token.
	}
}
