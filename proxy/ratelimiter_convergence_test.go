package main

import (
	"math"
	"testing"
	"time"
)

// These tests cover the control-loop defects found in production on
// 2026-09-08, when the proxy held a sustained 70-90% upstream 429 rate for
// days without converging. Each one fails against the pre-fix loop.

// runWindow drives exactly one adjustment window with the given mix of 429s
// and successes, then returns the rate the limiter settled on.
func runWindow(t *testing.T, arl *AdaptiveRateLimiter, window time.Duration, count429, countSuccess int) float64 {
	t.Helper()

	for i := 0; i < count429; i++ {
		arl.Record429()
	}
	for i := 0; i < countSuccess; i++ {
		arl.RecordSuccess()
	}

	// Force the window boundary, then land one more sample so tryAdjustRate
	// runs with the counters above already accumulated.
	arl.mu.Lock()
	arl.lastAdjustment = arl.lastAdjustment.Add(-window - time.Millisecond)
	arl.mu.Unlock()
	if count429 > 0 {
		arl.Record429()
	} else {
		arl.RecordSuccess()
	}

	return arl.GetCurrentRate()
}

// TestCeilingSeedsFromInitialRate pins the constructor to initialRate. Seeding
// at maxRate made the first 429 window blend toward maxRate and *raise* the
// rate: production launched at 8 req/s and jumped to 29.79 req/s on its first
// window with maxRate=40.
func TestCeilingSeedsFromInitialRate(t *testing.T) {
	arl := NewAdaptiveRateLimiterWithWindow(8, 0.1, 40, 10*time.Millisecond)

	if arl.estimatedCeiling != 8 {
		t.Errorf("estimatedCeiling = %.2f, want 8.00 (initialRate)", arl.estimatedCeiling)
	}

	// The first window under heavy rejection must not raise the rate.
	rate := runWindow(t, arl, 10*time.Millisecond, 80, 20)
	if rate > 8 {
		t.Errorf("first 429 window raised the rate to %.2f req/s from an initial 8.00; "+
			"backoff must never increase the rate", rate)
	}
}

// TestBackoffScalesWithRejectionRate is the core fix: the ceiling update must
// respond to how badly the window was rejected. The pre-fix loop fed the EWMA
// its own currentRate, which made every 429 window decay the estimate by the
// same 0.6% whether the window saw 6% 429s or 95%.
func TestBackoffScalesWithRejectionRate(t *testing.T) {
	const window = 10 * time.Millisecond

	tests := []struct {
		name         string
		count429     int
		countSuccess int
	}{
		{"mild rejection", 10, 90},
		{"heavy rejection", 90, 10},
	}

	drops := make(map[string]float64, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arl := NewAdaptiveRateLimiterWithWindow(10, 0.1, 40, window)
			before := arl.GetCurrentRate()
			after := runWindow(t, arl, window, tt.count429, tt.countSuccess)

			if after >= before {
				t.Fatalf("rate did not drop: %.4f → %.4f", before, after)
			}
			drops[tt.name] = (before - after) / before
			t.Logf("429 rate %d%%: %.4f → %.4f req/s (%.1f%% drop)",
				tt.count429, before, after, drops[tt.name]*100)
		})
	}

	// A 90% rejection window must back off substantially harder than a 10%
	// one. The pre-fix loop produced an identical 0.6% drop for both.
	if drops["heavy rejection"] <= drops["mild rejection"]*3 {
		t.Errorf("backoff is not proportional to rejection: 10%% 429s dropped %.2f%%, "+
			"90%% 429s dropped %.2f%% (want the heavy case at least 3x larger)",
			drops["mild rejection"]*100, drops["heavy rejection"]*100)
	}
}

// TestSustainedRejectionConvergesQuickly bounds how long a saturated limiter
// takes to reach a sustainable rate. Production sat at 8-10 req/s against an
// upstream tolerating roughly 1 req/s, shedding 0.6% per 30s window: over
// three hours to converge, and a single clean window re-probed upward before
// it got there.
func TestSustainedRejectionConvergesQuickly(t *testing.T) {
	const window = 10 * time.Millisecond
	const sustainable = 1.0

	arl := NewAdaptiveRateLimiterWithWindow(10, 0.1, 40, window)

	// Model an upstream that accepts `sustainable` req/s and 429s the rest,
	// which is what the fleet actually saw.
	windows := 0
	for ; windows < 20; windows++ {
		current := arl.GetCurrentRate()
		if current <= sustainable*1.5 {
			break
		}
		accepted := int(math.Round(100 * sustainable / current))
		runWindow(t, arl, window, 100-accepted, accepted)
	}

	final := arl.GetCurrentRate()
	if final > sustainable*1.5 {
		t.Errorf("after %d windows the rate is still %.2f req/s against a sustainable %.2f req/s",
			windows, final, sustainable)
	}
	if windows > 10 {
		t.Errorf("took %d windows to converge (want <= 10; at a 30s window that is 5 minutes)", windows)
	}
	t.Logf("converged to %.2f req/s in %d windows", final, windows)
}

// TestCeilingNeverFallsBelowMinRate guards the recovery path. Both the hold
// point and the probe rate are multiples of estimatedCeiling, so an estimate
// driven to zero by total rejection would leave the loop nothing to climb from.
func TestCeilingNeverFallsBelowMinRate(t *testing.T) {
	const window = 10 * time.Millisecond
	const minRate = 0.5

	arl := NewAdaptiveRateLimiterWithWindow(10, minRate, 40, window)

	for i := 0; i < 30; i++ {
		runWindow(t, arl, window, 100, 0) // 100% rejection
	}

	if arl.estimatedCeiling < minRate {
		t.Errorf("estimatedCeiling = %.4f, want >= minRate %.2f", arl.estimatedCeiling, minRate)
	}
	if got := arl.GetCurrentRate(); got < minRate {
		t.Errorf("currentRate = %.4f, want >= minRate %.2f", got, minRate)
	}
}

// TestCleanProbeRaisesCeiling covers the other half of the loop. A probe steps
// above the estimate to test for headroom; before the fix nothing recorded a
// clean result, so the next probe re-tested the identical rate forever and the
// estimate could only ever move down.
func TestCleanProbeRaisesCeiling(t *testing.T) {
	const window = 10 * time.Millisecond

	arl := NewAdaptiveRateLimiterWithWindow(10, 0.1, 40, window)
	arl.probeInterval = 3

	// Clean windows until a probe fires, taking the rate above the estimate.
	var probed bool
	for i := 0; i < 6 && !probed; i++ {
		runWindow(t, arl, window, 0, 100)
		arl.mu.RLock()
		probed = arl.currentRate > arl.estimatedCeiling
		arl.mu.RUnlock()
	}
	if !probed {
		t.Fatal("probe never raised the rate above the estimated ceiling")
	}

	probeRate := arl.GetCurrentRate()

	// The probe held clean, so the estimate must adopt the proven rate.
	runWindow(t, arl, window, 0, 100)

	arl.mu.RLock()
	ceiling := arl.estimatedCeiling
	arl.mu.RUnlock()

	if ceiling < probeRate {
		t.Errorf("estimatedCeiling = %.4f after a clean window at %.4f req/s; "+
			"a sustained probe must raise the estimate or the loop can never recover",
			ceiling, probeRate)
	}
}
